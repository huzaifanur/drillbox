package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const coursesURL = "http://127.0.0.1:8080/courses/"

func runLauncher(parent context.Context) error {
	for _, name := range []string{"zenity", "xdg-open"} {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("the launcher needs %s: %w", name, err)
		}
	}

	lock, err := launcherLock()
	if err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			_ = openCourses()
			return nil
		}
		return err
	}
	defer lock.Close()

	if connection, err := net.DialTimeout("tcp", address, 250*time.Millisecond); err == nil {
		connection.Close()
		showLauncherError("Port 8080 is already in use. Stop the existing Drillbox process, then launch again.")
		return errors.New("port 8080 is already in use")
	}

	ctx, cancel := context.WithCancel(parent)
	serverErr := make(chan error, 1)
	go func() { serverErr <- runServer(ctx) }()

	if err := waitForServer(ctx, serverErr); err != nil {
		cancel()
		showLauncherError(err.Error())
		return err
	}
	_ = openCourses()

	for ctx.Err() == nil {
		command := exec.CommandContext(ctx, "zenity",
			"--question",
			"--title=Drillbox",
			"--width=430",
			"--text=Drillbox is active.\n\nOpen Courses reopens the catalog.\nStop & Close shuts down the course server and lab containers while keeping your progress and Grafana data.",
			"--ok-label=Open Courses",
			"--cancel-label=Stop & Close",
			"--default-cancel",
		)
		if err := command.Run(); err != nil {
			break
		}
		_ = openCourses()
	}

	cancel()
	select {
	case err := <-serverErr:
		return err
	case <-time.After(40 * time.Second):
		return errors.New("timed out cleaning up Drillbox")
	}
}

func launcherLock() (*os.File, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("find cache directory: %w", err)
	}
	dir = filepath.Join(dir, "read-run")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(dir, "launcher.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open launcher lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func waitForServer(ctx context.Context, serverErr <-chan error) error {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case err := <-serverErr:
			if err == nil {
				return errors.New("Drillbox stopped before it became ready")
			}
			return err
		case <-ticker.C:
			response, err := client.Get("http://" + address + "/api/courses")
			if err == nil {
				response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return nil
				}
			}
		case <-timeout.C:
			return errors.New("Drillbox did not become ready within 10 seconds")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func openCourses() error {
	return exec.Command("xdg-open", coursesURL).Start()
}

func showLauncherError(message string) {
	_ = exec.Command("zenity", "--error", "--title=Drillbox", "--text="+message).Run()
}

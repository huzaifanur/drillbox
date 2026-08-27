package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// This slower check is run before releases, not on every local test cycle.
func TestEveryGoExerciseRuns(t *testing.T) {
	if os.Getenv("DRILLBOX_EXHAUSTIVE") != "1" {
		t.Skip("set DRILLBOX_EXHAUSTIVE=1 to execute the complete curriculum")
	}
	_, items, err := loadAuthoredCourses("lessons/courses")
	if err != nil {
		t.Fatal(err)
	}

	type check struct {
		lesson lesson
		name   string
		code   string
		pass   bool
	}
	jobs := make(chan check)
	errors := make(chan error, 32)
	var workers sync.WaitGroup
	for range 12 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				output, runErr := runCurriculumProgram(job.code)
				if runErr != nil {
					if job.pass {
						errors <- fmt.Errorf("%s %s did not run: %w", job.lesson.ID, job.name, runErr)
					}
					continue
				}
				matches := normalize(output) == normalize(job.lesson.Expected)
				if matches != job.pass {
					errors <- fmt.Errorf("%s %s produced %q; expected %q", job.lesson.ID, job.name, normalize(output), normalize(job.lesson.Expected))
				}
			}
		}()
	}
	go func() {
		for _, item := range items {
			if lessonMode(item) != "go" {
				continue
			}
			jobs <- check{lesson: item, name: "starter", code: item.Starter}
			jobs <- check{lesson: item, name: "solution", code: item.Solution, pass: true}
		}
		close(jobs)
		workers.Wait()
		close(errors)
	}()
	for err := range errors {
		t.Error(err)
	}
}

func runCurriculumProgram(code string) (string, error) {
	dir, err := os.MkdirTemp("", "drillbox-curriculum-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(code), 0o600); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "run", "main.go")
	command.Dir = dir
	command.Env = append(os.Environ(), "GOWORK=off")
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return string(output), ctx.Err()
	}
	return string(output), err
}

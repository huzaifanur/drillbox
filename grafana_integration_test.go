//go:build integration

package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestGrafanaDockerLifecycle(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker is not installed")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("Docker daemon is not available")
	}

	grafanaPort := freeTCPPort(t)
	prometheusPort := freeTCPPort(t)
	metricsPort := freeTCPPort(t)
	project := fmt.Sprintf("read-run-integration-%d", os.Getpid())
	t.Setenv("COMPOSE_PROJECT_NAME", project)
	t.Setenv("READ_RUN_GRAFANA_STATE", t.TempDir())
	t.Setenv("READ_RUN_GRAFANA_PORT", strconv.Itoa(grafanaPort))
	t.Setenv("READ_RUN_PROMETHEUS_PORT", strconv.Itoa(prometheusPort))
	t.Setenv("READ_RUN_METRICS_PORT", strconv.Itoa(metricsPort))
	t.Setenv("READ_RUN_GRAFANA_URL", fmt.Sprintf("http://127.0.0.1:%d", grafanaPort))
	t.Setenv("READ_RUN_PROMETHEUS_URL", fmt.Sprintf("http://127.0.0.1:%d", prometheusPort))
	t.Setenv("READ_RUN_METRICS_URL", fmt.Sprintf("http://127.0.0.1:%d", metricsPort))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	manager := newLabManager(ctx)
	defer func() {
		manager.stop()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = manager.compose(cleanupCtx, "down", "-v", "--remove-orphans")
	}()

	manager.start("grafana")
	deadline := time.Now().Add(2 * time.Minute)
	for manager.publicStatus("grafana").State == "starting" && time.Now().Before(deadline) {
		time.Sleep(250 * time.Millisecond)
	}
	status := manager.publicStatus("grafana")
	if status.State != "ready" {
		t.Fatalf("lab did not become ready: %s (%s)", status.State, status.Message)
	}
	if output, passed, err := manager.checkPromQL(ctx, "promql-selector", "go_lab_requests_total"); err != nil || !passed {
		t.Fatalf("live PromQL check output=%q passed=%v err=%v", output, passed, err)
	}

	manager.stop()
	if err := exec.Command("docker", "volume", "inspect", project+"_grafana_data").Run(); err != nil {
		t.Fatal("Grafana volume was not preserved by normal cleanup")
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

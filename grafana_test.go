package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLabManagerStartIsIdempotentAndStopPreservesData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics", "/-/ready":
			w.WriteHeader(http.StatusOK)
		case "/api/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"database": "ok", "version": "13.2.0"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	manager := newLabManager(context.Background())
	manager.stateDir = t.TempDir()
	manager.grafanaURL = server.URL
	manager.promURL = server.URL
	manager.metricsURL = server.URL
	manager.client = server.Client()
	var mu sync.Mutex
	var calls [][]string
	manager.composeRun = func(_ context.Context, args ...string) ([]byte, error) {
		mu.Lock()
		calls = append(calls, append([]string(nil), args...))
		mu.Unlock()
		return nil, nil
	}

	manager.start()
	manager.start()
	deadline := time.Now().Add(time.Second)
	for manager.publicStatus().State != "ready" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := manager.publicStatus().State; got != "ready" {
		t.Fatalf("lab state = %q, want ready", got)
	}
	manager.stop()

	mu.Lock()
	defer mu.Unlock()
	var up, reset, down int
	for _, call := range calls {
		joined := strings.Join(call, " ")
		switch {
		case joined == "up -d":
			up++
		case strings.HasPrefix(joined, "exec -T grafana"):
			reset++
		case joined == "down --timeout 10":
			down++
		}
		if strings.Contains(joined, "-v") {
			t.Fatalf("cleanup must preserve volumes: %q", joined)
		}
	}
	if up != 1 || reset != 1 || down != 1 {
		t.Fatalf("unexpected lifecycle calls: %#v", calls)
	}
}

func TestPromQLLessonQueriesLivePrometheus(t *testing.T) {
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Get("query")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data":   map[string]any{"resultType": "vector", "result": []any{map[string]any{"value": []any{1, "3"}}}},
		})
	}))
	defer server.Close()

	manager := newLabManager(context.Background())
	manager.promURL = server.URL
	manager.client = server.Client()
	output, passed, err := manager.checkPromQL(context.Background(), "promql-rate", "rate(go_lab_requests_total[5m])")
	if err != nil || !passed || !strings.Contains(output, "PASS") || query != "rate(go_lab_requests_total[5m])" {
		t.Fatalf("output=%q passed=%v query=%q err=%v", output, passed, query, err)
	}
	_, passed, err = manager.checkPromQL(context.Background(), "promql-rate", "go_lab_requests_total")
	if err != nil || passed {
		t.Fatalf("wrong query passed=%v err=%v", passed, err)
	}
}

func TestLabWorkspaceRejectsTraversal(t *testing.T) {
	manager := newLabManager(context.Background())
	manager.stateDir = t.TempDir()
	manager.state = "ready"
	_, passed, err := manager.runLesson(context.Background(), lesson{
		Mode: "file", Workspace: "../outside.yml", Validator: "provision-provider",
	}, "content")
	if err == nil || passed {
		t.Fatalf("path traversal passed=%v err=%v", passed, err)
	}
}

func TestParseDashboardModelAcceptsGrafanaExport(t *testing.T) {
	model, err := parseDashboardModel(`{"dashboard":{"title":"Read Run Go Service","panels":[{"type":"timeseries"}]},"meta":{}}`)
	if err != nil || model["title"] != "Read Run Go Service" || !hasDashboardPanels(model) {
		t.Fatalf("model=%#v err=%v", model, err)
	}
	model, err = parseDashboardModel(`{"title":"Empty","panels":[]}`)
	if err != nil || hasDashboardPanels(model) {
		t.Fatalf("empty dashboard reported panels: %#v err=%v", model, err)
	}
}

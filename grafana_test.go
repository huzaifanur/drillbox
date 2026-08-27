package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
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

	manager.start("grafana")
	manager.start("grafana")
	deadline := time.Now().Add(time.Second)
	for manager.publicStatus("grafana").State != "ready" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := manager.publicStatus("grafana").State; got != "ready" {
		t.Fatalf("lab state = %q, want ready", got)
	}
	manager.stop()

	mu.Lock()
	defer mu.Unlock()
	var up, reset, down int
	for _, call := range calls {
		joined := strings.Join(call, " ")
		switch {
		case joined == "--profile grafana up -d metrics prometheus grafana":
			up++
		case strings.HasPrefix(joined, "exec -T grafana"):
			reset++
		case joined == "--profile grafana --profile observability --profile pubsub --profile postgres --profile object-storage down --timeout 10":
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

func TestLabCleanupWaitsForActiveComposeOperation(t *testing.T) {
	manager := newLabManager(context.Background())
	upStarted := make(chan struct{})
	releaseUp := make(chan struct{})
	downStarted := make(chan struct{})
	manager.composeRun = func(_ context.Context, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "up -d") {
			close(upStarted)
			<-releaseUp
			return nil, errors.New("startup stopped for test")
		}
		if strings.Contains(joined, "down --timeout 10") {
			close(downStarted)
		}
		return nil, nil
	}

	manager.start("postgres")
	<-upStarted
	stopped := make(chan struct{})
	go func() {
		manager.stop()
		close(stopped)
	}()
	select {
	case <-downStarted:
		t.Fatal("cleanup overlapped the active compose operation")
	case <-time.After(30 * time.Millisecond):
	}
	close(releaseUp)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not finish after the active operation stopped")
	}
}

func TestEveryCourseLabProfileHasServicesAndPublicStatus(t *testing.T) {
	manager := newLabManager(context.Background())
	for _, profile := range []string{"grafana", "observability", "pubsub", "postgres", "object-storage"} {
		steps, services, ok := labProfile(profile)
		if !ok || len(steps) == 0 || len(steps) != len(services) {
			t.Fatalf("profile %q steps=%#v services=%#v ok=%v", profile, steps, services, ok)
		}
		status := manager.publicStatus(profile)
		if status.State != "stopped" || status.Steps == nil || status.Links == nil {
			t.Fatalf("profile %q public status = %#v", profile, status)
		}
	}
	if _, _, ok := labProfile("unknown"); ok {
		t.Fatal("unknown lab profile was accepted")
	}
}

func TestLabHandlersRejectUnknownProfiles(t *testing.T) {
	manager := newLabManager(context.Background())
	for _, handler := range []http.HandlerFunc{manager.statusHandler, manager.startHandler} {
		request := httptest.NewRequest(http.MethodGet, "/api/labs/unknown", nil)
		request.SetPathValue("id", "unknown")
		response := httptest.NewRecorder()
		handler(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("unknown lab status = %d, want 404", response.Code)
		}
	}
}

func TestServiceReadyLessonsProbeEveryDockerProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	manager := newLabManager(context.Background())
	manager.client = server.Client()
	manager.grafanaURL, manager.promURL, manager.metricsURL = server.URL, server.URL, server.URL
	manager.jaegerURL, manager.rabbitURL, manager.minioURL = server.URL, server.URL, server.URL
	manager.postgres = listener.Addr().String()
	for _, profile := range []string{"grafana", "observability", "pubsub", "postgres", "object-storage"} {
		manager.profile, manager.state = profile, "ready"
		output, passed, err := manager.runLesson(context.Background(), lesson{Mode: "lab", LabID: profile, Validator: "service-ready"}, "check")
		if err != nil || !passed || !strings.HasPrefix(output, "PASS") {
			t.Fatalf("profile %q output=%q passed=%v err=%v", profile, output, passed, err)
		}
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
	manager.profile = "grafana"
	_, passed, err := manager.runLesson(context.Background(), lesson{
		Mode: "file", Workspace: "../outside.yml", Validator: "provision-provider",
	}, "content")
	if err == nil || passed {
		t.Fatalf("path traversal passed=%v err=%v", passed, err)
	}
}

func TestOldLabFailureDoesNotReplaceCurrentStatus(t *testing.T) {
	manager := newLabManager(context.Background())
	manager.profile = "postgres"
	manager.state = "starting"
	manager.fail("grafana", context.DeadlineExceeded)
	if got := manager.publicStatus("postgres").State; got != "starting" {
		t.Fatalf("current lab state = %q, want starting", got)
	}
}

func TestReplacedLabStopsWaiting(t *testing.T) {
	manager := newLabManager(context.Background())
	manager.profile = "postgres"
	if err := manager.waitReady(context.Background(), "grafana", time.Minute); err == nil {
		t.Fatal("replaced lab continued waiting")
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

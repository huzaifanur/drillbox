package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type authoredCourse struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	RequiredLab string   `json:"requiredLab"`
	Lessons     []lesson `json:"lessons"`
}

func loadAuthoredCourse(path string) (course, []lesson, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return course{}, nil, fmt.Errorf("read authored course: %w", err)
	}
	var authored authoredCourse
	if err := json.Unmarshal(data, &authored); err != nil {
		return course{}, nil, fmt.Errorf("parse authored course: %w", err)
	}
	if authored.ID == "" || len(authored.Lessons) == 0 {
		return course{}, nil, errors.New("authored course is incomplete")
	}
	for i := range authored.Lessons {
		authored.Lessons[i].CourseID = authored.ID
		authored.Lessons[i].CourseTitle = authored.Title
		authored.Lessons[i].LabID = authored.RequiredLab
	}
	return course{
		ID:          authored.ID,
		Title:       authored.Title,
		Description: authored.Description,
		LessonCount: len(authored.Lessons),
		RequiredLab: authored.RequiredLab,
	}, authored.Lessons, nil
}

func loadAuthoredCourses(dir string) ([]course, []lesson, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("find authored courses: %w", err)
	}
	if len(paths) == 0 {
		return nil, nil, fmt.Errorf("no authored courses found in %s", dir)
	}
	sort.Strings(paths)
	seenCourses := make(map[string]bool, len(paths))
	seenLessons := make(map[string]bool)
	courses := make([]course, 0, len(paths))
	var lessons []lesson
	for _, path := range paths {
		item, items, err := loadAuthoredCourse(path)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, err)
		}
		if seenCourses[item.ID] {
			return nil, nil, fmt.Errorf("duplicate course id %q", item.ID)
		}
		seenCourses[item.ID] = true
		for _, lesson := range items {
			if lesson.ID == "" || lesson.Chapter == "" || lesson.Title == "" || lesson.Instruction == "" || lesson.Starter == "" {
				return nil, nil, fmt.Errorf("%s contains an incomplete lesson", path)
			}
			if seenLessons[lesson.ID] {
				return nil, nil, fmt.Errorf("duplicate lesson id %q", lesson.ID)
			}
			seenLessons[lesson.ID] = true
		}
		courses = append(courses, item)
		lessons = append(lessons, items...)
	}
	return courses, lessons, nil
}

type labStep struct {
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
}

type labLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type labStatus struct {
	State       string            `json:"state"`
	Message     string            `json:"message"`
	Steps       []labStep         `json:"steps"`
	Links       []labLink         `json:"links"`
	Credentials map[string]string `json:"credentials"`
}

type labManager struct {
	ctx        context.Context
	root       string
	stateDir   string
	grafanaURL string
	promURL    string
	metricsURL string
	jaegerURL  string
	rabbitURL  string
	minioURL   string
	minioUIURL string
	postgres   string
	client     *http.Client
	composeRun func(context.Context, ...string) ([]byte, error)

	mu      sync.Mutex
	opMu    sync.Mutex
	state   string
	message string
	steps   []labStep
	started bool
	profile string
}

func newLabManager(ctx context.Context) *labManager {
	root, _ := os.Getwd()
	stateDir := os.Getenv("READ_RUN_GRAFANA_STATE")
	if stateDir == "" {
		home, _ := os.UserHomeDir()
		stateDir = filepath.Join(home, ".local", "state", "read-run", "grafana")
	}
	return &labManager{
		ctx:        ctx,
		root:       root,
		stateDir:   stateDir,
		grafanaURL: envOr("READ_RUN_GRAFANA_URL", "http://127.0.0.1:3001"),
		promURL:    envOr("READ_RUN_PROMETHEUS_URL", "http://127.0.0.1:9090"),
		metricsURL: envOr("READ_RUN_METRICS_URL", "http://127.0.0.1:8081"),
		jaegerURL:  envOr("READ_RUN_JAEGER_URL", "http://127.0.0.1:16686"),
		rabbitURL:  envOr("READ_RUN_RABBIT_URL", "http://127.0.0.1:15672"),
		minioURL:   envOr("READ_RUN_MINIO_URL", "http://127.0.0.1:9000"),
		minioUIURL: envOr("READ_RUN_MINIO_CONSOLE_URL", "http://127.0.0.1:9001"),
		postgres:   envOr("READ_RUN_POSTGRES_ADDRESS", "127.0.0.1:5433"),
		client:     &http.Client{Timeout: 2 * time.Second},
		state:      "stopped",
		message:    "The required lab is stopped.",
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func labProfile(profile string) (steps []labStep, services []string, ok bool) {
	switch profile {
	case "grafana":
		return []labStep{{Name: "Go metrics"}, {Name: "Prometheus"}, {Name: "Grafana"}}, []string{"metrics", "prometheus", "grafana"}, true
	case "observability":
		return []labStep{{Name: "Go metrics"}, {Name: "Prometheus"}, {Name: "Grafana"}, {Name: "Jaeger"}}, []string{"metrics", "prometheus", "grafana", "jaeger"}, true
	case "pubsub":
		return []labStep{{Name: "RabbitMQ"}}, []string{"rabbitmq"}, true
	case "postgres":
		return []labStep{{Name: "PostgreSQL"}}, []string{"postgres"}, true
	case "object-storage":
		return []labStep{{Name: "MinIO"}}, []string{"minio"}, true
	default:
		return nil, nil, false
	}
}

func (m *labManager) publicStatus(profile string) labStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if profile != m.profile {
		steps, _, _ := labProfile(profile)
		return labStatus{State: "stopped", Message: "The required lab is stopped.", Steps: steps, Links: m.links(profile), Credentials: m.credentials(profile)}
	}
	steps := append([]labStep(nil), m.steps...)
	return labStatus{
		State:       m.state,
		Message:     m.message,
		Steps:       steps,
		Links:       m.links(profile),
		Credentials: m.credentials(profile),
	}
}

func (m *labManager) links(profile string) []labLink {
	switch profile {
	case "grafana":
		return []labLink{{Label: "Grafana", URL: m.grafanaURL}, {Label: "Prometheus", URL: m.promURL}, {Label: "Go metrics", URL: m.metricsURL + "/metrics"}}
	case "observability":
		return []labLink{{Label: "Grafana", URL: m.grafanaURL}, {Label: "Prometheus", URL: m.promURL}, {Label: "Jaeger", URL: m.jaegerURL}}
	case "pubsub":
		return []labLink{{Label: "RabbitMQ", URL: m.rabbitURL}}
	case "object-storage":
		return []labLink{{Label: "MinIO", URL: m.minioUIURL}}
	default:
		return []labLink{}
	}
}

func (m *labManager) credentials(profile string) map[string]string {
	switch profile {
	case "grafana":
		return map[string]string{"username": "admin", "password": "admin"}
	case "observability":
		return map[string]string{"username": "admin", "password": "admin"}
	case "pubsub":
		return map[string]string{"username": "guest", "password": "guest"}
	case "object-storage":
		return map[string]string{"username": "drillbox", "password": "drillbox-local"}
	case "postgres":
		return map[string]string{"username": "drillbox", "password": "drillbox-local"}
	default:
		return nil
	}
}

func (m *labManager) statusHandler(w http.ResponseWriter, r *http.Request) {
	profile := r.PathValue("id")
	if _, _, ok := labProfile(profile); !ok {
		http.Error(w, "lab not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, m.publicStatus(profile))
}

func (m *labManager) startHandler(w http.ResponseWriter, r *http.Request) {
	profile := r.PathValue("id")
	if _, _, ok := labProfile(profile); !ok {
		http.Error(w, "lab not found", http.StatusNotFound)
		return
	}
	m.start(profile)
	writeJSON(w, http.StatusAccepted, m.publicStatus(profile))
}

func (m *labManager) start(profile string) {
	steps, services, ok := labProfile(profile)
	if !ok {
		return
	}
	m.mu.Lock()
	if m.profile == profile && (m.state == "starting" || m.state == "ready") {
		m.mu.Unlock()
		return
	}
	m.profile = profile
	m.state = "starting"
	m.message = "Starting Docker services. The first launch can take a few minutes."
	m.steps = steps
	m.started = true
	m.mu.Unlock()

	go func() {
		m.opMu.Lock()
		defer m.opMu.Unlock()
		if profile == "grafana" || profile == "observability" {
			if err := m.ensureState(); err != nil {
				m.fail(profile, err)
				return
			}
		}
		args := append([]string{"--profile", profile, "up", "-d"}, services...)
		if output, err := m.compose(m.ctx, args...); err != nil {
			m.fail(profile, fmt.Errorf("Docker could not start the lab: %s", commandMessage(output, err)))
			return
		}
		if err := m.waitReady(m.ctx, profile, 2*time.Minute); err != nil {
			m.fail(profile, err)
			return
		}
		if profile == "grafana" || profile == "observability" {
			if output, err := m.compose(m.ctx, "exec", "-T", "grafana", "grafana", "cli", "admin", "reset-admin-password", "admin"); err != nil {
				m.fail(profile, fmt.Errorf("Grafana credentials could not be prepared: %s", commandMessage(output, err)))
				return
			}
		}
		readySteps, _, _ := labProfile(profile)
		for i := range readySteps {
			readySteps[i].Ready = true
		}
		m.mu.Lock()
		if m.profile != profile {
			m.mu.Unlock()
			return
		}
		m.state = "ready"
		m.message = "The required Docker services are ready."
		m.steps = readySteps
		m.mu.Unlock()
	}()
}

func (m *labManager) fail(profile string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.profile != profile {
		return
	}
	if errors.Is(err, context.Canceled) {
		m.state = "stopped"
		m.message = "The required lab was stopped."
		return
	}
	m.state = "error"
	m.message = err.Error()
}

func (m *labManager) waitReady(ctx context.Context, profile string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		current := m.profile == profile
		m.mu.Unlock()
		if !current {
			return errors.New("lab startup was replaced by another course")
		}
		steps := m.probe(ctx, profile)
		m.mu.Lock()
		if m.profile == profile {
			m.steps = steps
		}
		m.mu.Unlock()
		ready := true
		for _, step := range steps {
			ready = ready && step.Ready
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("the required lab did not become healthy within two minutes")
		case <-ticker.C:
		}
	}
}

func (m *labManager) probe(ctx context.Context, profile string) []labStep {
	switch profile {
	case "grafana":
		return []labStep{{Name: "Go metrics", Ready: m.getOK(ctx, m.metricsURL+"/metrics", nil)}, {Name: "Prometheus", Ready: m.getOK(ctx, m.promURL+"/-/ready", nil)}, {Name: "Grafana", Ready: m.getOK(ctx, m.grafanaURL+"/api/health", nil)}}
	case "observability":
		return []labStep{{Name: "Go metrics", Ready: m.getOK(ctx, m.metricsURL+"/metrics", nil)}, {Name: "Prometheus", Ready: m.getOK(ctx, m.promURL+"/-/ready", nil)}, {Name: "Grafana", Ready: m.getOK(ctx, m.grafanaURL+"/api/health", nil)}, {Name: "Jaeger", Ready: m.getOK(ctx, m.jaegerURL, nil)}}
	case "pubsub":
		return []labStep{{Name: "RabbitMQ", Ready: m.getOK(ctx, m.rabbitURL, nil)}}
	case "postgres":
		return []labStep{{Name: "PostgreSQL", Ready: m.tcpOK(ctx, m.postgres)}}
	case "object-storage":
		return []labStep{{Name: "MinIO", Ready: m.getOK(ctx, m.minioURL+"/minio/health/live", nil)}}
	default:
		return nil
	}
}

func (m *labManager) tcpOK(ctx context.Context, address string) bool {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	connection.Close()
	return true
}

func (m *labManager) getOK(ctx context.Context, endpoint string, target any) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	response, err := m.client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false
	}
	if target != nil && json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(target) != nil {
		return false
	}
	return true
}

func (m *labManager) compose(ctx context.Context, args ...string) ([]byte, error) {
	if m.composeRun != nil {
		return m.composeRun(ctx, args...)
	}
	command := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	command.Dir = m.root
	command.Env = append(os.Environ(), "READ_RUN_GRAFANA_STATE="+m.stateDir)
	return command.CombinedOutput()
}

func commandMessage(output []byte, err error) string {
	message := strings.TrimSpace(string(output))
	if len(message) > 600 {
		message = message[len(message)-600:]
	}
	if message == "" {
		message = err.Error()
	}
	return message
}

func (m *labManager) stop() {
	m.mu.Lock()
	started := m.started
	m.started = false
	m.state = "stopped"
	m.message = "The required lab is stopped."
	m.steps = nil
	m.profile = ""
	m.mu.Unlock()
	if !started {
		return
	}
	m.opMu.Lock()
	defer m.opMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{
		"--profile", "grafana",
		"--profile", "observability",
		"--profile", "pubsub",
		"--profile", "postgres",
		"--profile", "object-storage",
		"down", "--timeout", "10",
	}
	if output, err := m.compose(ctx, args...); err != nil {
		fmt.Fprintf(os.Stderr, "Drillbox could not clean up course containers: %s\n", commandMessage(output, err))
	}
}

func (m *labManager) ensureState() error {
	for _, path := range []string{"", "provisioning"} {
		fullPath := filepath.Join(m.stateDir, path)
		if err := os.MkdirAll(fullPath, 0o755); err != nil {
			return fmt.Errorf("prepare lab state: %w", err)
		}
		if err := os.Chmod(fullPath, 0o755); err != nil {
			return fmt.Errorf("prepare lab state permissions: %w", err)
		}
	}
	containerPaths := []string{"provisioning/datasources", "provisioning/dashboards", "provisioning/alerting", "provisioning/plugins", "dashboards"}
	for _, path := range containerPaths {
		fullPath := filepath.Join(m.stateDir, path)
		if err := os.MkdirAll(fullPath, 0o755); err != nil {
			return fmt.Errorf("prepare lab files: %w", err)
		}
		if err := os.Chmod(fullPath, 0o755); err != nil {
			return fmt.Errorf("prepare lab permissions: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Join(m.stateDir, "exports"), 0o700); err != nil {
		return fmt.Errorf("prepare lab exports: %w", err)
	}
	seed := filepath.Join(m.stateDir, "dashboards", "provider-check.json")
	if _, err := os.Stat(seed); errors.Is(err, os.ErrNotExist) {
		data := []byte(`{"uid":"provider-check","title":"Provider Check","schemaVersion":39,"panels":[]}`)
		if err := os.WriteFile(seed, data, 0o644); err != nil {
			return fmt.Errorf("seed provider check: %w", err)
		}
	}
	return nil
}

func (m *labManager) runLesson(ctx context.Context, item lesson, input string) (string, bool, error) {
	profile := item.LabID
	if profile == "" {
		profile = "grafana"
	}
	if m.publicStatus(profile).State != "ready" {
		return "", false, errors.New("the required lab is not ready")
	}
	switch lessonMode(item) {
	case "lab":
		if item.Validator == "service-ready" {
			return m.checkServiceReady(ctx, profile)
		}
		return m.check(ctx, item.Validator, input)
	case "promql":
		return m.checkPromQL(ctx, item.Validator, input)
	case "file":
		return m.applyFile(ctx, item, input)
	default:
		return "", false, fmt.Errorf("unsupported lesson mode %q", item.Mode)
	}
}

func (m *labManager) checkServiceReady(ctx context.Context, profile string) (string, bool, error) {
	steps := m.probe(ctx, profile)
	if len(steps) == 0 {
		return "", false, fmt.Errorf("unknown lab profile %q", profile)
	}
	for _, step := range steps {
		if !step.Ready {
			return "NOT YET · " + step.Name + " is not ready.", false, nil
		}
	}
	return "PASS · The live " + profile + " services are ready.", true, nil
}

func (m *labManager) checkPromQL(ctx context.Context, validator, input string) (string, bool, error) {
	expected := map[string]string{
		"promql-selector":    `go_lab_requests_total`,
		"promql-labels":      `go_lab_requests_total{job="go-lab"}`,
		"promql-range":       `go_lab_requests_total[5m]`,
		"promql-rate":        `rate(go_lab_requests_total[5m])`,
		"promql-aggregation": `sumby(job)(rate(go_lab_requests_total[5m]))`,
	}[validator]
	if expected == "" {
		return "", false, fmt.Errorf("unknown PromQL validator %q", validator)
	}
	compact := strings.NewReplacer(" ", "", "\n", "", "\t", "", "\r", "").Replace(input)
	if compact != strings.ReplaceAll(expected, " ", "") {
		return "The query ran, but it does not match this lesson's requested expression.", false, nil
	}
	var result struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []any  `json:"result"`
		} `json:"data"`
		Error string `json:"error"`
	}
	endpoint := m.promURL + "/api/v1/query?" + url.Values{"query": []string{input}}.Encode()
	if !m.getOK(ctx, endpoint, &result) {
		return "Prometheus rejected the query.", false, nil
	}
	if result.Status != "success" || len(result.Data.Result) == 0 {
		return "The query is valid but returned no live series yet. Wait for another scrape and retry.", false, nil
	}
	return fmt.Sprintf("PASS · Prometheus returned %s data for %s", result.Data.ResultType, input), true, nil
}

func (m *labManager) applyFile(ctx context.Context, item lesson, input string) (string, bool, error) {
	profile := item.LabID
	if profile == "" {
		profile = "grafana"
	}
	clean := filepath.Clean(item.Workspace)
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false, errors.New("lesson workspace path is not allowed")
	}
	allowed := map[string]bool{
		"exports/dashboard.json":                 true,
		"provisioning/dashboards/read-run.yml":   true,
		"provisioning/datasources/read-run.yml":  true,
		"dashboards/read-run-provisioned.json":   true,
		"exports/read-run-dashboard-backup.json": true,
	}
	if !allowed[filepath.ToSlash(clean)] {
		return "", false, errors.New("lesson workspace path is not allowlisted")
	}
	if !m.opMu.TryLock() {
		return "", false, errors.New("another lab operation is still running")
	}
	defer m.opMu.Unlock()
	if err := m.writeStateFile(clean, []byte(input)); err != nil {
		return "", false, err
	}

	restart := strings.HasPrefix(item.Validator, "provision-")
	if restart {
		m.mu.Lock()
		m.state = "starting"
		m.message = "Applying the provisioning file and restarting Grafana."
		m.mu.Unlock()
		output, err := m.compose(ctx, "restart", "grafana")
		if err != nil {
			m.fail(profile, fmt.Errorf("Grafana restart failed: %s", commandMessage(output, err)))
			return "", false, errors.New("Grafana restart failed")
		}
		if err := m.waitReady(ctx, profile, 60*time.Second); err != nil {
			m.fail(profile, err)
			return "", false, err
		}
		m.mu.Lock()
		m.state = "ready"
		m.message = "The provisioning file was applied and Grafana is ready."
		m.mu.Unlock()
	}
	return m.check(ctx, item.Validator, input)
}

func (m *labManager) writeStateFile(relative string, data []byte) error {
	path := filepath.Join(m.stateDir, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create workspace directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".read-run-*")
	if err != nil {
		return fmt.Errorf("create workspace file: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	mode := os.FileMode(0o600)
	if !strings.HasPrefix(filepath.ToSlash(relative), "exports/") {
		mode = 0o644
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("save workspace file: %w", err)
	}
	return nil
}

func (m *labManager) grafanaGET(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, m.grafanaURL+path, nil)
	if err != nil {
		return err
	}
	request.SetBasicAuth("admin", "admin")
	response, err := m.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Grafana returned %s", response.Status)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(target)
}

func (m *labManager) dashboardByTitle(ctx context.Context, title string) (map[string]any, error) {
	var results []struct {
		UID   string `json:"uid"`
		Title string `json:"title"`
	}
	if err := m.grafanaGET(ctx, "/api/search?query="+url.QueryEscape(title), &results); err != nil {
		return nil, err
	}
	for _, result := range results {
		if result.Title != title {
			continue
		}
		var response struct {
			Dashboard map[string]any `json:"dashboard"`
			Meta      map[string]any `json:"meta"`
		}
		if err := m.grafanaGET(ctx, "/api/dashboards/uid/"+url.PathEscape(result.UID), &response); err != nil {
			return nil, err
		}
		response.Dashboard["_meta"] = response.Meta
		return response.Dashboard, nil
	}
	return nil, errors.New("dashboard not found")
}

func (m *labManager) check(ctx context.Context, validator, input string) (string, bool, error) {
	pass := func(message string) (string, bool, error) { return "PASS · " + message, true, nil }
	fail := func(message string) (string, bool, error) { return "NOT YET · " + message, false, nil }

	switch validator {
	case "grafana-health":
		var health map[string]any
		if m.getOK(ctx, m.grafanaURL+"/api/health", &health) && health["database"] == "ok" {
			return pass("Grafana is healthy.")
		}
		return fail("Grafana health is not ready.")
	case "stack-ready":
		for _, step := range m.probe(ctx, "grafana") {
			if !step.Ready {
				return fail(step.Name + " is not ready.")
			}
		}
		var targets map[string]any
		if !m.getOK(ctx, m.promURL+"/api/v1/targets", &targets) || !jsonContains(targets, `"health":"up"`) || !jsonContains(targets, `"job":"go-lab"`) {
			return fail("Prometheus has not marked the go-lab target up.")
		}
		return pass("the observability stack is connected.")
	case "grafana-version":
		var health map[string]any
		if m.getOK(ctx, m.grafanaURL+"/api/health", &health) && health["version"] == "13.2.0" && health["database"] == "ok" {
			return pass("Grafana 13.2.0 is installed with a healthy database.")
		}
		return fail("expected Grafana 13.2.0 with database=ok.")
	case "dashboard-ui-tour", "dashboard-first":
		title := map[string]string{"dashboard-ui-tour": "Read Run UI Tour", "dashboard-first": "Read Run Go Service"}[validator]
		if _, err := m.dashboardByTitle(ctx, title); err == nil {
			return pass("dashboard " + title + " exists.")
		}
		return fail("create and save the dashboard " + title + ".")
	case "organization":
		var organizations []map[string]any
		if m.grafanaGET(ctx, "/api/orgs", &organizations) == nil && jsonContains(organizations, `"name":"Read Run Lab"`) {
			return pass("organization Read Run Lab exists.")
		}
		return fail("create the organization Read Run Lab.")
	case "datasource-created", "datasource-configured", "datasource-health", "provision-datasource":
		return m.checkDatasource(ctx, validator, pass, fail)
	case "scrape-target":
		var targets map[string]any
		if m.getOK(ctx, m.promURL+"/api/v1/targets", &targets) && jsonContains(targets, `"health":"up"`) && jsonContains(targets, `"job":"go-lab"`) {
			return pass("the go-lab scrape target is up.")
		}
		return fail("Prometheus does not show an up go-lab target.")
	case "explore-query":
		dashboard, err := m.dashboardByTitle(ctx, "Read Run UI Tour")
		if err == nil && jsonContains(dashboard, "go_lab_requests_total") {
			return pass("the UI Tour dashboard contains the explored metric.")
		}
		return fail("add go_lab_requests_total from Explore to Read Run UI Tour.")
	case "dashboard-panel":
		dashboard, err := m.dashboardByTitle(ctx, "Read Run Go Service")
		if err == nil && jsonContains(dashboard, "rate(go_lab_requests_total[5m])") {
			return pass("the dashboard has a request-rate panel.")
		}
		return fail("add a panel using rate(go_lab_requests_total[5m]).")
	case "dashboard-range":
		dashboard, err := m.dashboardByTitle(ctx, "Read Run Go Service")
		if err == nil && jsonContains(dashboard["time"], `"from":"now-6h"`) {
			return pass("the dashboard saves a six-hour range.")
		}
		return fail("save Read Run Go Service with the range Last 6 hours.")
	case "dashboard-variable":
		dashboard, err := m.dashboardByTitle(ctx, "Read Run Go Service")
		if err == nil && jsonContains(dashboard["templating"], `"name":"job"`) {
			return pass("the dashboard defines the job variable.")
		}
		return fail("add and save a dashboard variable named job.")
	case "dashboard-json":
		dashboard, err := parseDashboardModel(input)
		if err == nil && dashboard["title"] == "Read Run Go Service" && hasDashboardPanels(dashboard) && jsonContains(dashboard, `"name":"job"`) {
			return pass("the exported dashboard JSON contains its panel and variable.")
		}
		return fail("paste the exported JSON for Read Run Go Service.")
	case "alert-rule", "alert-expression":
		var rules []map[string]any
		if m.grafanaGET(ctx, "/api/v1/provisioning/alert-rules", &rules) != nil || !jsonContains(rules, `"title":"High Request Rate"`) {
			return fail("create the High Request Rate alert rule.")
		}
		if validator == "alert-expression" && (!jsonContains(rules, `"datasourceUid":"-100"`) && !jsonContains(rules, `"datasourceUid":"__expr__"`) && !jsonContains(rules, `"type":"reduce"`)) {
			return fail("add a Grafana expression stage to High Request Rate.")
		}
		return pass("the High Request Rate alert configuration is present.")
	case "contact-point":
		var points []map[string]any
		if m.grafanaGET(ctx, "/api/v1/provisioning/contact-points", &points) == nil && jsonContains(points, `"name":"Read Run Contact"`) && jsonContains(points, "http://metrics:8081/alerts") {
			return pass("the Read Run Contact webhook is configured.")
		}
		return fail("create Read Run Contact with the lab webhook URL.")
	case "notification-policy":
		var policy map[string]any
		if m.grafanaGET(ctx, "/api/v1/provisioning/policies", &policy) == nil && jsonContains(policy, `"receiver":"Read Run Contact"`) {
			return pass("notification policy routes to Read Run Contact.")
		}
		return fail("route the notification policy to Read Run Contact.")
	case "silence":
		var silences []map[string]any
		if m.grafanaGET(ctx, "/api/alertmanager/grafana/api/v2/silences", &silences) == nil && jsonContains(silences, "High Request Rate") {
			return pass("an active silence matches High Request Rate.")
		}
		return fail("create a silence for High Request Rate.")
	case "provision-provider":
		if _, err := m.dashboardByTitle(ctx, "Provider Check"); err == nil {
			return pass("Grafana loaded the dashboard provider file.")
		}
		return fail("the provider did not load Provider Check; review the YAML path and indentation.")
	case "folder-permission":
		return m.checkFolder(ctx, pass, fail)
	case "provision-dashboard":
		if dashboard, err := m.dashboardByTitle(ctx, "Read Run Provisioned"); err == nil && dashboard["uid"] == "read-run-provisioned" {
			return pass("Grafana loaded the provisioned dashboard.")
		}
		return fail("Grafana has not loaded dashboard UID read-run-provisioned.")
	case "backup-dashboard":
		dashboard, err := parseDashboardModel(input)
		if err == nil && (dashboard["uid"] == "read-run-go" || dashboard["title"] == "Read Run Go Service") && hasDashboardPanels(dashboard) {
			return pass("the dashboard backup is valid JSON and contains panels.")
		}
		return fail("paste a complete exported Read Run Go Service dashboard model.")
	default:
		return "", false, fmt.Errorf("unknown lab validator %q", validator)
	}
}

type checkResult func(string) (string, bool, error)

func (m *labManager) checkDatasource(ctx context.Context, validator string, pass, fail checkResult) (string, bool, error) {
	var sources []map[string]any
	if err := m.grafanaGET(ctx, "/api/datasources", &sources); err != nil {
		return fail("Grafana data sources are not available.")
	}
	wantName := "Read Run Prometheus"
	if validator == "provision-datasource" {
		wantName = "Provisioned Prometheus"
	}
	var source map[string]any
	for _, item := range sources {
		if item["name"] == wantName {
			source = item
			break
		}
	}
	if source == nil || source["type"] != "prometheus" {
		return fail("create the Prometheus data source " + wantName + ".")
	}
	if validator == "datasource-created" {
		return pass("the Read Run Prometheus data source exists.")
	}
	if source["url"] != "http://prometheus:9090" {
		return fail("set the data source URL to http://prometheus:9090.")
	}
	if validator == "datasource-configured" && source["isDefault"] != true {
		return fail("make Read Run Prometheus the default data source.")
	}
	if validator == "provision-datasource" {
		if source["readOnly"] == true {
			return pass("the provisioned Prometheus data source is read-only.")
		}
		return fail("the data source exists but was not loaded from provisioning.")
	}
	uid, _ := source["uid"].(string)
	var health map[string]any
	if uid != "" && m.grafanaGET(ctx, "/api/datasources/uid/"+url.PathEscape(uid)+"/health", &health) == nil && (health["status"] == "OK" || health["status"] == "success") {
		return pass("Grafana can query Read Run Prometheus.")
	}
	return fail("Save & test the data source until its health check passes.")
}

func (m *labManager) checkFolder(ctx context.Context, pass, fail checkResult) (string, bool, error) {
	var results []map[string]any
	if m.grafanaGET(ctx, "/api/search?type=dash-folder&query=Read%20Run%20Operations", &results) != nil {
		return fail("create the Read Run Operations folder.")
	}
	for _, result := range results {
		if result["title"] != "Read Run Operations" {
			continue
		}
		uid, _ := result["uid"].(string)
		var permissions []map[string]any
		if uid != "" && m.grafanaGET(ctx, "/api/folders/"+url.PathEscape(uid)+"/permissions", &permissions) == nil {
			for _, permission := range permissions {
				if permission["isInherited"] != true {
					return pass("the operations folder has an explicit permission entry.")
				}
			}
		}
	}
	return fail("create Read Run Operations and add a folder permission.")
}

func jsonContains(value any, fragment string) bool {
	data, _ := json.Marshal(value)
	return bytes.Contains(data, []byte(fragment))
}

func parseDashboardModel(input string) (map[string]any, error) {
	var model map[string]any
	if err := json.Unmarshal([]byte(input), &model); err != nil {
		return nil, err
	}
	if wrapped, ok := model["dashboard"].(map[string]any); ok {
		return wrapped, nil
	}
	return model, nil
}

func hasDashboardPanels(model map[string]any) bool {
	panels, ok := model["panels"].([]any)
	return ok && len(panels) > 0
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/importer"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	address        = "127.0.0.1:8080"
	maxCodeBytes   = 100_000
	maxOutputBytes = 64_000
)

type lesson struct {
	ID          string   `json:"id"`
	CourseID    string   `json:"courseId"`
	CourseTitle string   `json:"courseTitle"`
	Chapter     string   `json:"chapter"`
	Title       string   `json:"title"`
	Instruction string   `json:"instruction"`
	Starter     string   `json:"starter"`
	Expected    string   `json:"expected"`
	Solution    string   `json:"solution,omitempty"`
	Mode        string   `json:"mode,omitempty"`
	EditorLabel string   `json:"editorLabel,omitempty"`
	Validator   string   `json:"validator,omitempty"`
	Workspace   string   `json:"workspace,omitempty"`
	Setup       string   `json:"setup,omitempty"`
	Verify      string   `json:"verify,omitempty"`
	Columns     []string `json:"columns,omitempty"`
	LabID       string   `json:"-"`
}

type app struct {
	lessons []lesson
	byID    map[string]lesson
	courses []course
	labs    *labManager
}

type course struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	LessonCount int    `json:"lessonCount"`
	RequiredLab string `json:"requiredLab,omitempty"`
}

type cappedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		return 0, errors.New("output limit exceeded")
	}
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		return remaining, errors.New("output limit exceeded")
	}
	return b.Buffer.Write(p)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	if len(os.Args) > 1 && os.Args[1] == "--launcher" {
		err = runLauncher(ctx)
	} else {
		err = runServer(ctx)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func runServer(ctx context.Context) error {
	courses, lessons, err := loadAuthoredCourses("lessons/courses")
	if err != nil {
		return err
	}
	sqlCourse, sqlLessons := loadSQLCourse()
	lessons = append(lessons, sqlLessons...)
	courses = append(courses, sqlCourse)
	sortCourses(courses)

	a := &app{
		lessons: lessons,
		byID:    make(map[string]lesson, len(lessons)),
		courses: courses,
		labs:    newLabManager(ctx),
	}
	for _, item := range lessons {
		a.byID[item.ID] = item
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/lessons", a.listLessons)
	mux.HandleFunc("GET /api/courses", a.listCourses)
	mux.HandleFunc("GET /api/lessons/{id}", a.getLesson)
	mux.HandleFunc("POST /api/lessons/{id}/run", a.runLesson)
	mux.HandleFunc("POST /api/lessons/{id}/lint", a.lintLesson)
	mux.HandleFunc("POST /api/lint", lintCode)
	mux.HandleFunc("POST /api/format", formatCode)
	mux.HandleFunc("GET /api/labs/{id}", a.labs.statusHandler)
	mux.HandleFunc("POST /api/labs/{id}/start", a.labs.startHandler)
	mux.Handle("/", http.FileServer(http.Dir("static")))

	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	log.Printf("Drillbox is ready at http://%s", address)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	err = server.ListenAndServe()
	a.labs.stop()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func sortCourses(items []course) {
	order := map[string]int{
		"learn-go": 1, "http-clients": 2, "http-servers": 3, "pokedex": 4,
		"pubsub": 5, "ci-cd": 6, "http-protocol": 7, "blog-aggregator": 8,
		"cryptography": 9, "file-servers": 10, "observability": 11,
		"web-scraper": 12, "grafana": 13, "sql": 14,
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := order[items[i].ID], order[items[j].ID]
		if left == 0 {
			left = len(order) + 1
		}
		if right == 0 {
			right = len(order) + 1
		}
		if left == right {
			return items[i].Title < items[j].Title
		}
		return left < right
	})
}

var nonSlugCharacter = regexp.MustCompile(`[^a-z0-9]+`)

func slug(value string) string {
	value = strings.ToLower(value)
	return strings.Trim(nonSlugCharacter.ReplaceAllString(value, "-"), "-")
}

func (a *app) listCourses(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.courses)
}

func (a *app) listLessons(w http.ResponseWriter, r *http.Request) {
	type summary struct {
		ID          string `json:"id"`
		CourseID    string `json:"courseId"`
		CourseTitle string `json:"courseTitle"`
		Chapter     string `json:"chapter"`
		Title       string `json:"title"`
	}

	courseID := r.URL.Query().Get("course")
	if courseID == "" {
		courseID = "learn-go"
	}
	items := make([]summary, 0, len(a.lessons))
	for _, item := range a.lessons {
		if item.CourseID == courseID {
			items = append(items, summary{item.ID, item.CourseID, item.CourseTitle, item.Chapter, item.Title})
		}
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *app) getLesson(w http.ResponseWriter, r *http.Request) {
	item, ok := a.byID[r.PathValue("id")]
	if !ok {
		http.Error(w, "lesson not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, struct {
		ID          string `json:"id"`
		CourseID    string `json:"courseId"`
		CourseTitle string `json:"courseTitle"`
		Chapter     string `json:"chapter"`
		Title       string `json:"title"`
		Instruction string `json:"instruction"`
		Starter     string `json:"starter"`
		Mode        string `json:"mode"`
		EditorLabel string `json:"editorLabel"`
	}{item.ID, item.CourseID, item.CourseTitle, item.Chapter, item.Title, item.Instruction, item.Starter, lessonMode(item), editorLabel(item)})
}

func (a *app) runLesson(w http.ResponseWriter, r *http.Request) {
	item, ok := a.byID[r.PathValue("id")]
	if !ok {
		http.Error(w, "lesson not found", http.StatusNotFound)
		return
	}

	input, ok := readInput(w, r)
	if !ok {
		return
	}

	if lessonMode(item) == "sql" {
		runSQLLesson(w, r, item, input)
		return
	}
	if lessonMode(item) == "text" {
		output := normalize(input)
		writeJSON(w, http.StatusOK, struct {
			Output string `json:"output"`
			Passed bool   `json:"passed"`
		}{output, output == normalize(item.Expected)})
		return
	}
	if lessonMode(item) != "go" {
		output, passed, err := a.labs.runLesson(r.Context(), item, input)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Output string `json:"output"`
			Passed bool   `json:"passed"`
		}{output, passed})
		return
	}
	runGoLesson(w, r, item, input)
}

func runGoLesson(w http.ResponseWriter, r *http.Request, item lesson, code string) {

	tempDir, err := os.MkdirTemp("", "read-run-")
	if err != nil {
		http.Error(w, "could not create workspace", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tempDir)

	if err := os.WriteFile(tempDir+"/main.go", []byte(code), 0o600); err != nil {
		http.Error(w, "could not write program", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()

	output := &cappedBuffer{limit: maxOutputBytes}
	command := exec.CommandContext(ctx, "go", "run", "main.go")
	command.Dir = tempDir
	command.Env = append(os.Environ(), "GOWORK=off")
	command.Stdout = output
	command.Stderr = output
	err = command.Run()

	result := strings.TrimSpace(strings.ReplaceAll(output.String(), "\r\n", "\n"))
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		result = "execution timed out"
	}

	writeJSON(w, http.StatusOK, struct {
		Output string `json:"output"`
		Passed bool   `json:"passed"`
	}{
		Output: result,
		Passed: err == nil && result == normalize(item.Expected),
	})
}

type diagnostic struct {
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

func lintCode(w http.ResponseWriter, r *http.Request) {
	code, ok := readInput(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, diagnose(code))
}

func formatCode(w http.ResponseWriter, r *http.Request) {
	code, ok := readInput(w, r)
	if !ok {
		return
	}
	formatted, err := format.Source([]byte(code))
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Code string `json:"code"`
	}{string(formatted)})
}

func diagnose(code string) []diagnostic {
	fset := token.NewFileSet()
	file, parseErr := parser.ParseFile(fset, "main.go", code, parser.AllErrors)
	diagnostics := make([]diagnostic, 0)
	seen := make(map[string]bool)
	add := func(position token.Position, severity, message string) {
		line := max(position.Line, 1)
		column := max(position.Column, 1)
		key := fmt.Sprintf("%d:%d:%s", line, column, message)
		if seen[key] {
			return
		}
		seen[key] = true
		diagnostics = append(diagnostics, diagnostic{line, column, severity, message})
	}

	if parseErr != nil {
		if list, ok := parseErr.(scanner.ErrorList); ok {
			for _, item := range list {
				add(item.Pos, "error", item.Msg)
			}
		} else {
			add(token.Position{Line: 1, Column: 1}, "error", parseErr.Error())
		}
	}

	if file != nil && parseErr == nil {
		config := types.Config{
			Importer: importer.Default(),
			Error: func(err error) {
				var typeErr types.Error
				if errors.As(err, &typeErr) {
					add(fset.Position(typeErr.Pos), "error", typeErr.Msg)
					return
				}
				add(token.Position{Line: 1, Column: 1}, "error", err.Error())
			},
		}
		_, _ = config.Check("main", fset, []*ast.File{file}, nil)
	}

	if formatted, err := format.Source([]byte(code)); err == nil && !bytes.Equal(formatted, []byte(code)) {
		add(token.Position{Line: 1, Column: 1}, "warning", "Code is not gofmt formatted")
	}
	return diagnostics
}

func readInput(w http.ResponseWriter, r *http.Request) (string, bool) {
	var request struct {
		Input string `json:"input"`
		Code  string `json:"code"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxCodeBytes+1_024))
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return "", false
	}
	if request.Input == "" {
		request.Input = request.Code
	}
	if request.Input == "" || len(request.Input) > maxCodeBytes {
		http.Error(w, "input must be between 1 and 100000 bytes", http.StatusBadRequest)
		return "", false
	}
	return request.Input, true
}

func lessonMode(item lesson) string {
	if item.Mode == "" {
		return "go"
	}
	return item.Mode
}

func editorLabel(item lesson) string {
	if item.EditorLabel != "" {
		return item.EditorLabel
	}
	return "main.go"
}

func normalize(value string) string {
	return strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}

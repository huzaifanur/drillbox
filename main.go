package main

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	"strconv"
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
	Concept     string   `json:"concept,omitempty"`
	Instruction string   `json:"instruction"`
	Starter     string   `json:"starter"`
	Expected    string   `json:"expected"`
	Mode        string   `json:"mode,omitempty"`
	EditorLabel string   `json:"editorLabel,omitempty"`
	Validator   string   `json:"validator,omitempty"`
	Workspace   string   `json:"workspace,omitempty"`
	Setup       string   `json:"setup,omitempty"`
	Verify      string   `json:"verify,omitempty"`
	Columns     []string `json:"columns,omitempty"`
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
	lessons, err := loadLessons("lessons/lessons.json")
	if err != nil {
		log.Fatal(err)
	}
	trackLessons, err := loadTrack("lessons/track.json")
	if err != nil {
		log.Fatal(err)
	}
	lessons = append(lessons, trackLessons...)
	for i := range lessons {
		lessons[i].CourseID = "learn-go"
		lessons[i].CourseTitle = "Learn Go"
	}
	httpLessons, err := loadHTTPTrack("lessons/http-clients.json")
	if err != nil {
		log.Fatal(err)
	}
	lessons = append(lessons, httpLessons...)
	additionalCourses, additionalLessons, err := loadAdditionalCourses("lessons/additional-courses.json")
	if err != nil {
		log.Fatal(err)
	}
	lessons = append(lessons, additionalLessons...)
	grafanaCourse, grafanaLessons, err := loadAuthoredCourse("lessons/grafana.json")
	if err != nil {
		return err
	}
	lessons = append(lessons, grafanaLessons...)
	sqlCourse, sqlLessons := loadSQLCourse()
	lessons = append(lessons, sqlLessons...)
	prepareLessonContent(lessons)
	courses := []course{
		{ID: "learn-go", Title: "Learn Go", Description: "Go fundamentals from variables through concurrency and generics.", LessonCount: 145},
		{ID: "http-clients", Title: "Learn HTTP Clients in Go", Description: "HTTP, JSON, URLs, headers, methods, errors, and command-line clients.", LessonCount: len(httpLessons)},
	}
	courses = append(courses, additionalCourses...)
	courses = append(courses, grafanaCourse)
	courses = append(courses, sqlCourse)

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
	mux.HandleFunc("GET /api/labs/grafana", a.labs.statusHandler)
	mux.HandleFunc("POST /api/labs/grafana/start", a.labs.startHandler)
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

type trackChapter struct {
	Number  int      `json:"number"`
	Title   string   `json:"title"`
	Lessons []string `json:"lessons"`
}

type courseTrack struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Chapters    []trackChapter `json:"chapters"`
}

func loadAdditionalCourses(path string) ([]course, []lesson, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read additional courses: %w", err)
	}
	var tracks []courseTrack
	if err := json.Unmarshal(data, &tracks); err != nil {
		return nil, nil, fmt.Errorf("parse additional courses: %w", err)
	}

	courses := make([]course, 0, len(tracks))
	var lessons []lesson
	for _, track := range tracks {
		count := 0
		for _, chapter := range track.Chapters {
			for i, title := range chapter.Lessons {
				lessons = append(lessons, generateAdditionalLesson(track, chapter.Number, i+1, chapter.Title, title))
				count++
			}
		}
		courses = append(courses, course{ID: track.ID, Title: track.Title, Description: track.Description, LessonCount: count})
	}
	return courses, lessons, nil
}

func generateAdditionalLesson(track courseTrack, chapterNumber, lessonNumber int, chapter, title string) lesson {
	n := chapterNumber*100 + lessonNumber
	expected, starter, action := "", "", ""

	switch track.ID {
	case "http-servers":
		expected = fmt.Sprintf("200 checkpoint %d", n)
		starter = fmt.Sprintf(`package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

func main() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, "checkpoint", %d)
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	fmt.Println(recorder.Code, strings.TrimSpace(recorder.Body.String()))
}
`, n)
		action = "Return a successful HTTP status from the handler."
	case "pokedex":
		expected = fmt.Sprintf("pikachu %d", n)
		starter = `package main

import "fmt"

type pokemon struct {
	name  string
	level int
}

func main() {
	entry := pokemon{name: "pikachu", level: 0}
	fmt.Println(entry.name, entry.level)
}
`
		action = fmt.Sprintf("Store level %d in the Pokedex entry.", n)
	case "pubsub":
		expected = strconv.Itoa(n)
		starter = `package main

import "fmt"

func main() {
	events := make(chan int, 1)
	events <- 0
	fmt.Println(<-events)
}
`
		action = fmt.Sprintf("Publish event value %d through the channel.", n)
	case "ci-cd":
		expected = "format,test,build,deploy"
		starter = `package main

import (
	"fmt"
	"strings"
)

func main() {
	stages := []string{"format", "build", "deploy"}
	fmt.Println(strings.Join(stages, ","))
}
`
		action = "Add the missing test stage between formatting and building."
	case "http-protocol":
		expected = "GET /items HTTP/1.1"
		starter = `package main

import (
	"fmt"
	"strings"
)

func main() {
	requestLine := "POST /wrong HTTP/1.1"
	parts := strings.Fields(requestLine)
	fmt.Println(parts[0], parts[1], parts[2])
}
`
		action = "Correct the raw HTTP request line."
	case "blog-aggregator":
		expected = fmt.Sprintf("Go Feed %d", n)
		starter = `package main

import "fmt"

type feed struct {
	title string
	posts int
}

func main() {
	item := feed{title: "Go Feed", posts: 0}
	fmt.Println(item.title, item.posts)
}
`
		action = fmt.Sprintf("Record %d posts for the feed.", n)
	case "cryptography":
		message := fmt.Sprintf("checkpoint-%d", n)
		hash := sha256.Sum256([]byte(message))
		expected = fmt.Sprintf("%x", hash)
		starter = `package main

import (
	"crypto/sha256"
	"fmt"
)

func main() {
	digest := sha256.Sum256([]byte("TODO"))
	fmt.Printf("%x\n", digest)
}
`
		action = fmt.Sprintf("Hash the message %q with SHA-256.", message)
	case "file-servers":
		expected = ".png"
		starter = `package main

import (
	"fmt"
	"path/filepath"
)

func main() {
	fmt.Println(filepath.Ext("/uploads/file.txt"))
}
`
		action = "Change the uploaded filename to use the PNG extension."
	case "observability":
		expected = fmt.Sprintf("requests_total %d", n)
		starter = `package main

import "fmt"

func main() {
	metrics := map[string]int{"requests_total": 0}
	fmt.Println("requests_total", metrics["requests_total"])
}
`
		action = fmt.Sprintf("Record %d in the requests_total metric.", n)
	case "grafana":
		current := "TODO"
		switch chapterNumber {
		case 1:
			expected = "grafana-ready"
			action = "Mark the Grafana workspace as ready."
		case 2:
			expected = "prometheus http://localhost:9090"
			action = "Configure the Prometheus data source name and URL."
		case 3:
			expected = "rate(http_requests_total[5m])"
			action = "Use a five-minute rate query for the request counter."
		case 4:
			expected = fmt.Sprintf("requests-%d 6h", n)
			action = fmt.Sprintf("Name the dashboard requests-%d and set its time range to 6h.", n)
		case 5:
			expected = "HighErrorRate 5m"
			action = "Configure the high-error-rate alert with a five-minute window."
		case 6:
			expected = "GET /metrics"
			action = "Expose metrics from the standard GET /metrics endpoint."
		case 7:
			expected = "provisioned dashboards"
			action = "Mark the dashboards resource as provisioned."
		}
		starter = fmt.Sprintf(`package main

import "fmt"

func main() {
	checkpoint := %s
	fmt.Println(checkpoint)
}
`, strconv.Quote(current))
		action += "\n\nOptional live lab: run docker compose up -d, then open Grafana at http://127.0.0.1:3001."
	case "web-scraper":
		expected = fmt.Sprintf("example.com /page/%d", n)
		starter = `package main

import (
	"fmt"
	"net/url"
)

func main() {
	page, _ := url.Parse("https://wrong.local/")
	fmt.Println(page.Host, page.Path)
}
`
		action = fmt.Sprintf("Parse https://example.com/page/%d and print its host and path.", n)
	}

	return lesson{
		ID:          fmt.Sprintf("%s-%02d-%02d-%s", track.ID, chapterNumber, lessonNumber, slug(title)),
		CourseID:    track.ID,
		CourseTitle: track.Title,
		Chapter:     fmt.Sprintf("Chapter %d · %s", chapterNumber, chapter),
		Title:       title,
		Instruction: fmt.Sprintf("%s is an original local checkpoint in the %s chapter.\n\n%s\n\nExpected output:\n\n%s", title, chapter, action, expected),
		Starter:     starter,
		Expected:    expected + "\n",
	}
}

func loadHTTPTrack(path string) ([]lesson, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read HTTP track: %w", err)
	}
	var chapters []trackChapter
	if err := json.Unmarshal(data, &chapters); err != nil {
		return nil, fmt.Errorf("parse HTTP track: %w", err)
	}

	var lessons []lesson
	for _, chapter := range chapters {
		for i, title := range chapter.Lessons {
			lessons = append(lessons, generateHTTPLesson(chapter.Number, i+1, chapter.Title, title))
		}
	}
	return lessons, nil
}

func generateHTTPLesson(chapterNumber, lessonNumber int, chapter, title string) lesson {
	n := chapterNumber*100 + lessonNumber
	expected, starter, action := "", "", ""

	switch chapterNumber {
	case 1:
		expected = "GET api.example.com"
		starter = `package main

import (
	"fmt"
	"net/http"
)

func main() {
	request, _ := http.NewRequest(http.MethodPost, "https://api.example.com/items", nil)
	fmt.Println(request.Method, request.URL.Host)
}
`
		action = "Build the web request with the GET method."
	case 2:
		expected = fmt.Sprintf("ready %d", n)
		starter = fmt.Sprintf(`package main

import (
	"encoding/json"
	"fmt"
)

func main() {
	payload := []byte(%s)
	var response struct {
		Status string `+"`json:\"status\"`"+`
		Code   int    `+"`json:\"wrong\"`"+`
	}
	_ = json.Unmarshal(payload, &response)
	fmt.Println(response.Status, response.Code)
}
`, strconv.Quote(fmt.Sprintf(`{"status":"ready","code":%d}`, n)))
		action = "Correct the struct tag so both JSON fields are decoded."
	case 3:
		expected = "api.example.com 443"
		starter = `package main

import (
	"fmt"
	"net"
)

func main() {
	host, port, _ := net.SplitHostPort("api.example.com:80")
	fmt.Println(host, port)
}
`
		action = "Use the standard HTTPS port in the host address."
	case 4:
		expected = fmt.Sprintf("https api.example.com /items %d", n)
		starter = `package main

import (
	"fmt"
	"net/url"
)

func main() {
	address, _ := url.Parse("http://wrong.local/")
	fmt.Println(address.Scheme, address.Host, address.Path, address.Query().Get("page"))
}
`
		action = fmt.Sprintf("Parse https://api.example.com/items?page=%d and print its parts.", n)
	case 5:
		expected = "application/json"
		starter = `package main

import (
	"fmt"
	"net/http"
)

func main() {
	headers := http.Header{}
	headers.Set("Content-Type", "text/plain")
	fmt.Println(headers.Get("Content-Type"))
}
`
		action = "Set the Content-Type header to application/json."
	case 6:
		method := httpMethodForTitle(title)
		expected = method
		starter = `package main

import (
	"fmt"
	"net/http"
)

func main() {
	request, _ := http.NewRequest(http.MethodOptions, "https://api.example.com/items", nil)
	fmt.Println(request.Method)
}
`
		action = fmt.Sprintf("Use the %s HTTP method for this request.", method)
	case 7:
		expected = fmt.Sprintf("/todos %d", n)
		starter = `package main

import (
	"fmt"
	"net/url"
)

func main() {
	address, _ := url.Parse("https://api.example.com/wrong?limit=0")
	fmt.Println(address.Path, address.Query().Get("limit"))
}
`
		action = fmt.Sprintf("Use the /todos path and set the limit query parameter to %d.", n)
	case 8:
		expected = "https"
		starter = `package main

import (
	"fmt"
	"net/url"
)

func main() {
	address, _ := url.Parse("http://api.example.com")
	fmt.Println(address.Scheme)
}
`
		action = "Change the URL to use encrypted HTTPS."
	case 9:
		expected = "request failed"
		starter = `package main

import "fmt"

func main() {
	status := 500
	if status < 400 {
		fmt.Println("request failed")
	} else {
		fmt.Println("request succeeded")
	}
}
`
		action = "Fix the status-code check so a 500 response is treated as an error."
	case 10:
		method := httpMethodForTitle(title)
		expected = fmt.Sprintf("curl -X %s https://api.example.com/items", method)
		starter = `package main

import "fmt"

func main() {
	fmt.Println("TODO")
}
`
		action = fmt.Sprintf("Print the equivalent curl command using the %s method.", method)
	}

	return lesson{
		ID:          fmt.Sprintf("http-%02d-%02d-%s", chapterNumber, lessonNumber, slug(title)),
		CourseID:    "http-clients",
		CourseTitle: "Learn HTTP Clients in Go",
		Chapter:     fmt.Sprintf("Chapter %d · %s", chapterNumber, chapter),
		Title:       title,
		Instruction: fmt.Sprintf("%s is an original local checkpoint in the %s chapter.\n\n%s\n\nExpected output:\n\n%s", title, chapter, action, expected),
		Starter:     starter,
		Expected:    expected + "\n",
	}
}

func httpMethodForTitle(title string) string {
	upper := strings.ToUpper(title)
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if strings.Contains(upper, method) {
			return method
		}
	}
	return "GET"
}

func loadTrack(path string) ([]lesson, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read track: %w", err)
	}

	var chapters []trackChapter
	if err := json.Unmarshal(data, &chapters); err != nil {
		return nil, fmt.Errorf("parse track: %w", err)
	}

	var lessons []lesson
	for _, chapter := range chapters {
		if chapter.Number < 2 || chapter.Title == "" || len(chapter.Lessons) == 0 {
			return nil, fmt.Errorf("invalid track chapter %d", chapter.Number)
		}
		for i, title := range chapter.Lessons {
			lessons = append(lessons, generateLesson(chapter.Number, i+1, chapter.Title, title))
		}
	}
	return lessons, nil
}

var nonSlugCharacter = regexp.MustCompile(`[^a-z0-9]+`)

func generateLesson(chapterNumber, lessonNumber int, chapter, title string) lesson {
	n := chapterNumber*100 + lessonNumber
	expected := ""
	starter := ""
	action := ""

	switch chapterNumber {
	case 2:
		expected = fmt.Sprintf("constant %d", n)
		starter = `package main

import "fmt"

func main() {
	const checkpoint = 0
	fmt.Println("constant", checkpoint)
}
`
		action = fmt.Sprintf("Practice immutable values by changing checkpoint to the constant %d.", n)
	case 3:
		expected = "accepted"
		starter = fmt.Sprintf(`package main

import "fmt"

func main() {
	value := %d
	if value < 0 {
		fmt.Println("accepted")
	} else {
		fmt.Println("rejected")
	}
}
`, n)
		action = "Fix the conditional so a positive value is accepted."
	case 4:
		expected = strconv.Itoa(n * 2)
		starter = fmt.Sprintf(`package main

import "fmt"

func double(value int) int {
	return 0
}

func main() {
	fmt.Println(double(%d))
}
`, n)
		action = "Complete double so it returns twice the value it receives."
	case 5:
		expected = fmt.Sprintf("checkpoint %d", n)
		starter = fmt.Sprintf(`package main

import "fmt"

type checkpoint struct {
	name  string
	score int
}

func main() {
	item := checkpoint{name: "checkpoint", score: 0}
	fmt.Println(item.name, item.score)
}
`)
		action = fmt.Sprintf("Initialize the struct score with %d.", n)
	case 6:
		expected = fmt.Sprintf("interface %d", n)
		starter = `package main

import "fmt"

type labeler interface {
	label() string
}

type checkpoint struct{}

func (checkpoint) label() string {
	return "TODO"
}

func printLabel(value labeler) {
	fmt.Println(value.label())
}

func main() {
	printLabel(checkpoint{})
}
`
		action = fmt.Sprintf("Implement the interface method so it returns %q.", expected)
	case 7:
		expected = "negative value"
		starter = fmt.Sprintf(`package main

import (
	"errors"
	"fmt"
)

func validate(value int) error {
	if value > 0 {
		return errors.New("negative value")
	}
	return nil
}

func main() {
	if err := validate(-%d); err != nil {
		fmt.Println(err)
	}
}
`, n)
		action = "Fix the validation condition so negative values return the error."
	case 8:
		expected = strconv.Itoa(n * (n + 1) / 2)
		starter = fmt.Sprintf(`package main

import "fmt"

func main() {
	total := 0
	for i := 1; i < %d; i++ {
		total += i
	}
	fmt.Println(total)
}
`, n)
		action = fmt.Sprintf("Fix the loop so it includes every integer from 1 through %d.", n)
	case 9:
		expected = strconv.Itoa(3*n + 3)
		starter = fmt.Sprintf(`package main

import "fmt"

func main() {
	values := []int{%d, %d, %d}
	total := values[0] + values[1]
	fmt.Println(total)
}
`, n, n+1, n+2)
		action = "Use the complete slice when calculating the total."
	case 10:
		expected = strconv.Itoa(n)
		starter = `package main

import "fmt"

func main() {
	values := map[string]int{"checkpoint": 0}
	fmt.Println(values["checkpoint"])
}
`
		action = fmt.Sprintf("Store %d under the checkpoint map key.", n)
	case 11:
		expected = strconv.Itoa(n + 1)
		starter = fmt.Sprintf(`package main

import "fmt"

func increment(value *int) {
	// Update the value through the pointer.
}

func main() {
	value := %d
	increment(&value)
	fmt.Println(value)
}
`, n)
		action = "Dereference the pointer and increment the value."
	case 12:
		expected = strings.ToUpper(title)
		starter = `package main

import (
	"fmt"
	"strings"
)

func main() {
	fmt.Println(strings.ToUpper("TODO"))
}
`
		action = fmt.Sprintf("Use the strings package to uppercase %q.", title)
	case 13:
		expected = strconv.Itoa(n)
		starter = fmt.Sprintf(`package main

import "fmt"

func main() {
	values := make(chan int)
	go func() {
		values <- 0
	}()
	fmt.Println(<-values)
}
`)
		action = fmt.Sprintf("Send %d through the channel from the goroutine.", n)
	case 14:
		expected = strconv.Itoa(n * 2)
		starter = fmt.Sprintf(`package main

import (
	"fmt"
	"sync"
)

func main() {
	var mu sync.Mutex
	var wg sync.WaitGroup
	count := 0
	for worker := 0; worker < 2; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < %d; i++ {
				mu.Lock()
				count++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	fmt.Println(count)
}
`, n-1)
		action = fmt.Sprintf("Keep the mutex protection and fix each worker to count %d times.", n)
	case 15:
		expected = strconv.Itoa(n + 2)
		starter = fmt.Sprintf(`package main

import "fmt"

func last[T any](values []T) T {
	return values[0]
}

func main() {
	fmt.Println(last([]int{%d, %d, %d}))
}
`, n, n+1, n+2)
		action = "Make the generic function return the final slice element."
	case 16:
		expected = strconv.Itoa(lessonNumber)
		starter = fmt.Sprintf(`package main

import "fmt"

type status int

const (
	unknown status = iota
	ready
	running
	stopped
)

func main() {
	fmt.Println(unknown)
}
`)
		action = fmt.Sprintf("Print the enum-style constant whose numeric value is %d.", lessonNumber)
	case 17:
		expected = "clear is better than clever"
		starter = `package main

import "fmt"

func main() {
	fmt.Println("TODO")
}
`
		action = "Finish the Go proverb: clear is better than clever."
	}

	return lesson{
		ID:          fmt.Sprintf("%02d-%02d-%s", chapterNumber, lessonNumber, slug(title)),
		Chapter:     fmt.Sprintf("Chapter %d · %s", chapterNumber, chapter),
		Title:       title,
		Instruction: fmt.Sprintf("%s\n\n%s\n\nExpected output:\n\n%s", title+" is an original local checkpoint in the "+chapter+" chapter.", action, expected),
		Starter:     starter,
		Expected:    expected + "\n",
	}
}

func slug(value string) string {
	value = strings.ToLower(value)
	return strings.Trim(nonSlugCharacter.ReplaceAllString(value, "-"), "-")
}

func loadLessons(path string) ([]lesson, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read lessons: %w", err)
	}

	var lessons []lesson
	if err := json.Unmarshal(data, &lessons); err != nil {
		return nil, fmt.Errorf("parse lessons: %w", err)
	}
	if len(lessons) == 0 {
		return nil, errors.New("no lessons found")
	}

	seen := make(map[string]bool, len(lessons))
	for i, item := range lessons {
		if item.ID == "" || item.Title == "" || item.Starter == "" || item.Expected == "" {
			return nil, fmt.Errorf("lesson %d is incomplete", i+1)
		}
		if seen[item.ID] {
			return nil, fmt.Errorf("duplicate lesson id %q", item.ID)
		}
		seen[item.ID] = true
	}
	return lessons, nil
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
		Concept     string `json:"concept"`
		Instruction string `json:"instruction"`
		Starter     string `json:"starter"`
		Mode        string `json:"mode"`
		EditorLabel string `json:"editorLabel"`
	}{item.ID, item.CourseID, item.CourseTitle, item.Chapter, item.Title, item.Concept, item.Instruction, item.Starter, lessonMode(item), editorLabel(item)})
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

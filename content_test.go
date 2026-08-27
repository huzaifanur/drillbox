package main

import (
	"encoding/json"
	"go/format"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEveryLessonHasAuthoredContent(t *testing.T) {
	courses, items, err := loadAuthoredCourses("lessons/courses")
	if err != nil {
		t.Fatal(err)
	}
	sqlCourse, sqlItems := loadSQLCourse()
	courses = append(courses, sqlCourse)
	items = append(items, sqlItems...)

	want := map[string]int{
		"learn-go": 145, "http-clients": 55, "http-servers": 51, "pokedex": 13,
		"pubsub": 37, "ci-cd": 37, "http-protocol": 41, "blog-aggregator": 18,
		"cryptography": 78, "file-servers": 43, "observability": 73,
		"web-scraper": 13, "grafana": 35, "sql": 95,
	}
	if len(courses) != len(want) || len(items) != 734 {
		t.Fatalf("got %d courses and %d lessons, want %d and 734", len(courses), len(items), len(want))
	}
	for _, item := range courses {
		if item.LessonCount != want[item.ID] {
			t.Errorf("course %q has %d lessons, want %d", item.ID, item.LessonCount, want[item.ID])
		}
	}

	banned := []string{"original local checkpoint", "Run the statement against a fresh SQLite database", "Record 1103 in the requests_total metric"}
	for _, item := range items {
		if strings.TrimSpace(item.Instruction) == "" {
			t.Errorf("lesson %q has no authored instruction", item.ID)
		}
		for _, phrase := range banned {
			if strings.Contains(item.Instruction, phrase) {
				t.Errorf("lesson %q retains generated filler %q", item.ID, phrase)
			}
		}
		if lessonMode(item) == "go" {
			if item.Solution == "" {
				t.Errorf("Go lesson %q has no reference solution", item.ID)
				continue
			}
			if _, err := format.Source([]byte(item.Solution)); err != nil {
				t.Errorf("solution for %q is invalid Go: %v", item.ID, err)
			}
		}
		if lessonMode(item) == "text" {
			if normalize(item.Solution) != normalize(item.Expected) {
				t.Errorf("text solution for %q does not match expected content", item.ID)
			}
			if normalize(item.Starter) == normalize(item.Expected) {
				t.Errorf("text starter for %q already passes", item.ID)
			}
		}
	}
}

func TestLessonAPIExposesOnlyLearnerContent(t *testing.T) {
	item := lesson{ID: "one", Instruction: "Values have types. Print the value.", Starter: "package main", Expected: "secret", Solution: "hidden"}
	a := &app{byID: map[string]lesson{"one": item}}
	request := httptest.NewRequest("GET", "/api/lessons/one", nil)
	request.SetPathValue("id", "one")
	response := httptest.NewRecorder()
	a.getLesson(response, request)

	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload["instruction"] != item.Instruction {
		t.Fatalf("instruction = %#v, want %q", payload["instruction"], item.Instruction)
	}
	for _, hidden := range []string{"concept", "expected", "solution"} {
		if _, ok := payload[hidden]; ok {
			t.Errorf("lesson API exposed %q", hidden)
		}
	}
}

func TestTextLessonRunUsesExactNormalizedOutput(t *testing.T) {
	a := &app{byID: map[string]lesson{"text-one": {ID: "text-one", Mode: "text", Expected: "test\nbuild\ndeploy\n"}}}
	for _, test := range []struct {
		name   string
		input  string
		passed bool
	}{
		{name: "exact with platform newlines", input: "test\r\nbuild\r\ndeploy\r\n", passed: true},
		{name: "wrong order", input: "build\ntest\ndeploy\n", passed: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/lessons/text-one/run", strings.NewReader(`{"input":`+string(mustJSON(t, test.input))+`}`))
			request.Header.Set("Content-Type", "application/json")
			request.SetPathValue("id", "text-one")
			response := httptest.NewRecorder()
			a.runLesson(response, request)

			var result struct {
				Output string `json:"output"`
				Passed bool   `json:"passed"`
			}
			if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || result.Passed != test.passed {
				t.Fatalf("status=%d result=%#v", response.Code, result)
			}
		})
	}
}

func mustJSON(t *testing.T, value string) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

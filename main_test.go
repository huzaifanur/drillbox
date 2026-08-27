package main

import (
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLoadLessons(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lessons.json")
	data := `[{"id":"one","chapter":"Basics","title":"One","instruction":"Do it","starter":"package main","expected":"ok\n"}]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	lessons, err := loadLessons(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(lessons) != 1 || normalize(lessons[0].Expected) != "ok" {
		t.Fatalf("unexpected lessons: %#v", lessons)
	}
}

func TestNormalize(t *testing.T) {
	if got := normalize("hello\r\n\r\n"); got != "hello" {
		t.Fatalf("normalize() = %q", got)
	}
}

func TestFullTrack(t *testing.T) {
	base, err := loadLessons("lessons/lessons.json")
	if err != nil {
		t.Fatal(err)
	}
	rest, err := loadTrack("lessons/track.json")
	if err != nil {
		t.Fatal(err)
	}

	all := append(base, rest...)
	if len(all) != 145 {
		t.Fatalf("track has %d lessons, want 145", len(all))
	}
	seen := make(map[string]bool, len(all))
	for _, item := range all {
		if seen[item.ID] {
			t.Fatalf("duplicate lesson id %q", item.ID)
		}
		seen[item.ID] = true
		if item.Starter == "" || item.Expected == "" || item.Instruction == "" {
			t.Fatalf("lesson %q is incomplete", item.ID)
		}
	}
}

func TestHTTPTrack(t *testing.T) {
	lessons, err := loadHTTPTrack("lessons/http-clients.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(lessons) != 55 {
		t.Fatalf("HTTP track has %d lessons, want 55", len(lessons))
	}
	seen := make(map[string]bool, len(lessons))
	for _, item := range lessons {
		if seen[item.ID] || item.CourseID != "http-clients" || item.Starter == "" || item.Expected == "" {
			t.Fatalf("invalid HTTP lesson %#v", item)
		}
		seen[item.ID] = true
	}
}

func TestAdditionalCourses(t *testing.T) {
	courses, lessons, err := loadAdditionalCourses("lessons/additional-courses.json")
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]int{
		"http-servers":    51,
		"pokedex":         13,
		"pubsub":          37,
		"ci-cd":           37,
		"http-protocol":   41,
		"blog-aggregator": 18,
		"cryptography":    78,
		"file-servers":    43,
		"observability":   73,
		"web-scraper":     13,
	}
	if len(courses) != len(want) || len(lessons) != 404 {
		t.Fatalf("got %d courses and %d lessons", len(courses), len(lessons))
	}

	seen := make(map[string]bool, len(lessons))
	for _, item := range lessons {
		if seen[item.ID] || item.CourseID == "" || item.Instruction == "" || item.Expected == "" {
			t.Fatalf("invalid additional lesson %#v", item)
		}
		seen[item.ID] = true
		if _, err := format.Source([]byte(item.Starter)); err != nil {
			t.Fatalf("starter for %q is invalid Go: %v", item.ID, err)
		}
	}
	for _, item := range courses {
		if item.LessonCount != want[item.ID] {
			t.Fatalf("course %q has %d lessons, want %d", item.ID, item.LessonCount, want[item.ID])
		}
	}
}

func TestGrafanaCourse(t *testing.T) {
	item, lessons, err := loadAuthoredCourse("lessons/grafana.json")
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != "grafana" || item.RequiredLab != "grafana" || item.LessonCount != 35 || len(lessons) != 35 {
		t.Fatalf("unexpected Grafana course: %#v (%d lessons)", item, len(lessons))
	}

	wantModes := map[string]int{"lab": 20, "promql": 5, "go": 5, "file": 5}
	seen := make(map[string]bool, len(lessons))
	for _, lesson := range lessons {
		if seen[lesson.ID] || lesson.CourseID != "grafana" || lesson.Instruction == "" || lesson.Starter == "" {
			t.Fatalf("invalid Grafana lesson %#v", lesson)
		}
		seen[lesson.ID] = true
		wantModes[lessonMode(lesson)]--
		if lessonMode(lesson) == "go" {
			if lesson.Expected == "" {
				t.Fatalf("Go lesson %q has no expected output", lesson.ID)
			}
			if _, err := format.Source([]byte(lesson.Starter)); err != nil {
				t.Fatalf("starter for %q is invalid Go: %v", lesson.ID, err)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(lesson.Starter), 0o600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("go", "run", "main.go")
			command.Dir = dir
			command.Env = append(os.Environ(), "GOWORK=off")
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("starter for %q does not run: %s", lesson.ID, output)
			}
			if normalize(string(output)) == normalize(lesson.Expected) {
				t.Fatalf("starter for %q already passes without practice", lesson.ID)
			}
		} else if lesson.Validator == "" {
			t.Fatalf("interactive lesson %q has no validator", lesson.ID)
		}
	}
	for mode, remaining := range wantModes {
		if remaining != 0 {
			t.Fatalf("mode %q count differs by %d", mode, remaining)
		}
	}
}

func TestDiagnose(t *testing.T) {
	bad := diagnose("package main\nfunc main() { missing() }\n")
	if len(bad) == 0 || bad[0].Severity != "error" {
		t.Fatalf("expected a type error, got %#v", bad)
	}

	good := diagnose("package main\n\nfunc main() {}\n")
	if len(good) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", good)
	}
}

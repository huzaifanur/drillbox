package main

import (
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditorWrapsLongLines(t *testing.T) {
	source, err := os.ReadFile("static/editor-src.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "EditorView.lineWrapping") {
		t.Fatal("CodeMirror must wrap long lines inside the editor pane")
	}
}

func TestNormalize(t *testing.T) {
	if got := normalize("hello\r\n\r\n"); got != "hello" {
		t.Fatalf("normalize() = %q", got)
	}
}

func TestSortCoursesKeepsPreferredOrderThenUnknownTitles(t *testing.T) {
	items := []course{{ID: "custom-z", Title: "Zeta"}, {ID: "sql", Title: "SQL"}, {ID: "learn-go", Title: "Go"}, {ID: "custom-a", Title: "Alpha"}}
	sortCourses(items)
	want := []string{"learn-go", "sql", "custom-a", "custom-z"}
	for index := range want {
		if items[index].ID != want[index] {
			t.Fatalf("course %d = %q, want %q", index, items[index].ID, want[index])
		}
	}
}

func TestGrafanaCourse(t *testing.T) {
	item, lessons, err := loadAuthoredCourse("lessons/courses/grafana.json")
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

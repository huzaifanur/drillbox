package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEveryLessonHasCompactUsefulContent(t *testing.T) {
	items, err := loadLessons("lessons/lessons.json")
	if err != nil {
		t.Fatal(err)
	}
	rest, err := loadTrack("lessons/track.json")
	if err != nil {
		t.Fatal(err)
	}
	items = append(items, rest...)
	for i := range items {
		items[i].CourseID = "learn-go"
		items[i].CourseTitle = "Learn Go"
	}
	httpItems, err := loadHTTPTrack("lessons/http-clients.json")
	if err != nil {
		t.Fatal(err)
	}
	items = append(items, httpItems...)
	_, additional, err := loadAdditionalCourses("lessons/additional-courses.json")
	if err != nil {
		t.Fatal(err)
	}
	items = append(items, additional...)
	_, grafanaItems, err := loadAuthoredCourse("lessons/grafana.json")
	if err != nil {
		t.Fatal(err)
	}
	items = append(items, grafanaItems...)
	_, sqlItems := loadSQLCourse()
	items = append(items, sqlItems...)
	prepareLessonContent(items)

	if len(items) != 734 {
		t.Fatalf("got %d lessons, want 734", len(items))
	}
	for _, item := range items {
		if item.Concept == "" || item.Instruction == "" {
			t.Errorf("lesson %q has incomplete content", item.ID)
		}
		if chapterConcepts[item.CourseID][chapterTitle(item.Chapter)] == "" {
			t.Errorf("lesson %q has no chapter-specific concept", item.ID)
		}
		if words := len(strings.Fields(item.Concept)); words > 24 {
			t.Errorf("lesson %q concept has %d words: %q", item.ID, words, item.Concept)
		}
		if words := len(strings.Fields(item.Instruction)); words > 32 {
			t.Errorf("lesson %q task has %d words: %q", item.ID, words, item.Instruction)
		}
		if strings.Contains(item.Instruction, generatedLead) || strings.Contains(item.Instruction, sqlBoilerplate) {
			t.Errorf("lesson %q retains platform boilerplate: %q", item.ID, item.Instruction)
		}
	}
}

func TestLessonAPIIncludesConcept(t *testing.T) {
	item := lesson{ID: "one", Concept: "Values have types.", Instruction: "Print the value.", Starter: "package main"}
	a := &app{byID: map[string]lesson{"one": item}}
	request := httptest.NewRequest("GET", "/api/lessons/one", nil)
	request.SetPathValue("id", "one")
	response := httptest.NewRecorder()
	a.getLesson(response, request)

	var payload struct {
		Concept string `json:"concept"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Concept != item.Concept {
		t.Fatalf("concept = %q, want %q", payload.Concept, item.Concept)
	}
}

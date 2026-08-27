package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSQLCourse(t *testing.T) {
	item, lessons := loadSQLCourse()
	if item.ID != "sql" || item.LessonCount != 95 || len(lessons) != 95 {
		t.Fatalf("unexpected SQL course: %#v (%d lessons)", item, len(lessons))
	}
	chapters := map[string]bool{}
	seen := map[string]bool{}
	for _, lesson := range lessons {
		if seen[lesson.ID] || lesson.CourseID != "sql" || lessonMode(lesson) != "sql" || lesson.Starter == "" || lesson.Instruction == "" {
			t.Fatalf("invalid SQL lesson: %#v", lesson)
		}
		seen[lesson.ID] = true
		chapters[lesson.Chapter] = true
	}
	if len(chapters) != 11 {
		t.Fatalf("SQL course has %d chapters, want 11", len(chapters))
	}
}

func TestSQLRunnerChecksRowsColumnsAndSchema(t *testing.T) {
	_, lessons := loadSQLCourse()

	result := runSQLForTest(t, lessons[0], `SELECT name FROM users ORDER BY id;`)
	if !result.Passed || !strings.Contains(result.Output, "Ada") {
		t.Fatalf("query result: %#v", result)
	}

	aliasLesson := findSQLLesson(t, lessons, "AS Clause in SQL")
	if result := runSQLForTest(t, aliasLesson, `SELECT name FROM users WHERE id=1;`); result.Passed {
		t.Fatal("query with the wrong result column alias passed")
	}
	if result := runSQLForTest(t, aliasLesson, `SELECT name AS full_name FROM users WHERE id=1;`); !result.Passed {
		t.Fatalf("correct alias failed: %#v", result)
	}

	tableLesson := findSQLLesson(t, lessons, "Creating a Table")
	result = runSQLForTest(t, tableLesson, `CREATE TABLE payments (id INTEGER, amount REAL);`)
	if !result.Passed {
		t.Fatalf("schema result: %#v", result)
	}
}

func TestSQLRunnerReturnsUsefulErrors(t *testing.T) {
	_, lessons := loadSQLCourse()
	result := runSQLForTest(t, lessons[0], `SELECT FROM;`)
	if result.Passed || !strings.Contains(result.Output, "SQL error:") {
		t.Fatalf("error result: %#v", result)
	}
}

func TestSQLStartersRequirePractice(t *testing.T) {
	_, lessons := loadSQLCourse()
	for _, lesson := range lessons {
		if result := runSQLForTest(t, lesson, lesson.Starter); result.Passed {
			t.Fatalf("starter for %q already passes", lesson.ID)
		}
	}
}

func TestSQLRepresentativeSolutions(t *testing.T) {
	_, lessons := loadSQLCourse()
	cases := []struct {
		title string
		sql   string
	}{
		{"Constraints", `CREATE TABLE invites (email TEXT NOT NULL UNIQUE);`},
		{"DELETE", `DELETE FROM transactions WHERE status='pending';`},
		{"Between", `SELECT name FROM users WHERE age BETWEEN 30 AND 45 ORDER BY age;`},
		{"ORDER BY and LIMIT", `SELECT name, balance FROM users ORDER BY balance DESC LIMIT 2;`},
		{"SUM", `SELECT sum(amount) AS total_amount FROM transactions;`},
		{"Subqueries", `SELECT name FROM users WHERE id IN (SELECT user_id FROM transactions WHERE amount > 50) ORDER BY id;`},
		{"Normalization Review", `SELECT name FROM sqlite_master WHERE type='table' AND name IN ('users','banks','accounts') ORDER BY name;`},
		{"Joins", `SELECT u.name AS user_name, c.name AS country_name FROM users u JOIN countries c ON c.code=u.country_code ORDER BY u.id;`},
		{"SQL Indexes", `CREATE INDEX idx_transactions_user ON transactions(user_id);`},
	}
	for _, test := range cases {
		lesson := findSQLLesson(t, lessons, test.title)
		if result := runSQLForTest(t, lesson, test.sql); !result.Passed {
			t.Errorf("solution for %q failed: %#v", test.title, result)
		}
	}
}

func TestSQLLintUsesLessonSchema(t *testing.T) {
	_, lessons := loadSQLCourse()
	item := lessons[0]
	application := &app{byID: map[string]lesson{item.ID: item}}

	lint := func(input string) []diagnostic {
		body, _ := json.Marshal(map[string]string{"input": input})
		request := httptest.NewRequest("POST", "/api/lessons/"+item.ID+"/lint", strings.NewReader(string(body)))
		request.SetPathValue("id", item.ID)
		recorder := httptest.NewRecorder()
		application.lintLesson(recorder, request)
		var diagnostics []diagnostic
		if err := json.Unmarshal(recorder.Body.Bytes(), &diagnostics); err != nil {
			t.Fatalf("decode diagnostics: %v (%s)", err, recorder.Body.String())
		}
		return diagnostics
	}

	if diagnostics := lint(`SELECT name FROM users;`); len(diagnostics) != 0 {
		t.Fatalf("valid SQL diagnostics: %#v", diagnostics)
	}
	diagnostics := lint(`SELECT name FROM people;`)
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "no such table") {
		t.Fatalf("invalid SQL diagnostics: %#v", diagnostics)
	}
}

type sqlTestResult struct {
	Output string `json:"output"`
	Passed bool   `json:"passed"`
}

func runSQLForTest(t *testing.T, lesson lesson, input string) sqlTestResult {
	t.Helper()
	request := httptest.NewRequest("POST", "/", nil)
	recorder := httptest.NewRecorder()
	runSQLLesson(recorder, request, lesson, input)
	var result sqlTestResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode SQL response: %v (%s)", err, recorder.Body.String())
	}
	return result
}

func findSQLLesson(t *testing.T, lessons []lesson, title string) lesson {
	t.Helper()
	for _, lesson := range lessons {
		if lesson.Title == title {
			return lesson
		}
	}
	t.Fatalf("SQL lesson %q not found", title)
	return lesson{}
}

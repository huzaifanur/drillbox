package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"time"
)

type sqlTask struct {
	Title    string
	Goal     string
	Starter  string
	Expected string
	Verify   string
	Setup    string
	Columns  []string
	Fresh    bool
}

type sqlChapter struct {
	Title string
	Tasks []sqlTask
}

func query(title, goal, starter, expected string, columns ...string) sqlTask {
	return sqlTask{Title: title, Goal: goal, Starter: starter, Expected: expected, Columns: columns}
}

func verify(title, goal, starter, check, expected string) sqlTask {
	return sqlTask{Title: title, Goal: goal, Starter: starter, Verify: check, Expected: expected}
}

func schema(title, goal, setup, starter, check, expected string) sqlTask {
	return sqlTask{Title: title, Goal: goal, Setup: setup, Starter: starter, Verify: check, Expected: expected, Fresh: true}
}

func fixtureQuery(title, goal, setup, starter, expected string, columns ...string) sqlTask {
	return sqlTask{Title: title, Goal: goal, Setup: setup, Starter: starter, Expected: expected, Columns: columns, Fresh: true}
}

func loadSQLCourse() (course, []lesson) {
	chapters := sqlCurriculum()
	items := make([]lesson, 0, 95)
	for chapterIndex, chapter := range chapters {
		for lessonIndex, task := range chapter.Tasks {
			setup := cashPalSchema
			if task.Fresh {
				setup = task.Setup
			}
			items = append(items, lesson{
				ID:          fmt.Sprintf("sql-%02d-%02d-%s", chapterIndex+1, lessonIndex+1, slug(task.Title)),
				CourseID:    "sql",
				CourseTitle: "Learn SQL",
				Chapter:     fmt.Sprintf("Chapter %d · %s", chapterIndex+1, chapter.Title),
				Title:       task.Title,
				Instruction: task.Goal,
				Starter:     task.Starter,
				Expected:    task.Expected,
				Mode:        "sql",
				EditorLabel: "query.sql",
				Setup:       setup,
				Verify:      task.Verify,
				Columns:     task.Columns,
			})
		}
	}
	return course{
		ID:          "sql",
		Title:       "Learn SQL",
		Description: "Query, model, join, aggregate, normalize, and tune relational data with real SQLite exercises.",
		LessonCount: len(items),
	}, items
}

func runSQLLesson(w http.ResponseWriter, r *http.Request, item lesson, input string) {
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	result, err := executeSQLite(ctx, item, input)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		writeJSON(w, http.StatusOK, struct {
			Output string `json:"output"`
			Passed bool   `json:"passed"`
		}{"SQL execution timed out", false})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, struct {
			Output string `json:"output"`
			Passed bool   `json:"passed"`
		}{err.Error(), false})
		return
	}
	if result.Error != "" {
		writeJSON(w, http.StatusOK, struct {
			Output string `json:"output"`
			Passed bool   `json:"passed"`
		}{"SQL error: " + result.Error, false})
		return
	}
	display := result.Output
	if item.Verify != "" {
		display = "Verification\n" + result.Output
	} else if len(result.Columns) > 0 {
		display = strings.Join(result.Columns, " | ") + "\n" + display
	}
	columnsMatch := len(item.Columns) == 0 || slices.Equal(item.Columns, result.Columns)
	writeJSON(w, http.StatusOK, struct {
		Output string `json:"output"`
		Passed bool   `json:"passed"`
	}{normalize(display), columnsMatch && normalize(result.Output) == normalize(item.Expected)})
}

type sqliteResult struct {
	Columns []string `json:"columns"`
	Output  string   `json:"output"`
	Error   string   `json:"error"`
}

func executeSQLite(ctx context.Context, item lesson, input string) (sqliteResult, error) {
	payload, _ := json.Marshal(map[string]string{"setup": item.Setup, "input": input, "verify": item.Verify})
	output := &cappedBuffer{limit: maxOutputBytes}
	command := exec.CommandContext(ctx, "python3", "-c", sqliteRunner)
	command.Stdin = strings.NewReader(string(payload))
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	var result sqliteResult
	if err != nil || json.Unmarshal(output.Bytes(), &result) != nil {
		return sqliteResult{}, fmt.Errorf("SQLite runner: %s", normalize(output.String()))
	}
	return result, nil
}

func (a *app) lintLesson(w http.ResponseWriter, r *http.Request) {
	item, ok := a.byID[r.PathValue("id")]
	if !ok || lessonMode(item) != "sql" {
		http.Error(w, "SQL lesson not found", http.StatusNotFound)
		return
	}
	input, ok := readInput(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	result, err := executeSQLite(ctx, item, input)
	diagnostics := []diagnostic{}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		diagnostics = append(diagnostics, diagnostic{Line: 1, Column: 1, Severity: "error", Message: "SQLite lint timed out"})
	} else if err != nil {
		diagnostics = append(diagnostics, diagnostic{Line: 1, Column: 1, Severity: "error", Message: err.Error()})
	} else if result.Error != "" {
		diagnostics = append(diagnostics, diagnostic{Line: 1, Column: 1, Severity: "error", Message: result.Error})
	}
	writeJSON(w, http.StatusOK, diagnostics)
}

const sqliteRunner = `
import json, sqlite3, sys
p = json.load(sys.stdin)
try:
    db = sqlite3.connect(':memory:')
    db.execute('PRAGMA foreign_keys = ON')
    db.executescript(p['setup'])
    if p['verify']:
        db.executescript(p['input'])
        cur = db.execute(p['verify'])
    else:
        cur = db.execute(p['input'])
    columns = [d[0] for d in cur.description] if cur.description else []
    rows = cur.fetchall() if cur.description else []
    def cell(v):
        if v is None: return 'NULL'
        if isinstance(v, bytes): return v.hex()
        return str(v)
    output = '\n'.join('|'.join(cell(v) for v in row) for row in rows)
    print(json.dumps({'columns': columns, 'output': output}))
except Exception as e:
    print(json.dumps({'error': str(e)}))
`

const cashPalSchema = `
CREATE TABLE countries (code TEXT PRIMARY KEY, name TEXT NOT NULL);
INSERT INTO countries VALUES
  ('DE','Germany'),('FI','Finland'),('GB','United Kingdom'),('PK','Pakistan'),('US','United States');

CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  email TEXT UNIQUE,
  country_code TEXT REFERENCES countries(code),
  age INTEGER NOT NULL,
  active INTEGER NOT NULL DEFAULT 1,
  balance REAL,
  manager_id INTEGER REFERENCES users(id)
);
INSERT INTO users VALUES
  (1,'Ada','ada@example.com','GB',36,1,120.5,NULL),
  (2,'Linus','linus@example.com','FI',55,1,80,1),
  (3,'Grace','grace@example.com','US',45,0,200,1),
  (4,'Ali','ali@example.com','PK',28,1,50,2),
  (5,'Noor',NULL,'PK',31,1,NULL,2);

CREATE TABLE transactions (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id),
  amount REAL NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL
);
INSERT INTO transactions VALUES
  (1,1,25,'completed','2026-01-01'),
  (2,1,15,'pending','2026-01-02'),
  (3,2,70,'completed','2026-01-03'),
  (4,3,110,'failed','2026-01-04'),
  (5,4,40,'completed','2026-01-05'),
  (6,4,60,'completed','2026-01-06');

CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id), title TEXT NOT NULL, status TEXT NOT NULL);
INSERT INTO posts VALUES (1,1,'SQLite in Practice','published'),(2,1,'Indexes','draft'),(3,4,'Safe Queries','published');
CREATE TABLE banks (id INTEGER PRIMARY KEY, name TEXT NOT NULL, country_code TEXT REFERENCES countries(code));
INSERT INTO banks VALUES (1,'Atlas Bank','PK'),(2,'North Bank','US');
CREATE TABLE accounts (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id), bank_id INTEGER REFERENCES banks(id), balance REAL NOT NULL);
INSERT INTO accounts VALUES (1,1,1,500),(2,2,2,300),(3,4,1,150);
CREATE TABLE tickets (id INTEGER PRIMARY KEY, user_id INTEGER REFERENCES users(id), subject TEXT NOT NULL, status TEXT NOT NULL, priority INTEGER NOT NULL);
INSERT INTO tickets VALUES (1,1,'Card declined','open',3),(2,4,'Wrong balance','open',2),(3,2,'Close account','closed',1);
`

func sqlCurriculum() []sqlChapter {
	return []sqlChapter{
		{Title: "Introduction", Tasks: []sqlTask{
			query("Welcome to Learn SQL", "SELECT reads columns from a table, and ORDER BY makes row order deterministic. Fix the table name and return every user name in id order.", `SELECT name FROM people ORDER BY id;`, "Ada\nLinus\nGrace\nAli\nNoor", "name"),
			query("Select Single Column", "Projection returns only the columns named after SELECT. Select email from users, exclude NULL values, and order by id.", `SELECT name FROM users;`, "ada@example.com\nlinus@example.com\ngrace@example.com\nali@example.com", "email"),
			query("Select Multiple Columns", "A SELECT list can return several columns in a defined order. Return name and age for user id 1.", `SELECT name FROM users WHERE id = 1;`, "Ada|36", "name", "age"),
			query("What Is SQL?", "SQL describes the result you want rather than the steps used to find it. Return active users younger than 35 in id order.", `SELECT * FROM users;`, "Ali\nNoor", "name"),
			query("Which Databases Use SQL?", "SQLite exposes the runtime storage class of a value through typeof(). Return the storage classes of active and balance for user id 1 as active_type and balance_type.", `SELECT active, balance FROM users WHERE id = 1;`, "integer|real", "active_type", "balance_type"),
			query("NoSQL vs. SQL", "Relational databases connect rows through shared keys. Use country_code to return each Pakistani user's name in id order.", `SELECT * FROM users WHERE country_code = 'PK';`, "Ali\nNoor", "name"),
			query("Comparing SQL Databases", "Unlike stricter SQL engines, SQLite determines a literal's storage class at runtime. Use typeof() on integer 42 and text '42', aliased as number_type and text_type.", `SELECT 42, '42';`, "integer|text", "number_type", "text_type"),
		}},
		{Title: "Tables", Tasks: []sqlTask{
			schema("Creating a Table", "CREATE TABLE defines a table and each column's declared type. Create payments with id INTEGER and amount REAL, in that order.", "", `CREATE TABLE TODO;`, `SELECT group_concat(name || ':' || type, ',') FROM pragma_table_info('payments');`, "id:INTEGER,amount:REAL"),
			schema("Create Table Practice", "Each table needs its own CREATE TABLE statement. Create customers(id INTEGER) and orders(id INTEGER, customer_id INTEGER).", "", `CREATE TABLE customers (TODO);`, `SELECT (SELECT group_concat(name || ':' || type, ',') FROM pragma_table_info('customers')) || '|' || (SELECT group_concat(name || ':' || type, ',') FROM pragma_table_info('orders'));`, "id:INTEGER|id:INTEGER,customer_id:INTEGER"),
			schema("Altering Tables", "ALTER TABLE ... ADD COLUMN extends an existing table without recreating it. Add nickname TEXT to users.", `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`, `ALTER TABLE users ADD COLUMN TODO;`, `SELECT name || ':' || type FROM pragma_table_info('users') WHERE name='nickname';`, "nickname:TEXT"),
			schema("Intro to Migrations", "A migration records a repeatable schema change; its version identifies what has run. Create schema_migrations with version INTEGER PRIMARY KEY.", "", `CREATE TABLE schema_migrations (TODO);`, `SELECT name || ':' || type || ':' || pk FROM pragma_table_info('schema_migrations');`, "version:INTEGER:1"),
			schema("Up Migration", "An up migration applies the next schema change. Create refunds with id INTEGER PRIMARY KEY and amount REAL NOT NULL.", "", `CREATE TABLE refunds (TODO);`, `SELECT group_concat(name || ':' || type || ':' || pk || ':' || "notnull", ',') FROM pragma_table_info('refunds');`, "id:INTEGER:1:0,amount:REAL:0:1"),
			schema("Down Migration", "A down migration reverses an earlier schema change. Drop legacy_payments.", `CREATE TABLE legacy_payments (id INTEGER);`, `DROP TABLE TODO;`, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='legacy_payments';`, "0"),
			schema("Migration Review", "One migration can contain several ordered DDL statements. Create profiles with id INTEGER PRIMARY KEY, then add bio TEXT.", "", `CREATE TABLE profiles (id INTEGER PRIMARY KEY);`, `SELECT group_concat(name || ':' || type || ':' || pk, ',') FROM pragma_table_info('profiles');`, "id:INTEGER:1,bio:TEXT:0"),
			schema("SQL Data Types", "SQLite's common declared types include INTEGER, TEXT, REAL, and BLOB. Create events with id, title, price, and payload using those types in that order.", "", `CREATE TABLE events (id INTEGER);`, `SELECT group_concat(name || ':' || type, ',') FROM pragma_table_info('events');`, "id:INTEGER,title:TEXT,price:REAL,payload:BLOB"),
			schema("Practice – Posts Table", "PRIMARY KEY identifies a row, while NOT NULL rejects missing values. Create posts with id INTEGER PRIMARY KEY and required TEXT columns title and body.", "", `CREATE TABLE posts (TODO);`, `SELECT group_concat(name || ':' || type || ':' || pk || ':' || "notnull", ',') FROM pragma_table_info('posts');`, "id:INTEGER:1:0,title:TEXT:0:1,body:TEXT:0:1"),
			schema("Practice – Posts Table Migration", "A NOT NULL column added to populated tables needs a usable default. Add status TEXT NOT NULL DEFAULT 'draft' to posts.", `CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT NOT NULL, body TEXT NOT NULL);`, `ALTER TABLE posts ADD COLUMN TODO;`, `SELECT name || ':' || type || ':' || "notnull" || ':' || dflt_value FROM pragma_table_info('posts') WHERE name='status';`, "status:TEXT:1:'draft'"),
		}},
		{Title: "Constraints", Tasks: []sqlTask{
			query("Null Values", "NULL means missing, so it is tested with IS NULL rather than equality. Return every user whose email is NULL.", `SELECT name FROM users WHERE email = NULL;`, "Noor", "name"),
			schema("Constraints", "NOT NULL requires a value and UNIQUE prevents duplicates. Create invites with an email TEXT column using both constraints.", "", `CREATE TABLE invites (email TEXT);`, `SELECT (SELECT "notnull" FROM pragma_table_info('invites') WHERE name='email') || '|' || (SELECT count(*) FROM pragma_index_list('invites') WHERE "unique"=1);`, "1|1"),
			schema("Primary Keys", "A primary key uniquely identifies each row. Create tags with id INTEGER PRIMARY KEY and name TEXT NOT NULL.", "", `CREATE TABLE tags (id INTEGER, name TEXT);`, `SELECT group_concat(name || ':' || type || ':' || pk || ':' || "notnull", ',') FROM pragma_table_info('tags');`, "id:INTEGER:1:0,name:TEXT:0:1"),
			schema("Foreign Keys", "A foreign key prevents references to missing parent rows when enforcement is enabled. Create comments with id INTEGER PRIMARY KEY and post_id INTEGER referencing posts(id).", `CREATE TABLE posts (id INTEGER PRIMARY KEY);`, `CREATE TABLE comments (id INTEGER PRIMARY KEY, post_id INTEGER);`, `SELECT (SELECT pk FROM pragma_table_info('comments') WHERE name='id') || '|' || (SELECT "table" || ':' || "from" || ':' || "to" FROM pragma_foreign_key_list('comments'));`, "1|posts:post_id:id"),
			query("Schema", "SQLite records schema objects in sqlite_master; internal objects use sqlite_ names. List application table names alphabetically.", `SELECT * FROM sqlite_master;`, "accounts\nbanks\ncountries\nposts\ntickets\ntransactions\nusers", "name"),
			query("Relational Databases", "A join follows related keys to combine columns from different tables. Join users to countries and return Ada as user_name with her country_name.", `SELECT name FROM users WHERE id=1;`, "Ada|United Kingdom", "user_name", "country_name"),
			query("Relational vs. Non-Relational DBs", "Foreign-key relationships make connected records queryable without embedding them together. Count users that have a matching country, aliased as related_users.", `SELECT count(*) FROM users;`, "5", "related_users"),
		}},
		{Title: "CRUD", Tasks: []sqlTask{
			verify("CRUD", "INSERT creates rows, completing the create part of CRUD. Insert user 6: Mira, mira@example.com, FI, age 24, active 1, balance 10, manager 2.", `SELECT * FROM users;`, `SELECT id || '|' || name || '|' || email || '|' || country_code || '|' || age || '|' || active || '|' || balance || '|' || manager_id FROM users WHERE id=6;`, "6|Mira|mira@example.com|FI|24|1|10.0|2"),
			verify("Insert Statement", "An INSERT column list makes value order explicit. Insert transaction 7 for user 5, amount 35, status completed, created on 2026-01-07.", `INSERT INTO transactions TODO;`, `SELECT id || '|' || user_id || '|' || amount || '|' || status || '|' || created_at FROM transactions WHERE id=7;`, "7|5|35.0|completed|2026-01-07"),
			verify("Auto Increment", "An INTEGER PRIMARY KEY receives the next row id when omitted. Insert a completed transaction for user 2, amount 20, dated 2026-01-07 without specifying id.", `INSERT INTO transactions VALUES (TODO);`, `SELECT id || '|' || user_id || '|' || amount || '|' || status || '|' || created_at FROM transactions ORDER BY id DESC LIMIT 1;`, "7|2|20.0|completed|2026-01-07"),
			verify("Manual Entry", "A column list can mix explicit values with NULL. Insert user id 10 named Sam with email sam@example.com, country US, age 40, active 1, balance 0, and no manager.", `INSERT INTO users (name) VALUES ('Sam');`, `SELECT id || '|' || name || '|' || email || '|' || country_code || '|' || age || '|' || active || '|' || balance || '|' || coalesce(manager_id, 'NULL') FROM users WHERE id=10;`, "10|Sam|sam@example.com|US|40|1|0.0|NULL"),
			query("Count", "COUNT(*) returns the number of rows, including rows containing NULL values. Count all users as user_count.", `SELECT * FROM users;`, "5", "user_count"),
			query("WHERE Clause", "WHERE filters rows before they reach the result. Return active user names older than 35 in id order.", `SELECT name FROM users;`, "Ada\nLinus", "name"),
			query("Finding NULL Values", "Missing values do not compare equal to anything, including NULL. Return users with no balance using IS NULL.", `SELECT name FROM users WHERE balance = NULL;`, "Noor", "name"),
			verify("DELETE", "DELETE permanently removes every row matched by its WHERE clause. Delete only pending transactions and preserve every other row.", `DELETE FROM transactions;`, `SELECT sum(status='pending') || '|' || count(*) FROM transactions;`, "0|5"),
			verify("Danger of Deleting Data", "A missing or reversed DELETE predicate can erase valid data. Delete only failed transactions and preserve every other row.", `DELETE FROM transactions WHERE status != 'failed';`, `SELECT count(*) || '|' || sum(status='failed') FROM transactions;`, "5|0"),
			verify("Update Query in SQL", "UPDATE changes matched rows; expressions can use the old column value. Increase only Ali's balance by 25.", `UPDATE users SET balance = 25;`, `SELECT group_concat(id || ':' || coalesce(balance, 'NULL'), ',') FROM users;`, "1:120.5,2:80.0,3:200.0,4:75.0,5:NULL"),
			query("Object-Relational Mapping (ORMs)", "ORMs translate object operations into SQL, but the database still executes a query. Fetch user id 3's name and email directly.", `SELECT * FROM users;`, "Grace|grace@example.com", "name", "email"),
			query("Query Practice – User Count", "Grouping produces one aggregate result per key. Count active users per country as user_count, sorted by country_code.", `SELECT count(*) FROM users;`, "FI|1\nGB|1\nPK|2", "country_code", "user_count"),
			query("Query Practice – Country Codes", "NOT EXISTS can find parent rows with no related child. Return country codes that have no users, sorted.", `SELECT code FROM countries;`, "DE", "code"),
		}},
		{Title: "Basic Queries", Tasks: []sqlTask{
			query("AS Clause in SQL", "AS gives a result column a stable name without changing stored data. Return user 1's name aliased as full_name.", `SELECT name FROM users WHERE id=1;`, "Ada", "full_name"),
			query("SQL Functions", "Scalar functions transform each selected value. Return Ada's name through LOWER(), aliased as lower_name.", `SELECT name FROM users WHERE id=1;`, "ada", "lower_name"),
			query("Between", "BETWEEN includes both boundary values. Return names aged 30 through 45, ordered by age.", `SELECT name FROM users;`, "Noor\nAda\nGrace", "name"),
			query("Distinct", "DISTINCT removes duplicate result rows after selection. Return unique user country codes alphabetically.", `SELECT country_code FROM users;`, "FI\nGB\nPK\nUS", "country_code"),
			query("Logical Operators – AND", "AND requires both predicates to be true. Return active users aged at least 30 in id order.", `SELECT name FROM users WHERE active=1;`, "Ada\nLinus\nNoor", "name"),
			query("OR", "OR keeps a row when either predicate is true. Return users who are inactive or younger than 30 in id order.", `SELECT name FROM users;`, "Grace\nAli", "name"),
			query("In", "IN compares one value against a compact list of alternatives. Return users from PK or US in id order.", `SELECT name FROM users WHERE country_code='PK';`, "Grace\nAli\nNoor", "name"),
			query("Like", "LIKE uses % for any-length text; NULL never matches a pattern. Return emails ending in @example.com in user id order.", `SELECT email FROM users;`, "ada@example.com\nlinus@example.com\ngrace@example.com\nali@example.com", "email"),
			query("Underscore Operator", "The LIKE underscore wildcard matches exactly one character. Return three-letter names beginning with A in id order.", `SELECT name FROM users WHERE name LIKE '%a%';`, "Ada\nAli", "name"),
			query("Wildcards Quiz", "Multiple LIKE predicates can be combined with OR. Return names beginning with G or L, alphabetically.", `SELECT name FROM users;`, "Grace\nLinus", "name"),
			query("Query Practice – Discount Program", "Combine predicates carefully: NULL balances must be excluded before sorting. Return active users below 100 with name and balance, cheapest first.", `SELECT name FROM users WHERE active=1;`, "Ali|50.0\nLinus|80.0", "name", "balance"),
		}},
		{Title: "Structuring", Tasks: []sqlTask{
			query("LIMIT", "LIMIT caps the number of rows after ordering. Return the first two user names by id.", `SELECT name FROM users ORDER BY id;`, "Ada\nLinus", "name"),
			query("Limit Quiz", "OFFSET skips rows from an ordered result before LIMIT is applied. Skip the first two users and return the next two by id.", `SELECT name FROM users ORDER BY id LIMIT 2;`, "Grace\nAli", "name"),
			query("ORDER BY", "ORDER BY can sort descending; in SQLite, NULL values come last under DESC. Return names by balance descending.", `SELECT name FROM users;`, "Grace\nAda\nLinus\nAli\nNoor", "name"),
			query("ORDER BY Quiz", "Additional ORDER BY expressions break ties from left to right. Sort users by active descending, then age ascending.", `SELECT name FROM users ORDER BY active;`, "Ali\nNoor\nAda\nLinus\nGrace", "name"),
			query("ORDER BY and LIMIT", "Ordering before limiting selects the top rows rather than arbitrary rows. Return name and balance for the two highest balances.", `SELECT name FROM users LIMIT 2;`, "Grace|200.0\nAda|120.5", "name", "balance"),
			query("Query Practice – Security Breach", "A deterministic top-one query filters, sorts, then limits. Return the subject and priority of the highest-priority open ticket.", `SELECT subject FROM tickets;`, "Card declined|3", "subject", "priority"),
		}},
		{Title: "Aggregations", Tasks: []sqlTask{
			query("What Are Aggregations?", "An aggregate reduces many input rows to one value. Count transactions and alias the result transaction_count.", `SELECT * FROM transactions;`, "6", "transaction_count"),
			query("SUM", "SUM adds the non-NULL values in a column. Calculate total transaction amount as total_amount.", `SELECT amount FROM transactions;`, "320.0", "total_amount"),
			query("MAX", "MAX returns the greatest non-NULL value in a group. Return the largest transaction amount as max_amount.", `SELECT amount FROM transactions;`, "110.0", "max_amount"),
			query("MIN", "MIN returns the smallest non-NULL value in a group. Return the smallest transaction amount as min_amount.", `SELECT amount FROM transactions;`, "15.0", "min_amount"),
			query("GROUP BY", "GROUP BY creates one aggregate group for each distinct key. Count transactions per status as transaction_count, sorted by status.", `SELECT status FROM transactions;`, "completed|4\nfailed|1\npending|1", "status", "transaction_count"),
			query("Average", "AVG ignores NULL inputs and returns the arithmetic mean. Calculate user age as average_age.", `SELECT age FROM users;`, "39.0", "average_age"),
			query("HAVING", "HAVING filters completed groups, unlike WHERE which filters input rows. Return country codes with at least two users and their user_count.", `SELECT country_code, count(*) FROM users GROUP BY country_code;`, "PK|2", "country_code", "user_count"),
			query("HAVING vs. WHERE in SQL", "WHERE must remove inactive rows before grouping; HAVING then tests each aggregate. Return countries with at least two active users.", `SELECT country_code FROM users GROUP BY country_code;`, "PK|2", "country_code", "active_users"),
			query("ROUND", "ROUND(value, places) controls displayed decimal precision. Round average transaction amount to two places as average_amount.", `SELECT avg(amount) FROM transactions;`, "53.33", "average_amount"),
			query("Query Practice – Average Ages", "Grouped averages combine GROUP BY, AVG, and ROUND. Return each country_code and average age rounded to one decimal, sorted by code.", `SELECT country_code, age FROM users;`, "FI|55.0\nGB|36.0\nPK|29.5\nUS|45.0", "country_code", "average_age"),
		}},
		{Title: "Subqueries", Tasks: []sqlTask{
			query("Subqueries", "A subquery can produce a set for an outer predicate. Return users who have any transaction over 50, in id order.", `SELECT name FROM users;`, "Linus\nGrace\nAli", "name"),
			query("Subqueries", "A scalar subquery must produce one value where an expression is expected. Use one to return the name attached to the largest transaction.", `SELECT max(amount) FROM transactions;`, "Grace", "name"),
			query("Query Practice – Invoice Subquery", "An aggregate subquery can identify grouped keys for an outer query. Return users whose total transaction amount exceeds 50, in id order.", `SELECT name FROM users;`, "Linus\nGrace\nAli", "name"),
			query("No Tables", "SQLite can evaluate constant expressions without a FROM clause, including scalar subqueries. Return 2 + 3 from a scalar subquery as answer.", `SELECT 2 + 3;`, "5", "answer"),
			query("Quiz", "NOT EXISTS is true when a correlated subquery finds no row. Return users with no transactions, in id order.", `SELECT name FROM users;`, "Noor", "name"),
		}},
		{Title: "Normalization", Tasks: []sqlTask{
			query("Table Relationships", "Inspect accounts and count how many distinct users have a bank relationship.", `SELECT count(*) FROM accounts;`, "3", "related_users"),
			schema("One to Many", "Create orders where user_id is a foreign key to users(id).", `CREATE TABLE users (id INTEGER PRIMARY KEY);`, `CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER);`, `SELECT "table" || ':' || "from" || ':' || "to" FROM pragma_foreign_key_list('orders');`, "users:user_id:id"),
			schema("Many to Many", "Create user_roles with user_id and role_id as a composite primary key.", "", `CREATE TABLE user_roles (user_id INTEGER, role_id INTEGER);`, `SELECT group_concat(name, ',') FROM pragma_table_info('user_roles') WHERE pk > 0 ORDER BY pk;`, "user_id,role_id"),
			schema("Database Normalization", "Separate location data by creating cities(id INTEGER PRIMARY KEY, name TEXT NOT NULL) and customers(id INTEGER PRIMARY KEY, city_id INTEGER REFERENCES cities(id)).", "", `CREATE TABLE customers (id INTEGER PRIMARY KEY, city TEXT);`, `SELECT (SELECT group_concat(name || ':' || type || ':' || pk || ':' || "notnull", ',') FROM pragma_table_info('cities')) || '|' || (SELECT group_concat(name || ':' || type || ':' || pk, ',') FROM pragma_table_info('customers')) || '|' || (SELECT "table" || ':' || "from" || ':' || "to" FROM pragma_foreign_key_list('customers'));`, "id:INTEGER:1:0,name:TEXT:0:1|id:INTEGER:1,city_id:INTEGER:0|cities:city_id:id"),
			query("Normal Forms", "Show that user emails are atomic by returning the count of emails containing a comma.", `SELECT email FROM users;`, "0", "non_atomic_emails"),
			schema("First Normal Form (1NF)", "Store one tag per row: create user_tags with user_id INTEGER and tag TEXT as a composite primary key.", "", `CREATE TABLE user_tags (user_id INTEGER, tags TEXT);`, `SELECT group_concat(name || ':' || type || ':' || pk, ',') FROM pragma_table_info('user_tags');`, "user_id:INTEGER:1,tag:TEXT:2"),
			schema("Second Normal Form (2NF)", "Create enrollments with student_id INTEGER and course_id INTEGER as a composite primary key; do not duplicate the course title.", "", `CREATE TABLE enrollments (student_id INTEGER, course_id INTEGER, course_title TEXT);`, `SELECT group_concat(name || ':' || type || ':' || pk, ',') FROM pragma_table_info('enrollments');`, "student_id:INTEGER:1,course_id:INTEGER:2"),
			schema("Third Normal Form (3NF)", "Create cities(id INTEGER PRIMARY KEY, name TEXT NOT NULL) and customers(id INTEGER PRIMARY KEY, name TEXT NOT NULL, city_id INTEGER REFERENCES cities(id)).", "", `CREATE TABLE customers (id INTEGER, name TEXT, city_name TEXT);`, `SELECT (SELECT group_concat(name || ':' || type || ':' || pk || ':' || "notnull", ',') FROM pragma_table_info('cities')) || '|' || (SELECT group_concat(name || ':' || type || ':' || pk || ':' || "notnull", ',') FROM pragma_table_info('customers')) || '|' || (SELECT "table" || ':' || "from" || ':' || "to" FROM pragma_foreign_key_list('customers'));`, "id:INTEGER:1:0,name:TEXT:0:1|id:INTEGER:1:0,name:TEXT:0:1,city_id:INTEGER:0:0|cities:city_id:id"),
			schema("Boyce-Codd Normal Form (BCNF)", "Create room_assignments(course TEXT, instructor TEXT, room TEXT) with course and instructor as the composite primary key.", "", `CREATE TABLE room_assignments (course TEXT, instructor TEXT, room TEXT);`, `SELECT group_concat(name || ':' || type || ':' || pk, ',') FROM pragma_table_info('room_assignments');`, "course:TEXT:1,instructor:TEXT:2,room:TEXT:0"),
			query("Normalization Review", "List the three normalized tables that connect users to banks, alphabetically.", `SELECT name FROM sqlite_master WHERE type='table';`, "accounts\nbanks\nusers", "name"),
			query("Query Practice – Users & Banks", "Return account holder, bank, and account balance, ordered by account id.", `SELECT * FROM accounts;`, "Ada|Atlas Bank|500.0\nLinus|North Bank|300.0\nAli|Atlas Bank|150.0", "user_name", "bank_name", "balance"),
		}},
		{Title: "Joins", Tasks: []sqlTask{
			query("Joins", "Inner join users to countries and return name and country in user id order.", `SELECT name, country_code FROM users;`, "Ada|United Kingdom\nLinus|Finland\nGrace|United States\nAli|Pakistan\nNoor|Pakistan", "user_name", "country_name"),
			query("Namespacing on Tables", "Return users.name and countries.name as country_name for Pakistani users, alphabetically by user name.", `SELECT name FROM users;`, "Ali|28|Pakistan\nNoor|31|Pakistan", "name", "age", "country_name"),
			query("Left Join", "Left join every user to accounts, keeping users without accounts, ordered by user id.", `SELECT name FROM users;`, "Ada|500.0\nLinus|300.0\nGrace|NULL\nAli|150.0\nNoor|NULL", "name", "account_balance"),
			query("Right Join", "Express a right-join result portably by starting from banks and left joining accounts; return bank and account id.", `SELECT name FROM banks;`, "Atlas Bank|1\nAtlas Bank|3\nNorth Bank|2", "bank_name", "account_id"),
			query("Full Join", "Build a full outer join of users and accounts with UNION, preserving users without accounts.", `SELECT name FROM users JOIN accounts;`, "Ada|1\nLinus|2\nGrace|NULL\nAli|3\nNoor|NULL", "name", "account_id"),
			query("Joins Quiz", "Join transactions to users and return completed transaction id and user name in transaction order.", `SELECT id FROM transactions WHERE status='completed';`, "1|Ada\n3|Linus\n5|Ali\n6|Ali", "transaction_id", "name"),
			query("Joins Quiz", "Self join users to their managers and return employee and manager for users that have one.", `SELECT name, manager_id FROM users;`, "Linus|Ada\nGrace|Ada\nAli|Linus\nNoor|Linus", "employee", "manager"),
			query("Joins Quiz", "Join posts to users and return published post title and author in post id order.", `SELECT title FROM posts;`, "SQLite in Practice|Ada\nSafe Queries|Ali", "title", "author"),
			query("Join Practice", "Join accounts through users and banks; return holder, bank, and balance by account id.", `SELECT * FROM accounts;`, "Ada|Atlas Bank|500.0\nLinus|North Bank|300.0\nAli|Atlas Bank|150.0", "holder", "bank", "balance"),
			query("Query Practice – Support Tickets", "Join open tickets to users and return priority, subject, and user, highest priority first.", `SELECT * FROM tickets WHERE status='open';`, "3|Card declined|Ada\n2|Wrong balance|Ali", "priority", "subject", "user_name"),
		}},
		{Title: "Performance", Tasks: []sqlTask{
			verify("SQL Indexes", "Create an index named idx_transactions_user on transactions(user_id).", `SELECT * FROM transactions;`, `SELECT group_concat(name, ',') FROM pragma_index_info('idx_transactions_user');`, "user_id"),
			query("Index Review", "Inspect the built-in unique index SQLite created for users.email.", `SELECT name FROM sqlite_master WHERE type='index';`, "sqlite_autoindex_users_1", "name"),
			verify("Multi-Column Indexes", "Create idx_transactions_status_date on transactions(status, created_at) in that order.", `CREATE INDEX idx_transactions_status_date ON transactions(status);`, `SELECT group_concat(name, ',') FROM pragma_index_info('idx_transactions_status_date') ORDER BY seqno;`, "status,created_at"),
			verify("Denormalizing for Speed", "Add country_name to users and populate it from countries with a correlated subquery.", `ALTER TABLE users ADD COLUMN country_name TEXT;`, `SELECT name || '|' || country_name FROM users ORDER BY id;`, "Ada|United Kingdom\nLinus|Finland\nGrace|United States\nAli|Pakistan\nNoor|Pakistan"),
			query("SQL Injection", "Parameterized queries are the real defense. For this raw-SQL exercise, treat the malicious-looking email x' OR 1=1 -- as data by escaping its quote inside the string literal; return the matching count.", `SELECT count(*) FROM users WHERE email = 'x' OR 1=1 --';`, "0", "matching_users"),
		}},
	}
}

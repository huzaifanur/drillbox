package main

import "strings"

const (
	generatedLead  = " is an original local checkpoint in the "
	sqlBoilerplate = " Run the statement against a fresh SQLite database. The result or resulting schema must match exactly to pass."
)

func prepareLessonContent(items []lesson) {
	for i := range items {
		item := &items[i]
		if item.Concept == "" && item.CourseID == "learn-go" && !strings.Contains(item.Instruction, generatedLead) {
			item.Concept, item.Instruction = splitConcept(item.Instruction)
		}
		if item.Concept == "" {
			item.Concept = conceptFor(item.CourseID, item.Chapter)
		}
		item.Instruction = compactInstruction(item.Instruction)
	}
}

func splitConcept(instruction string) (string, string) {
	blocks := strings.Split(strings.TrimSpace(instruction), "\n\n")
	if len(blocks) == 0 {
		return "", instruction
	}
	first := blocks[0]
	end := -1
	for i := 0; i+1 < len(first); i++ {
		if strings.ContainsRune(".!?", rune(first[i])) && first[i+1] == ' ' {
			end = i + 1
			break
		}
	}
	if end < 0 {
		return "", instruction
	}
	concept := strings.TrimSpace(first[:end])
	blocks[0] = strings.TrimSpace(first[end:])
	if blocks[0] == "" {
		blocks = blocks[1:]
	}
	return concept, strings.Join(blocks, "\n\n")
}

func compactInstruction(instruction string) string {
	blocks := strings.Split(strings.TrimSpace(instruction), "\n\n")
	if len(blocks) > 1 && strings.Contains(blocks[0], generatedLead) {
		blocks = blocks[1:]
	}
	instruction = strings.Join(blocks, "\n\n")
	return strings.TrimSpace(strings.TrimSuffix(instruction, sqlBoilerplate))
}

func conceptFor(courseID, chapter string) string {
	chapter = chapterTitle(chapter)
	if chapters := chapterConcepts[courseID]; chapters != nil {
		if concept := chapters[chapter]; concept != "" {
			return concept
		}
	}
	return courseConcepts[courseID]
}

func chapterTitle(chapter string) string {
	if _, title, ok := strings.Cut(chapter, " · "); ok {
		return strings.TrimSpace(title)
	}
	return strings.TrimSpace(chapter)
}

var courseConcepts = map[string]string{
	"learn-go":        "Go favors small, explicit building blocks that compose into readable and concurrent programs.",
	"http-clients":    "HTTP clients construct requests, validate responses, and decode remote data safely.",
	"http-servers":    "HTTP servers route requests through handlers and return deliberate responses.",
	"pokedex":         "A command-line application combines user input, remote APIs, caching, and local state.",
	"pubsub":          "Publishers and subscribers exchange events through brokers without depending directly on each other.",
	"ci-cd":           "Delivery pipelines automate checks, builds, and deployments so changes remain repeatable.",
	"http-protocol":   "HTTP is a structured message protocol carried over network connections.",
	"blog-aggregator": "A feed aggregator combines configuration, persistence, RSS parsing, and scheduled collection.",
	"cryptography":    "Cryptography protects confidentiality, integrity, and authenticity with carefully chosen primitives.",
	"file-servers":    "File delivery balances storage, streaming, caching, security, and resilience.",
	"observability":   "Logs, metrics, profiles, and traces explain what a running service is doing.",
	"web-scraper":     "A crawler fetches pages, extracts links, limits traversal, and reports results.",
	"grafana":         "Grafana turns telemetry queries into dashboards and actionable alerts.",
	"sql":             "SQL describes the data a relational database should return or change.",
}

var chapterConcepts = map[string]map[string]string{
	"learn-go": {
		"Variables":                "Variables give typed values meaningful names; short declarations infer types inside functions.",
		"Constants and Formatting": "Declare immutable values and format strings and Unicode runes correctly.",
		"Conditionals":             "Express branching with scoped if initializers and switches while keeping decisions readable.",
		"Functions":                "Compose reusable behavior with parameters, multiple returns, closures, function values, and deferred cleanup.",
		"Structs":                  "Model related data with nested or embedded structs and attach behavior through methods.",
		"Interfaces":               "Define small behavioral contracts, rely on implicit implementation, and inspect dynamic types safely.",
		"Errors":                   "Return and wrap errors for recoverable failures; reserve panic for unrecoverable states.",
		"Loops":                    "Use Go's single for construct with conditions, ranges, break, and continue.",
		"Slices":                   "Manage dynamic sequences with make, append, ranges, capacity, and deliberate backing-array sharing.",
		"Maps":                     "Store keyed data, mutate entries, count occurrences, and organize nested lookups.",
		"Pointers":                 "Use pointers when functions must mutate shared values or avoid costly copies.",
		"Packages and Modules":     "Organize reusable code into focused packages and manage dependencies through Go modules.",
		"Channels":                 "Coordinate goroutines through sends, receives, buffering, closure, ranges, and select.",
		"Mutexes":                  "Protect shared mutable state with mutexes and use read locks for read-heavy access.",
		"Generics":                 "Write type-safe reusable algorithms by parameterizing types and constraining their operations.",
		"Enums":                    "Represent finite value sets with defined types and related constants generated by iota.",
		"Quiz":                     "Apply Go proverbs as guidance for simplicity, concurrency, interfaces, and readable design.",
	},
	"http-clients": {
		"Why HTTP?": "HTTP requests and responses exchange resources between web clients and servers.",
		"JSON":      "Marshal typed Go values into JSON and unmarshal payloads into matching structures.",
		"DNS":       "Resolve human-readable domains into network addresses before connecting to web services.",
		"URIs":      "Parse URIs into schemes, hosts, ports, paths, and query parameters.",
		"Headers":   "Headers carry metadata such as content types, authorization, and caching directives.",
		"Methods":   "Choose HTTP methods by operation semantics and validate status codes before reading bodies.",
		"Paths":     "Target REST resources with meaningful paths and encode optional filters as query parameters.",
		"HTTPS":     "TLS protects HTTP traffic with encryption and server identity verification.",
		"Errors":    "Check request and response failures explicitly and give callers useful context.",
		"cURL":      "Exercise endpoints with cURL and inspect structured responses with jq.",
	},
	"http-servers": {
		"Servers":        "Build concurrent HTTP servers with file serving, custom handlers, and reliable development workflows.",
		"Routing":        "Compose middleware, stateful handlers, and URL patterns to dispatch requests cleanly.",
		"Architecture":   "Separate application concerns, organize administrative routes, and compare deployment architectures.",
		"JSON":           "Call HTTP services and encode or decode JSON safely in Go.",
		"Storage":        "Persist users and messages with migrations, generated SQL, contexts, and resource-oriented queries.",
		"Authentication": "Secure identities with password hashing, access tokens, refresh tokens, revocation, and cookies.",
		"Authorization":  "Enforce permissions separately from identity verification, including ownership checks.",
		"Webhooks":       "Receive authenticated event callbacks and protect webhook endpoints with API keys.",
		"Documentation":  "Document API behavior, setup, and usage so a project remains maintainable.",
	},
	"pokedex": {
		"REPL":    "Build an interactive command loop with discoverable help and clean exit behavior.",
		"Cache":   "Manage state, query PokeAPI, cache responses, and implement explore, catch, and inspect commands.",
		"Pokedex": "Store caught Pokémon locally and package the completed application for submission.",
	},
	"pubsub": {
		"Pub/Sub Architecture":  "Decouple publishers and subscribers through events so services can evolve independently.",
		"Message Brokers":       "Connect to RabbitMQ through AMQP and understand where other broker protocols fit.",
		"Publishers & Queues":   "Declare queues, choose exchanges, and publish messages synchronously or asynchronously by topic.",
		"Subscribers & Routing": "Run consumers and use routing patterns to deliver messages intentionally.",
		"Delivery":              "Control acknowledgments, requeues, dead-letter queues, and delivery guarantees for safe failure handling.",
		"Serialization":         "Serialize event payloads with explicit schemas before publishing structured messages.",
		"Scalability":           "Scale brokers with clusters, quorum queues, prefetch limits, and backpressure-aware consumers.",
	},
	"ci-cd": {
		"Continuous Integration": "Automate pull-request checks with GitHub Actions so changes integrate safely and repeatedly.",
		"Tests":                  "Run tests and coverage in CI, then expose build health through a badge.",
		"Formatting":             "Enforce deterministic Go formatting locally and in CI to prevent style drift.",
		"Linting":                "Detect code-quality defects automatically by integrating Go linting into CI.",
		"Security":               "Run security checks in CI, remediate findings, and verify protections continuously.",
		"Build":                  "Containerize the Go application and automate image builds in a registry.",
		"Deploy":                 "Deploy and update container revisions with explicit service configuration.",
		"Database":               "Provision a database, apply migrations automatically, and connect deployed applications safely.",
	},
	"http-protocol": {
		"HTTP Streams":     "Read network data incrementally, recognize boundaries, and stream parsed bytes through channels.",
		"TCP":              "Compare TCP and UDP and understand how network streams differ from files.",
		"Requests":         "Translate raw TCP bytes into HTTP requests using RFC framing and method semantics.",
		"Request Lines":    "Parse HTTP request lines incrementally with state machines and stream-aware validation.",
		"HTTP Headers":     "Parse constrained header fields, preserve repeated values, and handle fragmented reads.",
		"HTTP Body":        "Parse request bodies from streams while respecting framing and completeness.",
		"HTTP Responses":   "Construct status lines, headers, content lengths, handlers, and complete server responses.",
		"Chunked Encoding": "Decode chunked bodies and process trailers when content length is unknown.",
		"Binary Data":      "Serve binary payloads correctly and understand HTTP/2 and HTTP/3 binary framing.",
	},
	"blog-aggregator": {
		"Config":    "Store application settings and register CLI commands for consistent execution.",
		"Database":  "Persist users and feed data with PostgreSQL, migrations, and generated queries.",
		"RSS":       "Parse RSS feeds and manage their metadata as structured application data.",
		"Following": "Add authenticated follow and unfollow workflows through reusable command middleware.",
		"Aggregate": "Fetch feeds repeatedly, store new posts, document usage, and package the application.",
	},
	"cryptography": {
		"Symmetric Encryption":  "Encrypt and decrypt data with a shared secret while managing keys safely.",
		"Encoding":              "Convert bytes into portable text without confusing representation with security.",
		"Brute Force":           "Estimate keyspace and entropy to understand why weak keys are guessable.",
		"Caesar Cipher":         "Implement character substitution and recognize why tiny keyspaces provide no real security.",
		"XOR":                   "Combine bits with XOR and understand one-time-pad secrecy under strict conditions.",
		"Stream Ciphers":        "Generate a keyed pseudorandom stream and never reuse its state.",
		"Block Ciphers":         "Transform fixed-size blocks through keyed rounds, schedules, and secure operating modes.",
		"DES":                   "Explore Feistel rounds and substitution boxes while understanding why DES is insecure.",
		"AES":                   "Use the modern block cipher with unique nonces and authenticated modes.",
		"Asymmetric Encryption": "Use public keys for encryption or verification and private keys for decryption or signing.",
		"RSA":                   "Build RSA keys with modular arithmetic and understand their practical tradeoffs.",
		"Hash Functions":        "Map arbitrary data to deterministic fixed-size digests while accounting for collisions.",
		"Digital Signatures":    "Authenticate message integrity with MACs or public-key signatures.",
		"KDFs":                  "Derive password-resistant keys with salted, deliberately slow algorithms such as Argon2.",
	},
	"file-servers": {
		"File Storage":    "Handle uploads, MIME types, multipart data, local files, video encoding, and safe edits.",
		"Caching":         "Control reuse with cache headers and versioned URLs that prevent stale content.",
		"AWS S3":          "Move files beyond one machine by uploading objects through AWS SDKs.",
		"Object Storage":  "Treat files as keyed objects without assuming filesystem semantics.",
		"Video Streaming": "Serve video efficiently through partial requests and evaluate streaming approaches.",
		"Security":        "Restrict storage with scoped roles, private buckets, signed URLs, and encryption.",
		"CDNs":            "Deliver files from edge regions and invalidate cached objects when necessary.",
		"Resiliency":      "Design for availability, reliability, and durability with versioning and recoverable deletion.",
	},
	"observability": {
		"Observability":      "Use logs, metrics, profiles, and traces to reveal a service's internal behavior.",
		"Logging":            "Add configurable logging with dependency injection, buffering, failure handling, and reliable cleanup.",
		"Structured Logging": "Emit machine-readable events with levels and key-value attributes using slog.",
		"Log Strategies":     "Produce minimal, timestamped records with one meaningful entry per event.",
		"Logging Errors":     "Capture useful error context while ensuring each error is logged once.",
		"Logging Context":    "Attach build, request, user, and cross-service identifiers so events remain traceable.",
		"Log Storage":        "Route logs to consoles, rotating files, or syslog according to operational needs.",
		"Log Security":       "Prevent secret leakage through filtering, obfuscation, restrained responses, and encryption.",
		"Metrics":            "Expose service measurements to Prometheus and visualize indicators that support decisions.",
		"Alerting":           "Trigger actionable notifications from meaningful thresholds while minimizing noise.",
		"Profiling":          "Use pprof profiles to locate CPU, memory, and goroutine bottlenecks.",
		"Tracing":            "Instrument requests with spans to follow execution across distributed services.",
	},
	"web-scraper": {
		"Setup":       "Normalize URLs, fetch and parse HTML, extract content and links, and structure page records.",
		"Crawling":    "Recursively fetch linked pages while controlling traversal and handling retrieval failures.",
		"Concurrency": "Crawl pages safely in parallel while enforcing a maximum page limit.",
		"Reporting":   "Export crawl results as JSON and package the completed scraper.",
	},
	"grafana": {
		"Grafana Foundations": "Understand Grafana's role, deployment, interface, organizations, and place in an observability stack.",
		"Data Sources":        "Connect Prometheus, verify data-source health and scrape targets, then explore live metrics.",
		"PromQL":              "Select, filter, rate, and aggregate Prometheus time series with practical expressions.",
		"Dashboards":          "Build reusable dashboards with panels, time ranges, variables, and portable JSON.",
		"Alerting":            "Detect conditions and route notifications with alert rules, contact points, policies, and silences.",
		"Go Metrics":          "Expose Go telemetry in Prometheus format with counters, gauges, histograms, and an HTTP handler.",
		"Operations":          "Operate Grafana reproducibly through file provisioning, permissions, managed resources, and backups.",
	},
	"sql": {
		"Introduction":  "Use SQL's declarative model to select, filter, and relate data.",
		"Tables":        "Define schemas, choose data types, alter tables, and manage repeatable migrations.",
		"Constraints":   "Protect integrity with null rules, keys, uniqueness, and validated relationships.",
		"CRUD":          "Create, read, update, and delete rows safely with targeted conditions.",
		"Basic Queries": "Shape precise results with aliases, functions, ranges, distinct values, operators, and patterns.",
		"Structuring":   "Control result order and size with sorting, limits, offsets, and deterministic tie-breaking.",
		"Aggregations":  "Summarize grouped data with counts, totals, extrema, averages, HAVING, and rounding.",
		"Subqueries":    "Nest queries to derive values or filter with intermediate results.",
		"Normalization": "Model relationships while reducing duplication through established normal forms.",
		"Joins":         "Combine related tables while preserving the rows each join type promises.",
		"Performance":   "Improve access with indexes and selective denormalization; prevent injection with parameters.",
	},
}

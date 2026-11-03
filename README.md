# Drillbox

[![CI](https://github.com/huzaifanur/drillbox/actions/workflows/ci.yml/badge.svg)](https://github.com/huzaifanur/drillbox/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Drillbox is a lean local practice platform for backend engineers: read a lesson, edit the exercise in the browser, run it and see whether it passes. It has 14 tracks and 734 runnable lessons covering Go, SQL, HTTP and observability, and the Grafana course starts its own local Docker lab with Grafana, Prometheus and a Go metrics service. Everything runs on your machine; progress and drafts stay in your browser.

**Install:** `git clone https://github.com/huzaifanur/drillbox && ./install.sh` (Ubuntu desktop) · **Stack:** Go, SQLite, Docker Compose · **License:** MIT

## Features

- 14 tracks, 734 runnable lessons:
  - **Learn Go** (145 lessons), assembled from the authored Chapter 1 lessons in `lessons/lessons.json` and the outline in `lessons/track.json`
  - **Learn SQL** (95 original exercises), each run against a fresh in-memory SQLite database
  - **HTTP Clients** (55 lessons)
  - **Learn Grafana for Go Services** (35 live, PromQL, Go and provisioning-file assessments) with a required local lab
  - 10 more Go tracks (404 lessons): HTTP servers, the HTTP protocol, Pub/Sub with RabbitMQ, CI/CD, cryptography, logging and observability, file servers and CDNs, a Pokedex, a blog aggregator and a web scraper
- Browser editor with Go highlighting and inline lint diagnostics (CodeMirror)
- Pass/fail checks on every lesson
- Desktop launcher with a tray icon that stays out of the way of the lesson
- Progress and code drafts saved in the browser's local storage

## Install the course launcher

Requires Go 1.22 or newer, Python 3 with SQLite and PyGObject, Ayatana AppIndicator, Docker with Compose, Zenity, and `xdg-open`. On Ubuntu, the tray dependencies are provided by `python3-gi` and `gir1.2-ayatanaappindicator3-0.1`. Install once:

```sh
git clone https://github.com/huzaifanur/drillbox.git
cd drillbox
./install.sh
```

Then open **Drillbox** from the application menu. The launcher opens the catalog in your browser and stays available from the system tray. **Stop & Close** shuts down the course server and Grafana lab containers. Browser progress, drafts, Grafana dashboards and Prometheus data survive a restart.

For development, `go run .` starts the server at <http://127.0.0.1:8080> without the desktop controller.

## Grafana lab

The Grafana course needs a local stack with Grafana, Prometheus and a small Go metrics service. Opening that course starts the stack automatically; the course page shows startup health, links, credentials and the exact task for each lesson. To start it by hand:

```sh
docker compose up -d
```

Open Grafana at <http://127.0.0.1:3001> (`admin` / `admin`, local lab only). The course deliberately asks you to configure the first data source yourself. Sample metrics are at <http://127.0.0.1:8081/metrics>. A manual `docker compose down` also keeps data; add `-v` only when you want to erase the Docker volumes.

## Tech stack

- **Server:** Go standard library only
- **Runner:** `go run` for Go lessons, Python's `sqlite3` with an in-memory database for SQL lessons
- **Frontend:** plain HTML, CSS and JavaScript, with a CodeMirror editor bundled by esbuild into `static/editor.js`
- **Launcher:** Python + GTK tray app (`launcher_tray.py`)
- **Lab:** Docker Compose with Grafana, Prometheus and a Go metrics service (`compose.yaml`, `labs/grafana/`)

## How it works

Lessons are JSON files under `lessons/`. The Go server loads them into courses, serves the catalog and lesson pages from `static/`, and runs submitted code locally: Go code with `go run`, SQL against a fresh in-memory database. It compares the output with the lesson's expected result and returns pass or fail; Grafana assessments also check the live lab, such as whether Prometheus is scraping the Go service. Courses that declare a required lab (currently Grafana) start their Docker Compose stack on demand.

The runner executes your code directly on this computer. It is bound to localhost on purpose and should not be exposed to other users without sandboxing execution first.

## Testing

```sh
go test ./... -race
go vet ./...
```

CI runs the same checks on every push and pull request, plus a rebuild check of the editor bundle, `node --check static/app.js` and `docker compose config`. After editor changes, rebuild the bundle with `npm install && npm run build:editor` and commit `static/editor.js`.

## Content

Topic ordering follows the publicly visible Boot.dev Learn Go and Learn SQL outlines. All explanations, seed data and runnable checkpoints in this project are original; this project is not affiliated with Boot.dev.

## License

[MIT](LICENSE)

Author: Huzaifa Nur — https://www.huzaifanur.com

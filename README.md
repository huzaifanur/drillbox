# Drillbox

A lean local practice platform: read a lesson, edit or run the exercise, and pass or fail.

## Supported interface

Drillbox is a desktop-only application with a minimum supported viewport width of 1024px. Mobile and tablet layouts, responsive behavior, SEO, and dedicated accessibility optimization are intentionally outside the project scope.

## Install the course launcher

Requires Go 1.22 or newer, Python 3 with SQLite and PyGObject, Ayatana AppIndicator, Docker with Compose, Zenity, and `xdg-open`. On Ubuntu, the tray dependencies are provided by `python3-gi` and `gir1.2-ayatanaappindicator3-0.1`. Install once:

```sh
git clone https://github.com/huzaifanur/drillbox.git
cd drillbox
./install.sh
```

Then open **Drillbox** from the application menu. The launcher opens the catalog in your browser and stays available from the system tray without covering the lesson. Closing the tray launcher shuts down the course server and every course container automatically. Browser progress, drafts, dashboards, and Docker volumes remain available after a restart.

For development, `go run .` still starts the server at <http://127.0.0.1:8080> without the desktop controller.

## Course labs

Courses start only the local services they need. Grafana uses Grafana, Prometheus, and a small Go metrics service; Observability also starts Jaeger; Pub/Sub uses RabbitMQ; the server and aggregator work uses PostgreSQL; file-server storage work uses MinIO. The course page shows startup health, links, and local credentials. The launcher runs `docker compose down` when it closes, preserving volumes.

```sh
docker compose --profile grafana up -d
docker compose --profile observability up -d
docker compose --profile pubsub up -d
docker compose --profile postgres up -d
docker compose --profile object-storage up -d
```

Open Grafana at <http://127.0.0.1:3001> (`admin` / `admin`). The course deliberately asks you to configure the first data source yourself. Sample metrics are available at <http://127.0.0.1:8081/metrics>. For manual cleanup, run `docker compose --profile <active-profile> down`; add `-v` only when you intentionally want to erase Docker volumes.

The committed `static/editor.js` bundle provides Go highlighting and inline lint diagnostics. Rebuild it after editor changes with `npm install && npm run build:editor`.

The app includes 14 tracks and 734 runnable lessons. Grafana has 35 authored live, PromQL, Go, and provisioning-file assessments. Learn SQL has 95 original exercises that run against a fresh in-memory SQLite database. Every other course is authored in `lessons/courses/`; content is freeform rather than forced into repeated lesson headings. Progress, submissions, and code drafts stay in the browser's local storage.

Topic ordering follows the publicly visible Boot.dev Learn Go and Learn SQL outlines. All explanations, seed data, and runnable checkpoints in this project are original; this project is not affiliated with Boot.dev.

The runner executes your code directly on this computer. It is intentionally bound to localhost and should not be exposed to other users without sandboxing execution first.

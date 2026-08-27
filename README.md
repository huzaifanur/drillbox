# Drillbox

A lean local practice platform: read a lesson, edit or run the exercise, and pass or fail.

## Install the course launcher

Requires Go 1.22 or newer, Python 3 with its standard SQLite module, Docker with Compose, Zenity, and `xdg-open`. Install once:

```sh
git clone https://github.com/huzaifanur/drillbox.git
cd drillbox
./install.sh
```

Then open **Drillbox** from the application menu. The launcher opens the catalog in your browser and stays visible as a small controller. **Stop & Close** (or closing the controller) shuts down the course server and Grafana lab containers automatically. Browser progress, drafts, Grafana dashboards, and Prometheus data remain available after a restart.

For development, `go run .` still starts the server at <http://127.0.0.1:8080> without the desktop controller.

## Grafana lab

The Grafana course includes a required local stack with Grafana, Prometheus, and a small Go metrics service. Opening that course starts the stack automatically; the course page shows startup health, links, credentials, and the exact task for each lesson.

```sh
docker compose up -d
```

Open Grafana at <http://127.0.0.1:3001> (`admin` / `admin`). The course deliberately asks you to configure the first data source yourself. Sample metrics are available at <http://127.0.0.1:8081/metrics>. Manual `docker compose down` also preserves data; add `-v` only when you intentionally want to erase Docker volumes.

The committed `static/editor.js` bundle provides Go highlighting and inline lint diagnostics. Rebuild it after editor changes with `npm install && npm run build:editor`.

The app includes 14 tracks and 734 runnable lessons. Grafana has 35 authored live, PromQL, Go, and provisioning-file assessments. Learn SQL has 95 original exercises that run against a fresh in-memory SQLite database. Learn Go is assembled from the authored Chapter 1 lessons in `lessons/lessons.json` and the remaining outline in `lessons/track.json`; HTTP Clients and the other Go tracks live in the other JSON files under `lessons/`. Progress and code drafts stay in the browser's local storage.

Topic ordering follows the publicly visible Boot.dev Learn Go and Learn SQL outlines. All explanations, seed data, and runnable checkpoints in this project are original; this project is not affiliated with Boot.dev.

The runner executes your code directly on this computer. It is intentionally bound to localhost and should not be exposed to other users without sandboxing execution first.

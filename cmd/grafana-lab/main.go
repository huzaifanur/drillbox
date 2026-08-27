package main

import (
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

var requests atomic.Uint64
var started = time.Now()

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		fmt.Fprintln(w, "Go metrics lab is running. Metrics: /metrics")
	})
	http.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		count := requests.Add(1)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintln(w, "# HELP go_lab_requests_total Requests handled by the lab service.")
		fmt.Fprintln(w, "# TYPE go_lab_requests_total counter")
		fmt.Fprintf(w, "go_lab_requests_total %d\n", count)
		fmt.Fprintln(w, "# HELP go_lab_uptime_seconds Time since the lab service started.")
		fmt.Fprintln(w, "# TYPE go_lab_uptime_seconds gauge")
		fmt.Fprintf(w, "go_lab_uptime_seconds %.0f\n", time.Since(started).Seconds())
	})
	http.HandleFunc("/alerts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	log.Println("Go metrics lab is ready at http://localhost:8081")
	log.Fatal(http.ListenAndServe(":8081", nil))
}

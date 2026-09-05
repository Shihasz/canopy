// Command sampleapp is a minimal HTTP service used only inside the local
// docker-compose lab. It exposes Prometheus metrics and can simulate
// errors/latency via env vars, so the lab can exercise canopy's canary
// analysis against real (if synthetic) traffic.
package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	target  = getenv("TARGET", "unknown") // "canary" or "stable"
	version = getenv("VERSION", "dev")

	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests handled, labeled by target and status.",
	}, []string{"target", "status"})

	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request duration in seconds, labeled by target.",
		Buckets: prometheus.DefBuckets,
	}, []string{"target"})
)

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

// handler simulates real request work: a baseline latency plus jitter,
// and a configurable probability of returning 500. ERROR_RATE and
// LATENCY_MS are read per-request (not cached at startup) so the lab can
// be tuned live by rewriting the systemd unit's environment if needed later.
func handler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	errorRate := getenvFloat("ERROR_RATE", 0)
	latencyMs := getenvFloat("LATENCY_MS", 20)

	jitter := time.Duration(rand.Intn(20)) * time.Millisecond
	time.Sleep(time.Duration(latencyMs)*time.Millisecond + jitter)

	status := http.StatusOK
	if rand.Float64() < errorRate {
		status = http.StatusInternalServerError
	}

	w.WriteHeader(status)
	fmt.Fprintf(w, "target=%s version=%s status=%d\n", target, version, status)

	requestsTotal.WithLabelValues(target, strconv.Itoa(status)).Inc()
	requestDuration.WithLabelValues(target).Observe(time.Since(start).Seconds())
}

func main() {
	port := getenv("PORT", "8080")

	mux := http.NewServeMux()
	mux.HandleFunc("/work", handler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	mux.Handle("/metrics", promhttp.Handler())

	log.Printf("sampleapp starting: target=%s version=%s port=%s", target, version, port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

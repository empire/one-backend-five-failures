// Package metrics counts what the service does, so you do not have to watch it from the outside.
// It records the four golden signals of the Google SRE book, one metric each:
//
//	latency      http_request_duration_seconds   how long requests take
//	traffic      http_requests_total             how many requests arrive
//	errors       http_errors_total               how many of them fail (status 500 and up)
//	saturation   http_requests_in_flight         how full the service is
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the four metrics and the registry that /metrics prints.
type Metrics struct {
	reg      *prometheus.Registry
	duration *prometheus.HistogramVec
	requests *prometheus.CounterVec
	errors   *prometheus.CounterVec
	inFlight prometheus.Gauge
}

// New returns metrics for the given routes (such as "GET /{code}"). A route that has not been called yet still shows as 0.
func New(routes ...string) *Metrics {
	m := &Metrics{
		reg: prometheus.NewRegistry(),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "http_request_duration_seconds", Help: "How long requests take.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
		}, []string{"route"}),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total", Help: "Requests received.",
		}, []string{"route", "code"}),
		errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_errors_total", Help: "Requests answered with status 500 or more.",
		}, []string{"route"}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_requests_in_flight", Help: "Requests being served right now.",
		}),
	}
	m.reg.MustRegister(m.duration, m.requests, m.errors, m.inFlight)
	for _, r := range routes {
		m.errors.WithLabelValues(r)
	}
	return m
}

// Handler serves the metrics in the Prometheus text format (GET /metrics).
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// Wrap records every request that mux serves, except the scrape of /metrics itself.
func (m *Metrics) Wrap(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.inFlight.Inc()
		defer m.inFlight.Dec()
		rec := &recorder{ResponseWriter: w, code: http.StatusOK}
		start := time.Now()
		mux.ServeHTTP(rec, r)
		route := r.Pattern // set by the mux: the route that matched, not the URL, so there is one series per route
		if route == "" {
			route = "unmatched"
		}
		if route == "GET /metrics" {
			return
		}
		m.duration.WithLabelValues(route).Observe(time.Since(start).Seconds())
		m.requests.WithLabelValues(route, strconv.Itoa(rec.code)).Inc()
		if rec.code >= 500 {
			m.errors.WithLabelValues(route).Inc()
		}
	})
}

type recorder struct {
	http.ResponseWriter
	code int
}

func (r *recorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	return rec.Body.String()
}

func TestFourSignalsAreRecordedPerRoute(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{code}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("code") == "bad" {
			http.Error(w, "broken", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusFound)
	})
	m := New("GET /{code}")
	h := m.Wrap(mux)
	for _, path := range []string{"/a", "/b", "/bad"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}
	out := scrape(t, m)
	for _, want := range []string{
		`http_requests_total{code="302",route="GET /{code}"} 2`, // traffic, one series for the route (not one per URL)
		`http_requests_total{code="500",route="GET /{code}"} 1`,
		`http_errors_total{route="GET /{code}"} 1`,                      // errors
		`http_request_duration_seconds_count{route="GET /{code}"} 3`,    // latency
		`http_request_duration_seconds_bucket{route="GET /{code}",le="`, // latency
		"http_requests_in_flight 0",                                     // saturation
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRouteWithoutRequestsShowsZeroErrors(t *testing.T) {
	if out := scrape(t, New("POST /links")); !strings.Contains(out, `http_errors_total{route="POST /links"} 0`) {
		t.Errorf("no zero line:\n%s", out)
	}
}

func TestScrapeIsNotCounted(t *testing.T) {
	mux := http.NewServeMux()
	m := New()
	mux.Handle("GET /metrics", m.Handler())
	h := m.Wrap(mux)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/metrics", nil))
	if out := scrape(t, m); strings.Contains(out, "http_requests_total{") {
		t.Errorf("the scrape was counted:\n%s", out)
	}
}

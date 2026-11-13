// Command thirdparty is a mock of the two APIs this service does not own.
//
//	POST /scan        {"url": "..."}   -> {"safe": true}
//	POST /analytics   {"code": "..."}  -> 204
//
// Each API has a base delay and two switches, set by the demos:
//
//	POST /admin/slow?api=analytics&ms=3000   answer after this many milliseconds
//	POST /admin/fail?api=analytics&pct=30    answer 500 for this share of calls
//	GET  /admin/stats                        calls received per API, and how many events each one delivered (and how many twice)
package main

import (
	"encoding/json"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type api struct {
	delayMS  atomic.Int64
	failPct  atomic.Int64
	received atomic.Int64
	failed   atomic.Int64

	// Calls that succeeded, by the event ID they carried (header X-Event-ID).
	delivered  atomic.Int64 // succeeded with an ID not seen before
	duplicates atomic.Int64 // succeeded with an ID that had already succeeded
	seen       sync.Map
}

func (a *api) handle(body any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.received.Add(1)
		time.Sleep(time.Duration(a.delayMS.Load()) * time.Millisecond)
		if rand.Int64N(100) < a.failPct.Load() {
			a.failed.Add(1)
			http.Error(w, "mock failure", http.StatusInternalServerError)
			return
		}
		if id := r.Header.Get("X-Event-ID"); id != "" {
			if _, again := a.seen.LoadOrStore(id, true); again {
				a.duplicates.Add(1)
			} else {
				a.delivered.Add(1)
			}
		}
		if body == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}
}

func main() {
	baseMS := int64(20)
	if v, err := strconv.ParseInt(os.Getenv("BASE_DELAY_MS"), 10, 64); err == nil {
		baseMS = v
	}
	apis := map[string]*api{"scan": {}, "analytics": {}}
	for _, a := range apis {
		a.delayMS.Store(baseMS)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /scan", apis["scan"].handle(map[string]bool{"safe": true}))
	mux.HandleFunc("POST /analytics", apis["analytics"].handle(nil))
	mux.HandleFunc("POST /admin/slow", func(w http.ResponseWriter, r *http.Request) {
		set(w, r, apis, func(a *api, n int64) { a.delayMS.Store(n) }, "ms")
	})
	mux.HandleFunc("POST /admin/fail", func(w http.ResponseWriter, r *http.Request) {
		set(w, r, apis, func(a *api, n int64) { a.failPct.Store(n) }, "pct")
	})
	mux.HandleFunc("GET /admin/stats", func(w http.ResponseWriter, r *http.Request) {
		out := map[string]map[string]int64{}
		for name, a := range apis {
			out[name] = map[string]int64{"received": a.received.Load(), "failed": a.failed.Load(), "delivered": a.delivered.Load(), "duplicates": a.duplicates.Load(), "delay_ms": a.delayMS.Load(), "fail_pct": a.failPct.Load()}
		}
		_ = json.NewEncoder(w).Encode(out)
	})

	addr := ":9000"
	log.Printf("mock third-party APIs on %s (base delay %d ms)", addr, baseMS)
	log.Fatal((&http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
}

func set(w http.ResponseWriter, r *http.Request, apis map[string]*api, apply func(*api, int64), param string) {
	a, ok := apis[r.URL.Query().Get("api")]
	n, err := strconv.ParseInt(r.URL.Query().Get(param), 10, 64)
	if !ok || err != nil {
		http.Error(w, "need ?api=scan|analytics&"+param+"=N", http.StatusBadRequest)
		return
	}
	apply(a, n)
	w.WriteHeader(http.StatusNoContent)
}

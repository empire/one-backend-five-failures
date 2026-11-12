// Command app is the URL shortener.
package main

import (
	"context"
	"expvar"
	"log"
	"net/http"
	"os"
	"time"

	"shortener/internal/links"
	"shortener/internal/server"
	"shortener/internal/session"
	"shortener/internal/thirdparty"
)

func main() {
	ctx := context.Background()
	store, err := links.Open(ctx, env("DATABASE_URL", "postgres://shortener:shortener@localhost:5432/shortener?pool_max_conns=10"))
	if err != nil {
		log.Fatal(err)
	}
	expvar.Publish("db_lookups", expvar.Func(func() any { return store.Lookups() }))

	srv := &server.Server{
		Sessions: session.NewMemory(),
		Links:    store,
		Third:    thirdparty.New(env("SCAN_URL", "http://localhost:9000/scan"), env("ANALYTICS_URL", "http://localhost:9000/analytics")),
	}
	addr := env("LISTEN", ":8080")
	log.Printf("listening on %s", addr)
	log.Fatal((&http.Server{Addr: addr, Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

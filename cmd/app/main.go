// Command app is the URL shortener.
package main

import (
	"context"
	"expvar"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"shortener/internal/limit"
	"shortener/internal/links"
	"shortener/internal/metrics"
	"shortener/internal/queue"
	"shortener/internal/server"
	"shortener/internal/session"
)

func main() {
	ctx := context.Background()
	store, err := links.Open(ctx, env("DATABASE_URL", "postgres://shortener:shortener@localhost:5432/shortener?pool_max_conns=10"), os.Getenv("REPLICA_URL")) // REPLICA_URL empty: reads go to the primary
	if err != nil {
		log.Fatal(err)
	}
	expvar.Publish("db_lookups", expvar.Func(func() any { return store.Lookups() }))

	opt, err := redis.ParseURL(env("STATE_URL", "redis://localhost:6379/0"))
	if err != nil {
		log.Fatal(err)
	}
	state := redis.NewClient(opt) // sessions, limiter buckets and the queue all live here

	events := queue.NewProducer(state, "click", "scan")
	expvar.Publish("events_click", expvar.Func(func() any { return events.Published("click") }))
	expvar.Publish("events_scan", expvar.Func(func() any { return events.Published("scan") }))

	srv := &server.Server{
		Sessions: sessions(state),
		Limiter:  limiter(state),
		Links:    cached(store),
		Events:   events,
		Metrics:  metrics.New("POST /login", "POST /links", "GET /{code}"),
	}
	addr := env("LISTEN", ":8080")
	log.Printf("listening on %s", addr)
	log.Fatal((&http.Server{Addr: addr, Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
}

// sessions picks where logged-in sessions live: SESSIONS=redis (default) or SESSIONS=memory.
func sessions(state *redis.Client) session.Store {
	if os.Getenv("SESSIONS") == "memory" {
		log.Print("sessions: in this process's memory")
		return session.NewMemory()
	}
	log.Print("sessions: in the shared state store")
	return session.NewRedis(state)
}

// limiter keeps one token bucket per client in the state store: LIMITER=on (default) or LIMITER=off.
func limiter(state *redis.Client) server.Limiter {
	if os.Getenv("LIMITER") == "off" {
		log.Print("limiter: off")
		return nil
	}
	size, _ := strconv.Atoi(env("BUCKET_SIZE", "10"))
	refill, _ := strconv.ParseFloat(env("REFILL_PER_S", "2"), 64)
	log.Printf("limiter: token bucket in the state store, %d tokens, %g new tokens a second", size, refill)
	return limit.New(state, size, refill)
}

// cached puts the cache in front of the database: CACHE=redis (default) or CACHE=off.
func cached(store *links.Store) server.Links {
	if os.Getenv("CACHE") == "off" {
		log.Print("cache: off, every open reads the database")
		return store
	}
	cache, err := links.OpenRedisCache(env("CACHE_URL", "redis://localhost:6380/0"))
	if err != nil {
		log.Fatal(err)
	}
	c := links.NewCached(store, cache, 5*time.Minute)
	expvar.Publish("cache_hits", expvar.Func(func() any { h, _ := c.Stats(); return h }))
	expvar.Publish("cache_misses", expvar.Func(func() any { _, m := c.Stats(); return m }))
	log.Print("cache: cache-aside in a separate Redis")
	return c
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

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

	sess := sessions()
	srv := &server.Server{
		Sessions: sess,
		Limiter:  limiter(),
		Links:    cached(store),
		Third:    thirdparty.New(env("SCAN_URL", "http://localhost:9000/scan"), env("ANALYTICS_URL", "http://localhost:9000/analytics")),
	}
	addr := env("LISTEN", ":8080")
	log.Printf("listening on %s", addr)
	log.Fatal((&http.Server{Addr: addr, Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
}

// sessions picks where logged-in sessions live: SESSIONS=redis (default) or SESSIONS=memory.
func sessions() session.Store {
	if os.Getenv("SESSIONS") == "memory" {
		log.Print("sessions: in this process's memory")
		return session.NewMemory()
	}
	opt, err := redis.ParseURL(env("STATE_URL", "redis://localhost:6379/0"))
	if err != nil {
		log.Fatal(err)
	}
	log.Print("sessions: in the shared state store")
	return session.NewRedis(redis.NewClient(opt))
}

// cached puts the cache in front of the database: CACHE=redis (default) or CACHE=off.
func cached(store *links.Store) server.Links {
	if os.Getenv("CACHE") == "off" {
		log.Print("cache: off, every open reads the database")
		return store
	}
	opt, err := redis.ParseURL(env("CACHE_URL", "redis://localhost:6380/0"))
	if err != nil {
		log.Fatal(err)
	}
	// A cache that is slow is worse than none: give up fast, and let the database answer.
	opt.DialTimeout, opt.ReadTimeout, opt.WriteTimeout = 100*time.Millisecond, 100*time.Millisecond, 100*time.Millisecond
	opt.MaxRetries = -1
	c := links.NewCached(store, links.NewRedisCache(redis.NewClient(opt)), 5*time.Minute)
	expvar.Publish("cache_hits", expvar.Func(func() any { h, _ := c.Stats(); return h }))
	expvar.Publish("cache_misses", expvar.Func(func() any { _, m := c.Stats(); return m }))
	log.Print("cache: cache-aside in a separate Redis")
	return c
}

// limiter keeps one token bucket per client in the shared state store: LIMITER=on (default) or LIMITER=off.
func limiter() server.Limiter {
	if os.Getenv("LIMITER") == "off" {
		log.Print("limiter: off")
		return nil
	}
	opt, err := redis.ParseURL(env("STATE_URL", "redis://localhost:6379/0"))
	if err != nil {
		log.Fatal(err)
	}
	size, _ := strconv.Atoi(env("BUCKET_SIZE", "10"))
	refill, _ := strconv.ParseFloat(env("REFILL_PER_S", "2"), 64)
	log.Printf("limiter: token bucket in the state store, %d tokens, %g new tokens a second", size, refill)
	return limit.New(redis.NewClient(opt), size, refill)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

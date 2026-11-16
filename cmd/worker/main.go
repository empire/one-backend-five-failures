// Command worker does the jobs the app queued: it calls the third-party APIs.
//
// One lane for each type of job ("click" for analytics, "scan" for the safety scan), each with its own
// workers, so a provider that is down only stops its own lane.
package main

import (
	"context"
	"expvar"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"shortener/internal/links"
	"shortener/internal/queue"
	"shortener/internal/thirdparty"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := links.Open(ctx, env("DATABASE_URL", "postgres://shortener:shortener@localhost:5432/shortener?pool_max_conns=10"), "") // the worker reads from the primary only
	if err != nil {
		log.Fatal(err)
	}
	cache, err := links.OpenRedisCache(env("CACHE_URL", "redis://localhost:6380/0"))
	if err != nil {
		log.Fatal(err)
	}
	// Removing an unsafe link must also remove its cached copy.
	linksDB := links.NewCached(store, cache, 5*time.Minute)

	opt, err := redis.ParseURL(env("STATE_URL", "redis://localhost:6379/0"))
	if err != nil {
		log.Fatal(err)
	}
	state := redis.NewClient(opt)

	third := thirdparty.New(env("SCAN_URL", "http://localhost:9000/scan"), env("ANALYTICS_URL", "http://localhost:9000/analytics"))
	workers, _ := strconv.Atoi(env("WORKERS", "16"))
	dedupe := os.Getenv("DEDUPE") != "off"

	click := queue.NewConsumer(state, "click", workers, func(ctx context.Context, e queue.Event) error {
		return third.Click(ctx, e.Fields["code"], e.ID)
	})
	scan := queue.NewConsumer(state, "scan", workers, func(ctx context.Context, e queue.Event) error {
		safe, err := third.Scan(ctx, e.Fields["url"], e.ID)
		if err != nil {
			return err // try again later
		}
		if !safe {
			return linksDB.Delete(ctx, e.Fields["code"])
		}
		return nil
	})
	for _, c := range []*queue.Consumer{click, scan} {
		c.Dedupe = dedupe
		c.Jitter = os.Getenv("JITTER") != "off"
	}
	log.Printf("worker: %d workers per lane, dedupe=%v", workers, dedupe)

	// What the measuring tools read at /debug/vars.
	for _, c := range []*queue.Consumer{click, scan} {
		lane := c.Lane
		expvar.Publish("done_"+lane, expvar.Func(func() any { return c.Done.Load() }))
		expvar.Publish("retries_"+lane, expvar.Func(func() any { return c.Retries.Load() }))
		expvar.Publish("dropped_"+lane, expvar.Func(func() any { return c.Dropped.Load() }))
		expvar.Publish("queue_"+lane, expvar.Func(func() any { return c.Depth(ctx) }))
	}
	go func() {
		log.Print(http.ListenAndServe(env("LISTEN", ":8080"), nil)) // expvar registers /debug/vars
	}()

	errs := make(chan error, 2)
	go func() { errs <- click.Run(ctx) }()
	go func() { errs <- scan.Run(ctx) }()
	<-errs
	<-errs
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

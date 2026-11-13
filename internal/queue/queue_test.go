package queue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestBackoffDoublesUpToTheLimit(t *testing.T) {
	want := []time.Duration{100, 200, 400, 800, 1000, 1000}
	for i, w := range want {
		if got := Backoff(i, 100*time.Millisecond, time.Second, false); got != w*time.Millisecond {
			t.Errorf("attempt %d: %v, want %v", i, got, w*time.Millisecond)
		}
	}
}

func TestJitterAddsUpToHalfAsMuchAgain(t *testing.T) {
	seen := map[time.Duration]bool{}
	for range 200 {
		d := Backoff(2, 100*time.Millisecond, time.Second, true) // 400 ms plus up to 200 ms
		if d < 400*time.Millisecond || d > 600*time.Millisecond {
			t.Fatalf("%v is outside 400-600 ms", d)
		}
		seen[d] = true
	}
	if len(seen) < 50 {
		t.Errorf("only %d different waits in 200 tries: the delay is not random", len(seen))
	}
}

// start runs a consumer in the background and returns it with the producer for its lane.
func start(t *testing.T, lane string, h Handler) (*Consumer, *Producer, *redis.Client) {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	c := NewConsumer(rdb, lane, 2, h)
	c.Base, c.Max = time.Millisecond, 5*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	go c.Run(ctx)
	t.Cleanup(cancel)
	return c, NewProducer(rdb, lane), rdb
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for range 400 {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestEventIsHandledOnceAndRemoved(t *testing.T) {
	var calls atomic.Int64
	c, p, rdb := start(t, "click", func(context.Context, Event) error { calls.Add(1); return nil })
	if err := p.Publish(context.Background(), "click", "e1", map[string]string{"code": "abc"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the event", func() bool { return c.Done.Load() == 1 })
	waitFor(t, "the stream to empty", func() bool { return c.Depth(context.Background()) == 0 })
	if calls.Load() != 1 {
		t.Errorf("handler called %d times, want 1", calls.Load())
	}
	_ = rdb
}

func TestFailedCallIsRetriedUntilItWorks(t *testing.T) {
	var calls atomic.Int64
	c, p, _ := start(t, "click", func(context.Context, Event) error {
		if calls.Add(1) <= 3 {
			return errors.New("provider is down")
		}
		return nil
	})
	_ = p.Publish(context.Background(), "click", "e1", nil)
	waitFor(t, "the event", func() bool { return c.Done.Load() == 1 })
	if calls.Load() != 4 || c.Retries.Load() != 3 {
		t.Errorf("calls = %d, retries = %d, want 4 and 3", calls.Load(), c.Retries.Load())
	}
}

func TestSameEventIDTwiceIsHandledOnce(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	c, p, _ := start(t, "click", func(_ context.Context, e Event) error {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, e.ID)
		return nil
	})
	for range 2 {
		_ = p.Publish(context.Background(), "click", "same", nil)
	}
	_ = p.Publish(context.Background(), "click", "other", nil)
	waitFor(t, "all three entries", func() bool { return c.Done.Load()+c.Dropped.Load() == 3 })
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || c.Dropped.Load() != 1 {
		t.Errorf("handled %v, dropped %d, want 2 handled and 1 dropped", seen, c.Dropped.Load())
	}
}

func TestWithoutDedupeARepeatIsHandledTwice(t *testing.T) {
	var calls atomic.Int64
	rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	c := NewConsumer(rdb, "click", 1, func(context.Context, Event) error { calls.Add(1); return nil })
	c.Dedupe = false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)
	p := NewProducer(rdb, "click")
	_ = p.Publish(ctx, "click", "same", nil)
	_ = p.Publish(ctx, "click", "same", nil)
	waitFor(t, "both entries", func() bool { return c.Done.Load() == 2 })
}

func TestOneLanesOutageDoesNotStopAnother(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	click := NewConsumer(rdb, "click", 1, func(context.Context, Event) error { return errors.New("analytics is down") })
	scan := NewConsumer(rdb, "scan", 1, func(context.Context, Event) error { return nil })
	for _, c := range []*Consumer{click, scan} {
		c.Base, c.Max = time.Millisecond, 5*time.Millisecond
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go click.Run(ctx)
	go scan.Run(ctx)
	p := NewProducer(rdb, "click", "scan")
	for i := range 5 {
		_ = p.Publish(ctx, "click", "c"+string(rune('0'+i)), nil)
		_ = p.Publish(ctx, "scan", "s"+string(rune('0'+i)), nil)
	}
	waitFor(t, "the scans", func() bool { return scan.Done.Load() == 5 })
	if click.Done.Load() != 0 || click.Depth(ctx) != 5 {
		t.Errorf("click done = %d, waiting = %d, want 0 and 5", click.Done.Load(), click.Depth(ctx))
	}
}

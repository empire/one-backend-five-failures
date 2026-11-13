// Package queue is a work queue on Redis Streams: one stream for each type of job (a "lane"),
// so a provider that is down only stops its own lane.
package queue

import (
	"context"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

const group = "workers"

// Stream is the name of the stream for a lane.
func Stream(lane string) string { return "events:" + lane }

// Event is one job. ID names the event, and stays the same if the event is delivered twice.
type Event struct {
	ID     string
	Fields map[string]string
	entry  string // the stream's own id for this entry
}

// Producer adds events to lanes.
type Producer struct {
	rdb       *redis.Client
	published map[string]*atomic.Int64
}

// NewProducer returns a producer for the given lanes.
func NewProducer(rdb *redis.Client, lanes ...string) *Producer {
	p := &Producer{rdb: rdb, published: map[string]*atomic.Int64{}}
	for _, l := range lanes {
		p.published[l] = new(atomic.Int64)
	}
	return p
}

// Publish adds an event to a lane. It returns when the state store has it.
func (p *Producer) Publish(ctx context.Context, lane, id string, fields map[string]string) error {
	values := map[string]any{"id": id}
	for k, v := range fields {
		values[k] = v
	}
	if err := p.rdb.XAdd(ctx, &redis.XAddArgs{Stream: Stream(lane), Values: values}).Err(); err != nil {
		return err
	}
	if n := p.published[lane]; n != nil {
		n.Add(1)
	}
	return nil
}

// Published is how many events this producer has added to the lane.
func (p *Producer) Published(lane string) int64 {
	if n := p.published[lane]; n != nil {
		return n.Load()
	}
	return 0
}

// Handler does the work of one event. An error means "try again later".
type Handler func(ctx context.Context, e Event) error

// Consumer reads one lane with Workers goroutines and calls Handle for each event.
// An event is removed from the stream only after Handle succeeds, so an event is never lost:
// a failed call is retried (the wait doubles each time, with a random part added),
// and an event whose worker died is taken over by another worker.
type Consumer struct {
	Lane    string
	Workers int
	Handle  Handler
	// Dedupe drops an event whose ID has been seen before (the queue can deliver an event twice).
	Dedupe bool
	// Jitter adds a random part to each wait, so retries do not all land at the same moment.
	Jitter bool
	// The first wait before a retry, and the longest wait.
	Base, Max time.Duration

	rdb     *redis.Client
	Done    atomic.Int64 // events handled
	Retries atomic.Int64 // failed calls that were tried again
	Dropped atomic.Int64 // repeats dropped by Dedupe
}

// NewConsumer returns a consumer for a lane.
func NewConsumer(rdb *redis.Client, lane string, workers int, h Handler) *Consumer {
	return &Consumer{Lane: lane, Workers: workers, Handle: h, Dedupe: true, Jitter: true, Base: 100 * time.Millisecond, Max: 2 * time.Second, rdb: rdb}
}

// Depth is how many events are in the lane: waiting, or being handled.
func (c *Consumer) Depth(ctx context.Context) int64 {
	n, _ := c.rdb.XLen(ctx, Stream(c.Lane)).Result()
	return n
}

// Run reads the lane until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	err := c.rdb.XGroupCreateMkStream(ctx, Stream(c.Lane), group, "0").Err()
	if err != nil && !isBusyGroup(err) {
		return err
	}
	done := make(chan struct{}, c.Workers+1)
	for i := range c.Workers {
		go func() { c.read(ctx, consumerName(c.Lane, i)); done <- struct{}{} }()
	}
	go func() { c.reclaim(ctx, consumerName(c.Lane, c.Workers)); done <- struct{}{} }()
	for range c.Workers + 1 {
		<-done
	}
	return ctx.Err()
}

func (c *Consumer) read(ctx context.Context, name string) {
	for ctx.Err() == nil {
		res, err := c.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: group, Consumer: name, Streams: []string{Stream(c.Lane), ">"}, Count: 10, Block: time.Second,
		}).Result()
		if err != nil {
			if err != redis.Nil {
				sleep(ctx, time.Second) // the state store is not answering: wait, then ask again
			}
			continue
		}
		for _, m := range res[0].Messages {
			c.process(ctx, m)
		}
	}
}

// reclaim takes over events that another worker read but never finished (it died).
func (c *Consumer) reclaim(ctx context.Context, name string) {
	for ctx.Err() == nil {
		sleep(ctx, 5*time.Second)
		msgs, _, err := c.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream: Stream(c.Lane), Group: group, Consumer: name, MinIdle: 30 * time.Second, Start: "0-0", Count: 10,
		}).Result()
		if err != nil {
			continue
		}
		for _, m := range msgs {
			c.process(ctx, m)
		}
	}
}

func (c *Consumer) process(ctx context.Context, m redis.XMessage) {
	e := Event{Fields: map[string]string{}, entry: m.ID}
	for k, v := range m.Values {
		if s, ok := v.(string); ok {
			e.Fields[k] = s
		}
	}
	e.ID = e.Fields["id"]
	if c.Dedupe && e.ID != "" && c.seenBefore(ctx, e) {
		c.Dropped.Add(1)
		c.finish(ctx, m.ID)
		return
	}
	for attempt := 0; ; attempt++ {
		if err := c.Handle(ctx, e); err == nil {
			break
		}
		if ctx.Err() != nil {
			return // leave it in the stream: another worker takes it over
		}
		c.Retries.Add(1)
		sleep(ctx, Backoff(attempt, c.Base, c.Max, c.Jitter))
	}
	c.Done.Add(1)
	c.finish(ctx, m.ID)
}

// seenBefore is true when another entry with the same event ID got here first.
// The first entry marks the ID with its own entry id, so a redelivery of that same entry is not a repeat.
func (c *Consumer) seenBefore(ctx context.Context, e Event) bool {
	key := "seen:" + e.ID
	ok, err := c.rdb.SetNX(ctx, key, e.entry, 24*time.Hour).Result()
	if err != nil || ok {
		return false
	}
	first, err := c.rdb.Get(ctx, key).Result()
	return err == nil && first != e.entry
}

func (c *Consumer) finish(ctx context.Context, entry string) {
	pipe := c.rdb.Pipeline()
	pipe.XAck(ctx, Stream(c.Lane), group, entry)
	pipe.XDel(ctx, Stream(c.Lane), entry)
	_, _ = pipe.Exec(ctx)
}

// Backoff is how long to wait before retry number attempt (0 is the first). The wait starts at base
// and doubles each time, up to max. With jitter, a random part (up to half as much again) is added.
func Backoff(attempt int, base, max time.Duration, jitter bool) time.Duration {
	d := base << min(attempt, 20)
	if d <= 0 || d > max {
		d = max
	}
	if jitter {
		d += rand.N(d/2 + 1)
	}
	return d
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func consumerName(lane string, i int) string { return lane + "-" + strconv.Itoa(i) }

func isBusyGroup(err error) bool { return err != nil && strings.HasPrefix(err.Error(), "BUSYGROUP") }

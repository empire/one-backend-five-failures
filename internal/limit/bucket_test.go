package limit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newBucket(t *testing.T, capacity int, refill float64) (*Bucket, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	mr.SetTime(time.Now())
	return New(redis.NewClient(&redis.Options{Addr: mr.Addr()}), capacity, refill), mr
}

func TestBurstPassesThenEmptyBucketWaits(t *testing.T) {
	b, _ := newBucket(t, 5, 2)
	ctx := context.Background()
	for i := range 5 {
		if ok, _, err := b.Allow(ctx, "quiet"); err != nil || !ok {
			t.Fatalf("request %d of the burst: ok=%v err=%v", i+1, ok, err)
		}
	}
	ok, wait, err := b.Allow(ctx, "quiet")
	if err != nil || ok {
		t.Fatalf("6th request: ok=%v err=%v, want it turned away", ok, err)
	}
	if wait <= 0 || wait > 500*time.Millisecond {
		t.Errorf("wait = %v, want up to 500ms (one token at 2 per second)", wait)
	}
}

func TestTokensComeBackWithTime(t *testing.T) {
	b, mr := newBucket(t, 2, 1)
	ctx := context.Background()
	b.Allow(ctx, "c")
	b.Allow(ctx, "c")
	if ok, _, _ := b.Allow(ctx, "c"); ok {
		t.Fatal("bucket should be empty")
	}
	mr.SetTime(time.Now().Add(1100 * time.Millisecond))
	if ok, _, _ := b.Allow(ctx, "c"); !ok {
		t.Error("one token should be back after 1.1 s at 1 per second")
	}
	if ok, _, _ := b.Allow(ctx, "c"); ok {
		t.Error("only one token came back")
	}
}

func TestClientsHaveSeparateBuckets(t *testing.T) {
	b, _ := newBucket(t, 1, 0.1)
	ctx := context.Background()
	b.Allow(ctx, "abuser")
	if ok, _, _ := b.Allow(ctx, "abuser"); ok {
		t.Fatal("the abuser's bucket should be empty")
	}
	if ok, _, _ := b.Allow(ctx, "normal"); !ok {
		t.Error("another client must not pay for the abuser")
	}
}

func TestBucketNeverHoldsMoreThanCapacity(t *testing.T) {
	b, mr := newBucket(t, 3, 100)
	ctx := context.Background()
	b.Allow(ctx, "c")
	mr.SetTime(time.Now().Add(time.Hour))
	passed := 0
	for range 10 {
		if ok, _, _ := b.Allow(ctx, "c"); ok {
			passed++
		}
	}
	if passed != 3 {
		t.Errorf("passed = %d after a long wait, want the capacity, 3", passed)
	}
}

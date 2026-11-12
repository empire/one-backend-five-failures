package session

import (
	"context"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
)

// Both stores must keep the same promise, so they run the same test.
func contract(t *testing.T, s Store) {
	ctx := context.Background()

	id, err := s.Create(ctx, "ana")
	if err != nil {
		t.Fatal(err)
	}
	user, ok, err := s.User(ctx, id)
	if err != nil || !ok || user != "ana" {
		t.Fatalf("User(%q) = %q, %v, %v; want ana, true, nil", id, user, ok, err)
	}
	if _, ok, err := s.User(ctx, "no-such-session"); err != nil || ok {
		t.Fatalf("unknown session: ok = %v, err = %v; want false, nil", ok, err)
	}
}

func TestMemory(t *testing.T) { contract(t, NewMemory()) }

// The reason for step 1: a session made through one app must be visible through the other.
func TestMemoryIsPerProcess(t *testing.T) {
	app1, app2 := NewMemory(), NewMemory()
	id, _ := app1.Create(context.Background(), "ana")
	if _, ok, _ := app2.User(context.Background(), id); ok {
		t.Fatal("app2 knows a session that only app1 created")
	}
}

// Needs a Redis: TEST_REDIS_URL=redis://localhost:6379/0 go test ./internal/session
func TestRedisIsShared(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL not set")
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	app1, app2 := NewRedis(redis.NewClient(opt)), NewRedis(redis.NewClient(opt))
	contract(t, app1)

	id, _ := app1.Create(context.Background(), "ana")
	if user, ok, _ := app2.User(context.Background(), id); !ok || user != "ana" {
		t.Fatalf("app2 cannot see app1's session: %q, %v", user, ok)
	}
}

package links

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeDB struct {
	rows    map[string]string
	lookups int
}

func (d *fakeDB) Create(_ context.Context, url string) (string, error) { return "new", nil }
func (d *fakeDB) Delete(_ context.Context, code string) error          { delete(d.rows, code); return nil }
func (d *fakeDB) Lookup(_ context.Context, code string) (string, bool, error) {
	d.lookups++
	url, ok := d.rows[code]
	return url, ok, nil
}

type fakeCache struct {
	m    map[string]string
	fail bool
	gets int
}

func (c *fakeCache) Get(_ context.Context, key string) (string, bool, error) {
	c.gets++
	if c.fail {
		return "", false, errors.New("cache is down")
	}
	v, ok := c.m[key]
	return v, ok, nil
}

func (c *fakeCache) Set(_ context.Context, key, value string, _ time.Duration) error {
	if c.fail {
		return errors.New("cache is down")
	}
	c.m[key] = value
	return nil
}

func (c *fakeCache) Del(_ context.Context, key string) error { delete(c.m, key); return nil }

func setup() (*Cached, *fakeDB, *fakeCache) {
	db := &fakeDB{rows: map[string]string{"abc": "https://example.com"}}
	cache := &fakeCache{m: map[string]string{}}
	return NewCached(db, cache, time.Minute), db, cache
}

func TestMissReadsDatabaseOnceThenHits(t *testing.T) {
	c, db, _ := setup()
	for range 3 {
		url, ok, err := c.Lookup(context.Background(), "abc")
		if err != nil || !ok || url != "https://example.com" {
			t.Fatalf("got %q %v %v", url, ok, err)
		}
	}
	if db.lookups != 1 {
		t.Errorf("database lookups = %d, want 1", db.lookups)
	}
	if hits, misses := c.Stats(); hits != 2 || misses != 1 {
		t.Errorf("hits, misses = %d, %d, want 2, 1", hits, misses)
	}
}

func TestUnknownCodeIsNotCached(t *testing.T) {
	c, db, cache := setup()
	for range 2 {
		if _, ok, _ := c.Lookup(context.Background(), "nope"); ok {
			t.Fatal("found a code that does not exist")
		}
	}
	if db.lookups != 2 || len(cache.m) != 0 {
		t.Errorf("lookups = %d, cached = %d, want 2 and 0", db.lookups, len(cache.m))
	}
}

func TestDeadCacheFallsBackToDatabaseAndIsSkipped(t *testing.T) {
	c, db, cache := setup()
	cache.fail = true
	for range 5 {
		url, ok, err := c.Lookup(context.Background(), "abc")
		if err != nil || !ok || url != "https://example.com" {
			t.Fatalf("got %q %v %v", url, ok, err)
		}
	}
	if db.lookups != 5 {
		t.Errorf("database lookups = %d, want 5", db.lookups)
	}
	if cache.gets != 1 {
		t.Errorf("the dead cache was asked %d times, want 1 (it is skipped for a while)", cache.gets)
	}
}

func TestCacheIsUsedAgainAfterItRecovers(t *testing.T) {
	c, _, cache := setup()
	cache.fail = true
	_, _, _ = c.Lookup(context.Background(), "abc")
	cache.fail = false
	c.skipUntil.Store(0) // the skip time has passed
	_, _, _ = c.Lookup(context.Background(), "abc")
	_, _, _ = c.Lookup(context.Background(), "abc")
	if hits, _ := c.Stats(); hits != 1 {
		t.Errorf("hits = %d, want 1", hits)
	}
}

func TestDeleteRemovesTheLinkAndItsCachedCopy(t *testing.T) {
	c, _, cache := setup()
	_, _, _ = c.Lookup(context.Background(), "abc") // now cached
	if err := c.Delete(context.Background(), "abc"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.Lookup(context.Background(), "abc"); ok {
		t.Error("a deleted link is still found")
	}
	if len(cache.m) != 0 {
		t.Error("the cached copy is still there")
	}
}

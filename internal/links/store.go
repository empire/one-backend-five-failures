// Package links stores short links in Postgres.
package links

import (
	"context"
	"crypto/rand"
	"errors"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schema = `CREATE TABLE IF NOT EXISTS links (
	code       text PRIMARY KEY,
	url        text NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now()
)`

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// Store reads and writes links. Creates and deletes go to the primary; reads go to the replica when there is one.
type Store struct {
	pool    *pgxpool.Pool // the primary: every write
	read    *pgxpool.Pool // the replica, or the primary again when there is no replica
	lookups atomic.Int64  // database reads started, shown at /debug/vars
}

// Open connects to the primary, makes sure the table exists, and, when replicaDSN is not empty, connects to the replica for reads.
// The table is made on the primary only: a replica is read-only, and it receives the table from the primary.
func Open(ctx context.Context, dsn, replicaDSN string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, err
	}
	s := &Store{pool: pool, read: pool}
	if replicaDSN != "" {
		if s.read, err = pgxpool.New(ctx, replicaDSN); err != nil {
			pool.Close()
			return nil, err
		}
	}
	return s, nil
}

// Create saves url under a new random 7-character code (base 62).
func (s *Store) Create(ctx context.Context, url string) (string, error) {
	for range 5 {
		code, err := newCode()
		if err != nil {
			return "", err
		}
		tag, err := s.pool.Exec(ctx,
			`INSERT INTO links (code, url) VALUES ($1, $2) ON CONFLICT DO NOTHING`, code, url)
		if err != nil {
			return "", err
		}
		if tag.RowsAffected() == 1 {
			return code, nil
		}
	}
	return "", errors.New("could not find a free code")
}

// Lookup returns the long URL for code. ok is false when the code does not exist.
func (s *Store) Lookup(ctx context.Context, code string) (url string, ok bool, err error) {
	s.lookups.Add(1)
	err = s.read.QueryRow(ctx, `SELECT url FROM links WHERE code = $1`, code).Scan(&url)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return url, err == nil, err
}

// Delete removes a link. It is not an error if the code does not exist.
func (s *Store) Delete(ctx context.Context, code string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM links WHERE code = $1`, code)
	return err
}

// Lookups returns how many database reads have started.
func (s *Store) Lookups() int64 { return s.lookups.Load() }

func newCode() (string, error) {
	b := make([]byte, 7)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}

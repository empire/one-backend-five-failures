// Package session keeps logged-in sessions.
//
// Step 1: a Store is either this process's memory (Memory) or a shared store (Redis).
// With more than one app, only the shared store works: any app must be able to serve any user.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// Store maps a session ID to the user who logged in.
type Store interface {
	// Create starts a session for user and returns its ID.
	Create(ctx context.Context, user string) (id string, err error)
	// User returns the user of a session. ok is false when the session does not exist.
	User(ctx context.Context, id string) (user string, ok bool, err error)
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

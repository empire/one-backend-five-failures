// Package session keeps logged-in sessions.
//
// Step 0: sessions live in this process's memory.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// Store maps a session ID to the user who logged in.
type Store struct {
	mu    sync.RWMutex
	users map[string]string
}

// NewMemory returns a store that keeps every session in memory.
func NewMemory() *Store {
	return &Store{users: map[string]string{}}
}

// Create starts a session for user and returns its ID.
func (s *Store) Create(user string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	s.mu.Lock()
	s.users[id] = user
	s.mu.Unlock()
	return id
}

// User returns the user of a session, if the session exists.
func (s *Store) User(id string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	return u, ok
}

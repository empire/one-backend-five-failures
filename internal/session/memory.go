package session

import (
	"context"
	"sync"
)

// Memory keeps every session in this process's memory.
// Another app instance has never seen these sessions.
type Memory struct {
	mu    sync.RWMutex
	users map[string]string
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{users: map[string]string{}}
}

func (s *Memory) Create(_ context.Context, user string) (string, error) {
	id := newID()
	s.mu.Lock()
	s.users[id] = user
	s.mu.Unlock()
	return id, nil
}

func (s *Memory) User(_ context.Context, id string) (string, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	return u, ok, nil
}

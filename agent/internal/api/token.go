package api

import "sync"

// TokenSource holds the agent's bearer token in a way that can change at
// runtime (the web panel can update it). The auth middleware reads the current
// value on every request instead of capturing a stale string at startup.
type TokenSource struct {
	mu    sync.RWMutex
	token string
}

func NewTokenSource(initial string) *TokenSource {
	return &TokenSource{token: initial}
}

func (t *TokenSource) Get() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.token
}

func (t *TokenSource) Set(token string) {
	t.mu.Lock()
	t.token = token
	t.mu.Unlock()
}

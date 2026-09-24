package pixiv

import (
	"strings"
	"sync"
)

// Pool round-robins the preset service refresh tokens used for anonymous
// public reads (pixiv.service_refresh_tokens). Evict removes a token Pixiv
// rejected (invalid_grant) so the rotation stops handing out a dead token;
// once drained the pool serves nothing and callers fall back to their
// existing gate (no session → not authenticated).
type Pool struct {
	mu     sync.Mutex
	tokens []string
	idx    int
}

// NewPool copies tokens so later caller-side mutation can't race with
// Next/Evict, trimming surrounding whitespace and dropping blanks so every
// pooled token is a canonical, non-empty string (config file and env sources
// may carry stray spaces). A nil/empty slice yields an empty pool.
func NewPool(tokens []string) *Pool {
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return &Pool{tokens: out}
}

// Next returns the next refresh token in round-robin order, or ok=false
// when the pool is empty.
func (p *Pool) Next() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.tokens) == 0 {
		return "", false
	}
	t := p.tokens[p.idx%len(p.tokens)]
	p.idx++
	return t, true
}

// HasTokens reports whether the pool still holds at least one token, WITHOUT
// advancing the rotation cursor. Read gates use this to check availability —
// calling Next for an existence probe would steal a rotation slot.
func (p *Pool) HasTokens() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.tokens) > 0
}

// Evict removes every occurrence of refreshToken from the pool and rewinds
// the rotation cursor. Safe to call for a token that was never pooled.
func (p *Pool) Evict(refreshToken string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.tokens[:0]
	for _, t := range p.tokens {
		if t != refreshToken {
			out = append(out, t)
		}
	}
	p.tokens = out
	p.idx = 0
}

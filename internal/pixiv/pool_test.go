package pixiv

import "testing"

func TestPoolEvictsRejectedToken(t *testing.T) {
	p := NewPool([]string{"good", "bad"})
	p.Evict("bad")
	got, ok := p.Next()
	if !ok || got != "good" {
		t.Fatalf("got %q,%v want good,true", got, ok)
	}
	if _, ok := p.Next(); !ok {
		t.Fatal("pool should still serve good")
	}
}

// An empty (or fully evicted) pool serves nothing instead of a blank token.
func TestPoolNextEmpty(t *testing.T) {
	p := NewPool(nil)
	if _, ok := p.Next(); ok {
		t.Fatal("empty pool should not serve a token")
	}
	p2 := NewPool([]string{"only"})
	p2.Evict("only")
	if _, ok := p2.Next(); ok {
		t.Fatal("pool drained by eviction should not serve a token")
	}
}

// NewPool must copy the input: a later mutation of the caller's slice
// (e.g. reusing a config buffer) must not race with Next/Evict.
func TestNewPoolCopiesInput(t *testing.T) {
	in := []string{"stable"}
	p := NewPool(in)
	in[0] = "mutated"
	if got, ok := p.Next(); !ok || got != "stable" {
		t.Fatalf("got %q,%v want stable,true", got, ok)
	}
}

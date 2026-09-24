package api

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/txperl/PixivBiu/internal/config"
	"github.com/txperl/PixivBiu/internal/pixiv"
	"github.com/txperl/PixivBiu/internal/state"
)

// unauthedService builds a pixiv.Service with an empty state file, so
// Authenticated() is false without any network round-trip.
func unauthedService(t *testing.T) *pixiv.Service {
	t.Helper()
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	svc, err := pixiv.NewService(config.PixivConfig{}, slog.New(slog.DiscardHandler), store)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

// publicService is an unauthenticated Service switched into public mode with
// a populated pool — the anonymous public-site configuration.
func publicService(t *testing.T) *pixiv.Service {
	t.Helper()
	svc := unauthedService(t)
	svc.SetPublicRead(true)
	svc.ReloadPool([]string{"tok"})
	return svc
}

func gateReq() *http.Request { return httptest.NewRequest(http.MethodGet, "/illusts/1", nil) }

func TestRequirePublicReadAnonymous(t *testing.T) {
	h := &APIHandler{}
	if err := h.requirePublicRead(gateReq()); err == nil {
		t.Fatal("want error when svc is nil")
	}
	// Local mode, unauthenticated, even with pool tokens configured: the
	// public-read switch is what authorizes anonymous reads, not the pool.
	svc := unauthedService(t)
	svc.ReloadPool([]string{"tok"})
	if err := (&APIHandler{svc: svc}).requirePublicRead(gateReq()); err == nil {
		t.Fatal("want error in local mode without a session")
	}
}

func TestRequirePublicRead_Gates(t *testing.T) {
	authed := authedService(t)
	unauthed := unauthedService(t)

	// Public mode: pool presence is the only pass — fail-closed even when the
	// operator session happens to be authenticated (public mode issues
	// upstream tokens from the pool exclusively).
	authedPublic := authedService(t)
	authedPublic.SetPublicRead(true)
	unauthedPublic := publicService(t)
	unauthedEmptyPublic := unauthedService(t)
	unauthedEmptyPublic.SetPublicRead(true) // switch on, pool drained

	tests := []struct {
		name    string
		handler *APIHandler
		wantErr bool
	}{
		{"no svc", &APIHandler{}, true},
		{"local mode unauthenticated", &APIHandler{svc: unauthed}, true},
		{"local mode authenticated", &APIHandler{svc: authed}, false},
		{"public mode with pool", &APIHandler{svc: unauthedPublic}, false},
		{"public mode with empty pool", &APIHandler{svc: unauthedEmptyPublic}, true},
		{"public mode empty pool despite authed operator", &APIHandler{svc: authedPublic}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.handler.requirePublicRead(gateReq())
			if tt.wantErr && err == nil {
				t.Fatal("want error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("want nil, got %v", err)
			}
		})
	}
}

func TestRequireUserWrite_Gates(t *testing.T) {
	unauthed := unauthedService(t)
	authed := authedService(t)

	if err := (&APIHandler{svc: unauthed}).requireUserWrite(); err == nil {
		t.Error("want error for unauthenticated write")
	}
	// Writes stay operator-only even with public reads wide open.
	if err := (&APIHandler{svc: publicService(t)}).requireUserWrite(); err == nil {
		t.Error("want error for anonymous write with pool open")
	}
	if err := (&APIHandler{svc: authed}).requireUserWrite(); err != nil {
		t.Errorf("want nil for authenticated write, got %v", err)
	}
	// requireAuth keeps the same behavior for config/system call-sites.
	if err := (&APIHandler{svc: unauthed}).requireAuth(); err == nil {
		t.Error("want requireAuth to keep rejecting unauthenticated")
	}
}

// The server download surface is operator-only: anonymous POST must be
// rejected by the gate before download.Manager is ever touched (dl is nil
// here — reaching it would panic), even when public reads are wide open.
func TestSubmitDownload_AnonymousRejectedBeforeManager(t *testing.T) {
	h := NewHandler(publicService(t), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "")

	rec := httptest.NewRecorder()
	h.SubmitDownload(rec, httptest.NewRequest(http.MethodPost, "/downloads", strings.NewReader(`{"illust_id":1}`)))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

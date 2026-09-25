package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/txperl/PixivBiu/internal/api"
	"github.com/txperl/PixivBiu/internal/config"
	"github.com/txperl/PixivBiu/internal/pixiv"
	"github.com/txperl/PixivBiu/internal/state"
)

// newTestService builds a pixiv.Service with an optional seeded session, so
// the handler's mode flag is real without any network round-trip.
func newTestService(t *testing.T, seedToken bool) *pixiv.Service {
	t.Helper()
	store := state.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if seedToken {
		if err := store.Save(state.Token{AccessToken: "at", RefreshToken: "rt"}); err != nil {
			t.Fatalf("seed token: %v", err)
		}
	}
	svc, err := pixiv.NewService(config.PixivConfig{}, slog.New(slog.DiscardHandler), store)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func newTestServer(t *testing.T, seedToken, publicMode bool) http.Handler {
	t.Helper()
	svc := newTestService(t, seedToken)
	if publicMode {
		svc.SetPublicRead(true)
		svc.ReloadPool([]string{"tok"})
	}
	h := api.NewHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "")
	return New(&config.Config{}, slog.New(slog.DiscardHandler), h)
}

// The dev docs are operator surfaces: in public mode /docs and /openapi.json
// must answer the structured 404 envelope instead of advertising the endpoint
// contract (including the closed config/auth endpoints) to anonymous callers.
func TestDocsClosedInPublicMode(t *testing.T) {
	srv := newTestServer(t, false, true)

	for _, path := range []string{"/docs", "/openapi.json"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
			var env api.Error
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			if env.Code != api.ErrorCodeNotFound || env.Kind != api.ErrorKindApp {
				t.Errorf("envelope = {code:%s kind:%s}, want {not_found app}", env.Code, env.Kind)
			}
		})
	}
}

// Local mode keeps the dev docs: the spec endpoint serves the embedded
// document and /docs the Scalar page.
func TestDocsOpenInLocalMode(t *testing.T) {
	srv := newTestServer(t, false, false)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("openapi.json: status = %d, want 200", rec.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	if _, ok := doc["openapi"]; !ok {
		t.Error("spec response missing the openapi version field")
	}

	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("docs: status = %d, want 200", rec2.Code)
	}
}

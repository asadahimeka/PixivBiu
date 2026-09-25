package server

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httplog/v3"

	"github.com/txperl/PixivBiu/internal/api"
	"github.com/txperl/PixivBiu/internal/config"
	"github.com/txperl/PixivBiu/internal/web"
)

const apiBase = "/api/v1"

func New(cfg *config.Config, logger *slog.Logger, h *api.APIHandler) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(httplog.RequestLogger(logger, &httplog.Options{
		Level:         slog.LevelInfo,
		Schema:        httplog.SchemaECS,
		RecoverPanics: true,
	}))
	// Per-IP read budget for API reads (public-site abuse guard): sits AFTER
	// httplog so overflow responses still flow through RequestID → RealIP →
	// httplog (logged with error.type=rate_limited via WriteError) and the
	// single Recoverer stays untouched; BEFORE the generated routes so every
	// /api/v1 handler passes through it. Health and the image proxy are
	// exempt inside the middleware; SPA/docs are outside the API base.
	r.Use(api.ReadRateLimit(apiBase))

	// Dev docs. /docs renders Scalar API Reference; /openapi.json feeds it
	// from the oapi-codegen embedded spec. Both sit outside /api/v1, and both
	// are operator surfaces: a public deployment must not advertise its
	// endpoint contract (including the closed config/auth endpoints) to
	// anonymous visitors, so the live public_read_enabled flag answers the
	// structured 404 envelope instead — same rule the auth and system
	// surfaces enforce inside /api/v1.
	openIfLocal := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, req *http.Request) {
			if h.PublicReadMode() {
				api.WriteError(w, req, api.ErrPublicModeClosed)
				return
			}
			next(w, req)
		}
	}
	r.Get("/docs", openIfLocal(handleDocs))
	r.Get("/openapi.json", openIfLocal(handleOpenAPI))

	// Everything not matched by the API or docs falls through here. The
	// oapi-codegen routes are registered flat on this same router, so an
	// unmapped /api/v1/* path lands here too — answer those with the
	// structured JSON 404 envelope; serve the embedded SPA for the rest
	// (with index.html fallback for client-side routes).
	spa := web.Handler()
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, apiBase) {
			api.WriteError(w, req, &api.RouteNotFoundError{Method: req.Method, Path: req.URL.Path})
			return
		}
		spa.ServeHTTP(w, req)
	})

	return api.HandlerWithOptions(h, api.ChiServerOptions{
		BaseURL:          apiBase,
		BaseRouter:       r,
		ErrorHandlerFunc: api.WriteError,
	})
}

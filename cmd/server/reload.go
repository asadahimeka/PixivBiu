package main

import (
	"log/slog"

	"github.com/txperl/PixivBiu/internal/config"
)

// registerReloadHooks wires the config-manager reload callbacks that apply
// live (non-restart) settings on PATCH. It must run after newApp so every
// service the hooks close over exists.
//
// Reload hooks each take the whole *Config because some keys cross
// service boundaries — pixiv.proxy is reused by the download client.
// Restart-required keys are pinned inside each Reload, so passing the
// full new config is safe.
func (a *app) registerReloadHooks() {
	a.cfgMgr.OnReload(func(n *config.Config) {
		if lvl, err := parseLogLevel(n.Log.Level); err == nil {
			a.levelVar.Set(lvl)
		}
	})
	a.cfgMgr.OnReload(func(n *config.Config) {
		if err := a.svc.Reload(n.Pixiv); err != nil {
			a.logger.Error("pixiv config reload failed", slog.Any("error", err))
		}
	})
	// Anonymous service-token pool (pixiv.service_refresh_tokens) and the
	// public-read switch (pixiv.public_read_enabled) are hot: both only flip
	// in-memory state inside pixiv.Service, so this hook never blocks and
	// never re-enters Patch/Reset. SetPublicRead feeds the live read gate
	// (requirePublicRead / ReadRefreshToken) — no restart needed.
	a.cfgMgr.OnReload(func(n *config.Config) {
		a.svc.ReloadPool(n.Pixiv.ServiceRefreshTokens)
		a.svc.SetPublicRead(n.Pixiv.PublicReadEnabled)
	})
	a.cfgMgr.OnReload(func(n *config.Config) {
		if err := a.dlMgr.Reload(n.Download, n.Pixiv.Proxy); err != nil {
			a.logger.Error("download config reload failed", slog.Any("error", err))
		}
	})
	a.cfgMgr.OnReload(func(n *config.Config) {
		a.dlPub.SetThrottle(n.Inbox.ProgressThrottle)
	})
	a.cfgMgr.OnReload(func(n *config.Config) {
		a.hbAtomic.Store(int64(n.Inbox.Heartbeat))
	})
	a.cfgMgr.OnReload(func(n *config.Config) {
		a.searchPagesAtomic.Store(int64(n.Search.Sample.Pages))
		a.searchConcurrencyAtomic.Store(int64(n.Search.Sample.Concurrency))
	})
	a.cfgMgr.OnReload(func(n *config.Config) {
		a.updSvc.Reload(n.App.Update, n.Pixiv.Proxy)
	})
	a.cfgMgr.OnReload(func(n *config.Config) {
		if err := a.imgProxy.Reload(n.Image.Cache.MaxBytes(), n.Pixiv.Proxy, n.Download.HTTPTimeout); err != nil {
			a.logger.Error("image proxy reload failed", slog.Any("error", err))
		}
	})
}

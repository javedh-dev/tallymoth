package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/javedh-dev/tallymoth/internal/auth"
	"github.com/javedh-dev/tallymoth/internal/config"
	"github.com/javedh-dev/tallymoth/internal/ingest"
	"github.com/javedh-dev/tallymoth/internal/limit"
	"github.com/javedh-dev/tallymoth/internal/store"
)

func serve(path string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	var current atomic.Pointer[config.Config]
	current.Store(cfg)

	stores := store.NewManager(cfg.Server.DataDir)
	openAll := func(c *config.Config) {
		for _, t := range c.Tenants {
			if err := stores.Open(t.ID); err != nil {
				slog.Error("tenant disabled", "err", err) // other tenants keep working
			}
		}
	}
	openAll(cfg)

	h := &ingest.Handler{Config: &current, Stores: stores, Replay: auth.NewReplay(), Limit: limit.NewIP()}
	healthz := func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) }

	public := http.NewServeMux()
	public.HandleFunc("GET /healthz", healthz)
	h.Register(public, version)
	private := http.NewServeMux() // query API arrives in M2; keep it off the public port
	private.HandleFunc("GET /healthz", healthz)

	servers := []*http.Server{newServer(cfg.Server.Listen.Public, public), newServer(cfg.Server.Listen.Private, private)}
	for _, s := range servers {
		go func() {
			if err := s.ListenAndServe(); err != http.ErrServerClosed {
				slog.Error("listen", "addr", s.Addr, "err", err)
				os.Exit(1)
			}
		}()
	}
	slog.Info("tallymoth started", "version", version, "public", cfg.Server.Listen.Public, "private", cfg.Server.Listen.Private, "tenants", len(cfg.Tenants))

	go pruneLoop(&current, stores)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	for s := range sig {
		if s == syscall.SIGHUP {
			next, err := config.Load(path)
			if err != nil {
				slog.Error("reload rejected, keeping old config", "err", err)
				continue
			}
			if o, n := current.Load().Server, next.Server; o.Listen != n.Listen || o.DataDir != n.DataDir {
				slog.Warn("listen addresses and data_dir only change on restart; ignoring them")
				next.Server.Listen, next.Server.DataDir = o.Listen, o.DataDir
			}
			current.Store(next)
			keep := map[string]bool{}
			for _, t := range next.Tenants {
				keep[t.ID] = true
			}
			stores.Retain(keep) // removed tenants stop accepting; their data stays on disk
			openAll(next)
			slog.Info("config reloaded", "tenants", len(next.Tenants))
			continue
		}
		break
	}

	slog.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, s := range servers {
		s.Shutdown(ctx)
	}
	stores.Close() // after the servers stop: flushes queued events
	return nil
}

func newServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: h,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 8 << 10,
	}
}

// pruneLoop applies each tenant's retention hourly.
func pruneLoop(cfg *atomic.Pointer[config.Config], stores *store.Manager) {
	for {
		for _, t := range cfg.Load().Tenants {
			if db := stores.Get(t.ID); db != nil {
				if err := db.Prune(t.RetentionDays); err != nil {
					slog.Error("prune", "tenant", t.ID, "err", err)
				}
			}
		}
		time.Sleep(time.Hour)
	}
}

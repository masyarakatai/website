// Command web serves the HuShield marketing site.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Hushield/website/internal/cache"
	"github.com/Hushield/website/internal/catalog"
	"github.com/Hushield/website/internal/config"
	"github.com/Hushield/website/internal/github"
	"github.com/Hushield/website/internal/handlers"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags|log.Lmsgprefix)

	if err := run(logger); err != nil {
		logger.Fatalf("fatal: %v", err)
	}
}

func run(logger *log.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Fail fast on a malformed catalog rather than rendering a broken page.
	if problems := catalog.Validate(); len(problems) > 0 {
		for _, p := range problems {
			logger.Printf("catalog: %s", p)
		}
		return errors.New("catalog validation failed")
	}

	if !cfg.TokenConfigured() {
		logger.Print("WARNING: GITHUB_TOKEN is unset. Unauthenticated requests are capped at " +
			"60/hour, below what the poll schedule needs, so datasets will go stale intermittently.")
	}

	client := github.New(cfg.Owner, cfg.Repo, cfg.GitHubToken)
	snaps := cache.NewSnapshots()
	poller := cache.NewPoller(client, snaps, cache.Intervals{Unauthenticated: !cfg.TokenConfigured()}, logger)

	srv, err := handlers.New(cfg, snaps, logger)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Prime the cache before accepting traffic so the first visitor does not see
	// an "unavailable" page. Bounded so a slow GitHub cannot block startup
	// indefinitely -- if it times out, pages render the unavailable state and
	// the poller keeps retrying.
	primeCtx, cancelPrime := context.WithTimeout(ctx, 25*time.Second)
	poller.RefreshAll(primeCtx)
	cancelPrime()

	go poller.Run(ctx)

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Printf("hushield website listening on %s (repo %s/%s)", cfg.Addr, cfg.Owner, cfg.Repo)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Print("shutting down")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutCtx)
	}
}

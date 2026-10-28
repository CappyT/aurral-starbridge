// Command aurral-starbridge adds the Aurral flow tracks that a user stars in Navidrome
// to the Aurral Library, so a flow rotation does not delete them.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		log.Error("invalid config", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	client := &http.Client{Timeout: 30 * time.Second}
	b := &bridge{
		nd: &navidrome{
			baseURL:  strings.TrimRight(cfg.NavidromeURL, "/"),
			user:     cfg.NavidromeUser,
			password: cfg.NavidromePassword,
			http:     client,
		},
		au: &aurral{
			baseURL: strings.TrimRight(cfg.AurralURL, "/"),
			user:    cfg.AurralUser,
			groups:  cfg.AurralGroups,
			http:    client,
		},
		navidromeRoot: cfg.NavidromeRoot,
		aurralRoot:    cfg.AurralRoot,
		log:           log,
		handled:       map[string]bool{},
		jobPaths:      map[string]string{},
	}
	log.Info("starting", "navidrome", cfg.NavidromeURL, "aurral", cfg.AurralURL, "user", cfg.AurralUser, "interval", cfg.Interval.String())

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		st := b.cycle(ctx)
		if ctx.Err() != nil {
			break
		}
		log.Info("cycle done", "starred", st.Starred, "kept", st.Kept, "already_kept", st.AlreadyKept, "unmatched", st.Unmatched, "failed", st.Failed)
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	log.Info("stopped")
}

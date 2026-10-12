// Command example is a starting point for a Loop plugin: an action and a
// tool that count the channel's files and lines, and a panel showing the
// last count.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/radutopala/loop/pluginsdk"

	"github.com/radutopala/loop-plugins/template/internal/server"
	"github.com/radutopala/loop-plugins/template/internal/store"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Getenv, pluginsdk.ManifestPath, logger, nil)
	stop()
	if err != nil {
		logger.Error("example stopped", "err", err)
		os.Exit(1)
	}
}

// run serves the plugin until ctx ends, reporting the version of the
// manifest at manifestPath. ready, when set, gets the listening address.
func run(ctx context.Context, getenv func(string) string, manifestPath string, logger *slog.Logger, ready func(addr string)) error {
	env := pluginsdk.LoadEnv(getenv)
	secret, err := env.Secret()
	if err != nil {
		return err
	}
	m, err := pluginsdk.ReadManifest(manifestPath)
	if err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(env.DataDir, "state.json"))
	if err != nil {
		return err
	}
	srv := server.New(env.Client(), st, secret, m.Version, logger)
	defer srv.Close()

	ln, err := env.Listen()
	if err != nil {
		return err
	}
	logger.Info("example listening", "addr", ln.Addr().String(), "channel", env.ChannelID, "version", m.Version)
	if ready != nil {
		ready(ln.Addr().String())
	}
	return pluginsdk.Serve(ctx, ln, srv.Handler())
}

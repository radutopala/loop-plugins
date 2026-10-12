// Command cogtest is a Loop plugin that quizzes the developer on the
// channel's code: an agent writes multiple-choice questions anchored in the
// code, and a panel asks them and grades the answers.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/radutopala/loop/pluginsdk"

	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/quiz"
	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/server"
	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/store"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Getenv, pluginsdk.ManifestPath, logger, nil)
	stop()
	if err != nil {
		logger.Error("cogtest stopped", "err", err)
		os.Exit(1)
	}
}

// run serves cogtest until ctx ends, reporting the version of the manifest
// at manifestPath. ready, when set, gets the listening address.
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
	st, err := store.Open(ctx, filepath.Join(env.DataDir, "cogtest.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	svc := quiz.New(st, env.Client(), logger)
	svc.Recover(ctx)
	srv := server.New(svc, secret, m.Version, logger)
	defer srv.Close()

	ln, err := env.Listen()
	if err != nil {
		return err
	}
	logger.Info("cogtest listening", "addr", ln.Addr().String(), "channel", env.ChannelID, "version", m.Version)
	if ready != nil {
		ready(ln.Addr().String())
	}
	return pluginsdk.Serve(ctx, ln, srv.Handler())
}

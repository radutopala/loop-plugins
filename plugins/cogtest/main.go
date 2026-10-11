// Command cogtest is a Loop plugin that quizzes the developer on the
// channel's code: an agent writes multiple-choice questions anchored in the
// code, and a panel asks them and grades the answers.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/loopapi"
	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/quiz"
	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/server"
	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/store"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "0.3.0"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Getenv, logger, nil)
	stop()
	if err != nil {
		logger.Error("cogtest stopped", "err", err)
		os.Exit(1)
	}
}

// run serves cogtest until ctx ends. ready, when set, gets the listening
// address.
func run(ctx context.Context, env func(string) string, logger *slog.Logger, ready func(addr string)) error {
	secret, err := os.ReadFile(env("LOOP_PLUGIN_SECRET_FILE"))
	if err != nil {
		return fmt.Errorf("reading the secret: %w", err)
	}
	dataDir := env("LOOP_DATA_DIR")
	if dataDir == "" {
		dataDir = "/data"
	}
	st, err := store.Open(ctx, filepath.Join(dataDir, "cogtest.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	svc := quiz.New(st, loopapi.New(env("LOOP_API_URL"), env("LOOP_PLUGIN_TOKEN_FILE")), logger)
	srv := server.New(svc, strings.TrimSpace(string(secret)), version, logger)
	defer srv.Close()

	ln, err := net.Listen("tcp", ":"+env("LOOP_PLUGIN_PORT"))
	if err != nil {
		return err
	}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	logger.Info("cogtest listening", "addr", ln.Addr().String(), "channel", env("LOOP_CHANNEL_ID"), "version", version)
	if ready != nil {
		ready(ln.Addr().String())
	}
	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := hs.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

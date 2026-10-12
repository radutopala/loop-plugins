//go:build ui

package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/radutopala/loop/pluginsdk"
	"github.com/radutopala/loop/pluginsdk/pluginsdktest"

	"github.com/radutopala/loop-plugins/template/internal/server"
	"github.com/radutopala/loop-plugins/template/internal/store"
)

// TestUI loads the built panel in headless Chrome under Loop's content
// security policy, with its /x calls answered by the plugin's own handler.
// Build the UI first (cd web && npm run build), then run
// go test -tags ui -run TestUI .
func TestUI(t *testing.T) {
	_, err := os.Stat("web/dist/index.html")
	require.NoError(t, err, "build the UI first: cd web && npm run build")

	// Lay the bundle out as the image's /loop-plugin dir.
	dir := t.TempDir()
	manifest, err := os.ReadFile("plugin.json")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.json"), manifest, 0o600))
	require.NoError(t, os.CopyFS(filepath.Join(dir, "ui", "main"), os.DirFS("web/dist")))

	m, err := pluginsdk.ParseManifest(manifest)
	require.NoError(t, err)
	loop := pluginsdktest.NewFakeLoop(t, pluginsdktest.Config{Manifest: m, Files: map[string]string{"main.go": "package main\n"}})
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)
	srv := server.New(loop.Client(), st, "sekret", m.Version, slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	problems, err := pluginsdktest.CheckUI(ctx, dir, pluginsdktest.UIOptions{API: srv.X})
	require.NoError(t, err)
	require.Empty(t, problems)
}

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

	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/quiz"
	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/server"
	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/store"
)

// TestUI loads the built panel and summary card in headless Chrome under
// Loop's content security policy, with the panel's /x calls answered by the
// plugin's own handler. Build the UI first (cd web && npm run build), then
// run go test -tags ui -run TestUI .
func TestUI(t *testing.T) {
	_, err := os.Stat("web/dist/quiz/index.html")
	require.NoError(t, err, "build the UI first: cd web && npm run build")

	// Lay the bundle out as the image's /loop-plugin dir.
	dir := t.TempDir()
	manifest, err := os.ReadFile("plugin.json")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin.json"), manifest, 0o600))
	require.NoError(t, os.CopyFS(filepath.Join(dir, "ui"), os.DirFS("web/dist")))

	m, err := pluginsdk.ParseManifest(manifest)
	require.NoError(t, err)
	loop := pluginsdktest.NewFakeLoop(t, pluginsdktest.Config{Manifest: m, Files: map[string]string{"main.go": "package main\n"}})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "cogtest.db"))
	require.NoError(t, err)
	defer st.Close()
	srv := server.New(quiz.New(st, loop.Client(), logger), "sekret", m.Version, logger)
	defer srv.Close()

	card := map[string]any{
		"quiz_id": 1,
		"date":    "2026-10-12",
		"score":   map[string]any{"total": 2, "answered": 2, "correct": 1},
		"streak":  3,
		"max_box": 5,
		"questions": []any{
			map[string]any{"question": "What does run return?", "area": "main", "path": "main.go", "line": 1, "correct": true},
			map[string]any{"question": "Where is the store opened?", "area": "main", "correct": false, "reask": true},
		},
		"moves": []any{map[string]any{"area": "main", "from": 1, "to": 2}},
	}
	problems, err := pluginsdktest.CheckUI(ctx, dir, pluginsdktest.UIOptions{API: srv.X, CardData: map[string]any{"summary": card}})
	require.NoError(t, err)
	require.Empty(t, problems)
}

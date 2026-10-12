// Package server serves the Loop → plugin contract: the count action, the
// count_lines tool and the panel's /x API, all behind the secret Loop
// hands the container.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/radutopala/loop/pluginsdk"

	"github.com/radutopala/loop-plugins/template/internal/store"
)

// Loop is the part of the Plugin API the plugin calls; *pluginsdk.Client
// implements it.
type Loop interface {
	Channel(ctx context.Context) (*pluginsdk.Channel, error)
	File(ctx context.Context, path string, start, end int) (*pluginsdk.File, error)
	Tree(ctx context.Context, glob string) (*pluginsdk.Tree, error)
	Notify(ctx context.Context, title, body string) error
}

// Server is the plugin's HTTP handler.
type Server struct {
	*pluginsdk.Server
	loop  Loop
	store *store.Store
	now   func() time.Time
}

// New returns a Server that accepts requests carrying secret.
func New(loop Loop, st *store.Store, secret, version string, logger *slog.Logger) *Server {
	s := &Server{loop: loop, store: st, now: time.Now}
	x := http.NewServeMux()
	x.HandleFunc("GET /stats", s.stats)
	x.HandleFunc("POST /count", s.countNow)
	s.Server = &pluginsdk.Server{
		Secret:  secret,
		Version: version,
		Logger:  logger,
		Actions: map[string]pluginsdk.ActionFunc{"count": s.countAction},
		Tools:   map[string]pluginsdk.ToolFunc{"count_lines": s.countLines},
		X:       x,
	}
	return s
}

// count counts the channel's files matching the glob setting and records
// the count.
func (s *Server) count(ctx context.Context) (store.Count, error) {
	ch, err := s.loop.Channel(ctx)
	if err != nil {
		return store.Count{}, fmt.Errorf("reading the channel: %w", err)
	}
	glob, _ := ch.Settings["glob"].(string)
	tree, err := s.loop.Tree(ctx, glob)
	if err != nil {
		return store.Count{}, fmt.Errorf("listing the files: %w", err)
	}
	c := store.Count{Glob: glob, Files: len(tree.Files), Truncated: tree.Truncated, At: s.now().UTC()}
	if _, err := s.store.Add(c); err != nil {
		return store.Count{}, err
	}
	return c, nil
}

func (s *Server) countAction(ctx context.Context, _ pluginsdk.ActionRequest) error {
	c, err := s.count(ctx)
	if err != nil {
		return err
	}
	return s.loop.Notify(ctx, "Files counted", fmt.Sprintf("%d files", c.Files))
}

type countLinesInput struct {
	Path string `json:"path"`
}

// countLines answers the count_lines tool. Its errors reach the agent as
// a result with is_error set, so it can retry.
func (s *Server) countLines(ctx context.Context, req pluginsdk.ToolRequest) (pluginsdk.ToolResult, error) {
	var in countLinesInput
	if err := json.Unmarshal(req.Input, &in); err != nil {
		return pluginsdk.ToolResult{}, fmt.Errorf("decoding the input: %w", err)
	}
	if in.Path == "" {
		return pluginsdk.ToolResult{}, errors.New("path is required")
	}
	f, err := s.loop.File(ctx, in.Path, 0, 0)
	if pluginsdk.HasStatus(err, http.StatusNotFound) {
		return pluginsdk.ToolResult{}, fmt.Errorf("%s doesn't exist", in.Path)
	}
	if err != nil {
		return pluginsdk.ToolResult{}, fmt.Errorf("reading %s: %w", in.Path, err)
	}
	return pluginsdk.ToolResult{Content: fmt.Sprintf("%s has %d lines", in.Path, f.TotalLines)}, nil
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Load()
	if err != nil {
		s.internal(w, r, err)
		return
	}
	pluginsdk.WriteJSON(w, http.StatusOK, st)
}

func (s *Server) countNow(w http.ResponseWriter, r *http.Request) {
	if _, err := s.count(r.Context()); err != nil {
		s.internal(w, r, err)
		return
	}
	s.stats(w, r)
}

func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	pluginsdk.WriteError(w, http.StatusInternalServerError, err.Error())
}

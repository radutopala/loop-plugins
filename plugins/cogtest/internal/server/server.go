// Package server serves the Loop → plugin contract: the health check, the
// daily action, the submit_questions tool, run completion and the panel's
// /x API, all behind the secret Loop hands the container.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/quiz"
)

const (
	maxBody = 1 << 20
	// workTimeout caps work done after answering, such as starting a run.
	workTimeout = 2 * time.Minute
	// requestTimeout caps work done while Loop waits for the answer; Loop
	// itself gives a tool call 60 s and an /x call 30 s.
	requestTimeout = 50 * time.Second
)

// Server is cogtest's HTTP handler.
type Server struct {
	Quiz    *quiz.Service
	Secret  string
	Version string
	Logger  *slog.Logger

	// base is the parent of background work; cancel stops it.
	base   context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New returns a Server that accepts requests carrying secret.
func New(svc *quiz.Service, secret, version string, logger *slog.Logger) *Server {
	base, cancel := context.WithCancel(context.Background())
	return &Server{Quiz: svc, Secret: secret, Version: version, Logger: logger, base: base, cancel: cancel}
}

// Wait waits for background work to end.
func (s *Server) Wait() { s.wg.Wait() }

// Close cancels background work and waits for it.
func (s *Server) Close() {
	s.cancel()
	s.wg.Wait()
}

// background runs fn after the request is answered.
func (s *Server) background(name string, fn func(ctx context.Context) error) {
	s.wg.Go(func() {
		ctx, cancel := context.WithTimeout(s.base, workTimeout)
		defer cancel()
		if err := fn(ctx); err != nil {
			s.Logger.Info(name, "err", err)
		}
	})
}

// Handler returns the routes behind the secret check.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("POST /actions/{action}", s.action)
	mux.HandleFunc("POST /tools/{tool}", s.tool)
	mux.HandleFunc("POST /runs/{run_id}/done", s.runDone)
	mux.HandleFunc("GET /x/quizzes/current", s.currentQuiz)
	mux.HandleFunc("POST /x/quizzes", s.newQuiz)
	mux.HandleFunc("GET /x/quizzes/{id}", s.getQuiz)
	mux.HandleFunc("POST /x/attempts", s.attempt)
	mux.HandleFunc("GET /x/history", s.history)
	mux.HandleFunc("GET /x/settings", s.getSettings)
	mux.HandleFunc("PUT /x/settings", s.putSettings)
	want := []byte("Bearer " + s.Secret)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Secret == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, err.Error())
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"api": "1", "version": s.Version})
}

func (s *Server) action(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("action") != "daily" {
		writeError(w, http.StatusNotFound, "unknown action")
		return
	}
	var req struct {
		Trigger string `json:"trigger"`
	}
	// The body is optional; a missing or odd one counts as a manual trigger.
	_ = json.NewDecoder(r.Body).Decode(&req)
	trigger := req.Trigger
	if trigger == "" {
		trigger = "manual"
	}
	w.WriteHeader(http.StatusAccepted)
	s.background("daily action", func(ctx context.Context) error { return s.Quiz.Daily(ctx, trigger) })
}

func (s *Server) tool(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("tool") != quiz.ToolName {
		writeJSON(w, http.StatusNotFound, quiz.ToolResult{Content: "unknown tool", IsError: true})
		return
	}
	var req struct {
		RunID string          `json:"run_id"`
		Input json.RawMessage `json:"input"`
	}
	if !decode(w, r, &req) {
		return
	}
	res, err := s.Quiz.Submit(r.Context(), req.RunID, req.Input)
	if err != nil {
		s.Logger.Error("submit_questions", "err", err)
		res = quiz.ToolResult{Content: "cogtest could not store the questions: " + err.Error(), IsError: true}
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) runDone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.Quiz.RunDone(r.Context(), r.PathValue("run_id"), req.Status, req.Error); err != nil {
		s.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) currentQuiz(w http.ResponseWriter, r *http.Request) {
	v, err := s.Quiz.Today(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// getQuiz returns a past quiz with its questions and answers, for History.
func (s *Server) getQuiz(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad quiz id")
		return
	}
	v, err := s.Quiz.Quiz(r.Context(), id)
	if errors.Is(err, quiz.ErrNoQuiz) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) newQuiz(w http.ResponseWriter, r *http.Request) {
	q, err := s.Quiz.Begin(r.Context(), "manual", true)
	if errors.Is(err, quiz.ErrGenerating) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.background("starting a quiz", func(ctx context.Context) error { return s.Quiz.Launch(ctx, q) })
	writeJSON(w, http.StatusAccepted, map[string]any{"quiz": map[string]any{"id": q.ID, "status": q.Status}})
}

func (s *Server) attempt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QuestionID  int64 `json:"question_id"`
		ChosenIndex *int  `json:"chosen_index"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.QuestionID <= 0 || req.ChosenIndex == nil {
		writeError(w, http.StatusBadRequest, "question_id and chosen_index are required")
		return
	}
	g, err := s.Quiz.Answer(r.Context(), req.QuestionID, *req.ChosenIndex)
	switch {
	case errors.Is(err, quiz.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, quiz.ErrBadChoice):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, quiz.ErrAnswered):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		s.internal(w, r, err)
	default:
		writeJSON(w, http.StatusOK, g)
	}
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	v, err := s.Quiz.History(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	v, err := s.Quiz.Settings(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsBody(v))
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var v quiz.SettingsView
	if !decode(w, r, &v) {
		return
	}
	v.Model, v.Effort = strings.TrimSpace(v.Model), strings.TrimSpace(v.Effort)
	err := s.Quiz.SaveSettings(r.Context(), v)
	if errors.Is(err, quiz.ErrBadSettings) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsBody(v))
}

func settingsBody(v quiz.SettingsView) map[string]any {
	return map[string]any{"model": v.Model, "effort": v.Effort, "models": quiz.Models, "efforts": quiz.Efforts}
}

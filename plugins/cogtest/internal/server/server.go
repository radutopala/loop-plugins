// Package server serves the Loop → plugin contract: the health check, the
// daily action, the submit_questions tool, run completion and the panel's
// /x API, all behind the secret Loop hands the container.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/radutopala/loop/pluginsdk"

	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/quiz"
)

// Server is cogtest's HTTP handler.
type Server struct {
	*pluginsdk.Server
	Quiz *quiz.Service
}

// New returns a Server that accepts requests carrying secret.
func New(svc *quiz.Service, secret, version string, logger *slog.Logger) *Server {
	s := &Server{Quiz: svc}
	x := http.NewServeMux()
	x.HandleFunc("GET /quizzes/current", s.currentQuiz)
	x.HandleFunc("POST /quizzes/current/seen", s.seen)
	x.HandleFunc("POST /quizzes", s.newQuiz)
	x.HandleFunc("GET /quizzes/{id}", s.getQuiz)
	x.HandleFunc("POST /attempts", s.attempt)
	x.HandleFunc("GET /history", s.history)
	x.HandleFunc("GET /settings", s.getSettings)
	x.HandleFunc("PUT /settings", s.putSettings)
	s.Server = &pluginsdk.Server{
		Secret:  secret,
		Version: version,
		Logger:  logger,
		Actions: map[string]pluginsdk.ActionFunc{
			"daily": s.daily,
		},
		Tools: map[string]pluginsdk.ToolFunc{quiz.ToolName: s.submit},
		RunDone: func(ctx context.Context, runID string, req pluginsdk.RunDoneRequest) error {
			return svc.RunDone(ctx, runID, req.Status, req.Error)
		},
		X: x,
	}
	return s
}

// daily runs the daily action. A quiz already ready or generating, or a
// channel that isn't local, is how a day goes rather than a failure.
func (s *Server) daily(ctx context.Context, req pluginsdk.ActionRequest) error {
	err := s.Quiz.Daily(ctx, string(req.Trigger))
	if errors.Is(err, quiz.ErrDone) || errors.Is(err, quiz.ErrGenerating) || errors.Is(err, quiz.ErrNotLocal) {
		s.Logger.Info("daily action skipped", "reason", err)
		return nil
	}
	return err
}

func (s *Server) submit(ctx context.Context, req pluginsdk.ToolRequest) (pluginsdk.ToolResult, error) {
	res, err := s.Quiz.Submit(ctx, req.RunID, req.Input)
	if err != nil {
		return res, fmt.Errorf("cogtest could not store the questions: %w", err)
	}
	return res, nil
}

func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	pluginsdk.WriteError(w, http.StatusInternalServerError, err.Error())
}

func (s *Server) currentQuiz(w http.ResponseWriter, r *http.Request) {
	v, err := s.Quiz.Today(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	pluginsdk.WriteJSON(w, http.StatusOK, v)
}

// seen clears the badge once the panel shows today's quiz.
func (s *Server) seen(w http.ResponseWriter, r *http.Request) {
	if err := s.Quiz.Seen(r.Context()); err != nil {
		s.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getQuiz returns a past quiz with its questions and answers, for History.
func (s *Server) getQuiz(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		pluginsdk.WriteError(w, http.StatusBadRequest, "bad quiz id")
		return
	}
	v, err := s.Quiz.Quiz(r.Context(), id)
	if errors.Is(err, quiz.ErrNoQuiz) {
		pluginsdk.WriteError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	pluginsdk.WriteJSON(w, http.StatusOK, v)
}

func (s *Server) newQuiz(w http.ResponseWriter, r *http.Request) {
	q, err := s.Quiz.Begin(r.Context(), "manual", true)
	if errors.Is(err, quiz.ErrGenerating) {
		pluginsdk.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.Go("starting a quiz", func(ctx context.Context) error { return s.Quiz.Launch(ctx, q) })
	pluginsdk.WriteJSON(w, http.StatusAccepted, map[string]any{"quiz": map[string]any{"id": q.ID, "status": q.Status}})
}

func (s *Server) attempt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QuestionID  int64 `json:"question_id"`
		ChosenIndex *int  `json:"chosen_index"`
	}
	if !pluginsdk.DecodeJSON(w, r, &req) {
		return
	}
	if req.QuestionID <= 0 || req.ChosenIndex == nil {
		pluginsdk.WriteError(w, http.StatusBadRequest, "question_id and chosen_index are required")
		return
	}
	g, err := s.Quiz.Answer(r.Context(), req.QuestionID, *req.ChosenIndex)
	switch {
	case errors.Is(err, quiz.ErrNotFound):
		pluginsdk.WriteError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, quiz.ErrBadChoice):
		pluginsdk.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, quiz.ErrAnswered):
		pluginsdk.WriteError(w, http.StatusConflict, err.Error())
	case err != nil:
		s.internal(w, r, err)
	default:
		pluginsdk.WriteJSON(w, http.StatusOK, g)
	}
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	v, err := s.Quiz.History(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	pluginsdk.WriteJSON(w, http.StatusOK, v)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	v, err := s.Quiz.Settings(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	pluginsdk.WriteJSON(w, http.StatusOK, settingsBody(v))
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var v quiz.SettingsView
	if !pluginsdk.DecodeJSON(w, r, &v) {
		return
	}
	v.Model, v.Effort = strings.TrimSpace(v.Model), strings.TrimSpace(v.Effort)
	err := s.Quiz.SaveSettings(r.Context(), v)
	if errors.Is(err, quiz.ErrBadSettings) {
		pluginsdk.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	pluginsdk.WriteJSON(w, http.StatusOK, settingsBody(v))
}

func settingsBody(v quiz.SettingsView) map[string]any {
	return map[string]any{"model": v.Model, "effort": v.Effort, "models": quiz.Models, "efforts": quiz.Efforts}
}

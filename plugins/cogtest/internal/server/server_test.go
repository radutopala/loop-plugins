package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/loopapi"
	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/loopapi/loopapitest"
	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/quiz"
	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/store"
)

type ServerSuite struct {
	suite.Suite
	ctx  context.Context
	st   *store.Store
	loop *loopapitest.Client
	svc  *quiz.Service
	srv  *Server
	h    http.Handler
}

func TestServerSuite(t *testing.T) {
	suite.Run(t, new(ServerSuite))
}

func (s *ServerSuite) SetupTest() {
	s.ctx = context.Background()
	st, err := store.Open(s.ctx, filepath.Join(s.T().TempDir(), "cogtest.db"))
	s.Require().NoError(err)
	s.st = st
	s.loop = &loopapitest.Client{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s.svc = quiz.New(st, s.loop, logger)
	now := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	s.svc.Now = func() time.Time { return now }
	s.svc.Count = 1
	s.srv = New(s.svc, "sekret", "1.2.3", logger)
	s.h = s.srv.Handler()
}

func (s *ServerSuite) TearDownTest() {
	s.srv.Close()
	s.loop.AssertExpectations(s.T())
	_ = s.st.Close()
}

// do sends an authorised request and returns the status and decoded body.
func (s *ServerSuite) do(method, path, body string) (int, map[string]any) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer sekret")
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Body.Len() > 0 {
		s.Require().NoError(json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
	}
	return rec.Code, out
}

// readyQuiz stores a ready one-question quiz and returns the question id.
func (s *ServerSuite) readyQuiz() int64 {
	q, err := s.svc.Begin(s.ctx, "manual", true)
	s.Require().NoError(err)
	s.Require().NoError(s.st.SetRunID(s.ctx, q.ID, "r1", s.svc.Now()))
	s.Require().NoError(s.st.SaveQuestions(s.ctx, q.ID, []store.Question{{
		Question: "A?", Choices: []string{"a", "b", "c"}, CorrectIndex: 1, Explanation: "see a.go",
		Anchors: []store.Anchor{{Path: "a.go", Start: 1, End: 2}}, Fingerprint: "f", Area: ".",
	}}, s.svc.Now()))
	qs, err := s.st.Questions(s.ctx, q.ID)
	s.Require().NoError(err)
	return qs[0].ID
}

func (s *ServerSuite) TestAuth() {
	cases := []struct {
		name, secret, header string
	}{
		{"missing header", "sekret", ""},
		{"wrong secret", "sekret", "Bearer nope"},
		{"not bearer", "sekret", "sekret"},
		{"empty secret refuses everything", "", "Bearer "},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			srv := New(s.svc, tc.secret, "v", s.srv.Logger)
			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			s.Require().Equal(http.StatusUnauthorized, rec.Code)
			s.Require().JSONEq(`{"error":"unauthorized"}`, rec.Body.String())
		})
	}
}

func (s *ServerSuite) TestHealthz() {
	code, body := s.do(http.MethodGet, "/healthz", "")
	s.Require().Equal(http.StatusOK, code)
	s.Require().Equal(map[string]any{"api": "1", "version": "1.2.3"}, body)
}

func (s *ServerSuite) TestActions() {
	code, _ := s.do(http.MethodPost, "/actions/weekly", "{}")
	s.Require().Equal(http.StatusNotFound, code)

	for _, tc := range []struct{ body, platform string }{{`{"trigger":"schedule"}`, "slack"}, {``, "discord"}} {
		s.loop.On("Channel", mock.Anything).Return(&loopapi.Channel{Platform: tc.platform}, nil).Once()
		code, _ = s.do(http.MethodPost, "/actions/daily", tc.body)
		s.Require().Equal(http.StatusAccepted, code)
		s.srv.Wait()
	}
	_, err := s.st.LatestQuiz(s.ctx)
	s.Require().ErrorIs(err, store.ErrNotFound)
}

func (s *ServerSuite) TestTools() {
	code, body := s.do(http.MethodPost, "/tools/other", `{}`)
	s.Require().Equal(http.StatusNotFound, code)
	s.Require().Equal(true, body["is_error"])

	code, _ = s.do(http.MethodPost, "/tools/submit_questions", `{`)
	s.Require().Equal(http.StatusBadRequest, code)

	code, body = s.do(http.MethodPost, "/tools/submit_questions", `{"run_id":"r9","input":{"questions":[]}}`)
	s.Require().Equal(http.StatusOK, code)
	s.Require().Equal(true, body["is_error"])
	s.Require().Contains(body["content"], "No quiz is waiting")

	s.Require().NoError(s.st.Close())
	code, body = s.do(http.MethodPost, "/tools/submit_questions", `{"run_id":"r9","input":{}}`)
	s.Require().Equal(http.StatusOK, code)
	s.Require().Equal(true, body["is_error"])
	s.Require().Contains(body["content"], "cogtest could not store the questions")
}

func (s *ServerSuite) TestRunDone() {
	q, err := s.svc.Begin(s.ctx, "manual", true)
	s.Require().NoError(err)
	s.Require().NoError(s.st.SetRunID(s.ctx, q.ID, "r1", s.svc.Now()))
	s.loop.On("Event", mock.Anything, quiz.EventFailed, mock.Anything).Return(nil).Once()

	code, _ := s.do(http.MethodPost, "/runs/r1/done", `{"status":"done"}`)
	s.Require().Equal(http.StatusNoContent, code)
	got, err := s.st.Quiz(s.ctx, q.ID)
	s.Require().NoError(err)
	s.Require().Equal(store.StatusFailed, got.Status)

	code, _ = s.do(http.MethodPost, "/runs/r1/done", `nope`)
	s.Require().Equal(http.StatusBadRequest, code)
}

func (s *ServerSuite) TestCurrentAndAttempts() {
	code, body := s.do(http.MethodGet, "/x/quizzes/current", "")
	s.Require().Equal(http.StatusOK, code)
	s.Require().Nil(body["quiz"])
	s.Require().Equal(map[string]any{"streak": 0.0, "answered": 0.0, "correct": 0.0, "days": 30.0, "due_areas": 0.0}, body["summary"])

	qid := s.readyQuiz()
	code, body = s.do(http.MethodGet, "/x/quizzes/current", "")
	s.Require().Equal(http.StatusOK, code)
	cur := body["quiz"].(map[string]any)
	s.Require().Equal("ready", cur["status"])
	question := cur["questions"].([]any)[0].(map[string]any)
	s.Require().NotContains(question, "answer")

	cases := []struct {
		name string
		body string
		code int
	}{
		{"bad json", `[`, http.StatusBadRequest},
		{"missing choice", `{"question_id":1}`, http.StatusBadRequest},
		{"missing question", `{"chosen_index":0}`, http.StatusBadRequest},
		{"unknown question", `{"question_id":999,"chosen_index":0}`, http.StatusNotFound},
		{"bad choice", `{"question_id":` + itoa(qid) + `,"chosen_index":7}`, http.StatusBadRequest},
		{"answer", `{"question_id":` + itoa(qid) + `,"chosen_index":1}`, http.StatusOK},
		{"again", `{"question_id":` + itoa(qid) + `,"chosen_index":1}`, http.StatusConflict},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			code, body := s.do(http.MethodPost, "/x/attempts", tc.body)
			s.Require().Equal(tc.code, code, body)
			if tc.code == http.StatusOK {
				s.Require().Equal(true, body["correct"])
				s.Require().EqualValues(1, body["correct_index"])
				s.Require().Equal("see a.go", body["explanation"])
			} else {
				s.Require().NotEmpty(body["error"])
			}
		})
	}

	code, body = s.do(http.MethodGet, "/x/history", "")
	s.Require().Equal(http.StatusOK, code)
	s.Require().Len(body["quizzes"], 1)
	s.Require().Len(body["mastery"], 1)
}

func (s *ServerSuite) TestNewQuiz() {
	// The generation waits until the second request is in, so the first
	// quiz still generates when it comes.
	release := make(chan time.Time)
	s.loop.On("Channel", mock.Anything).Return(&loopapi.Channel{Platform: "slack"}, nil).WaitUntil(release).Once()
	s.loop.On("Event", mock.Anything, quiz.EventFailed, mock.Anything).Return(nil).Once()

	code, body := s.do(http.MethodPost, "/x/quizzes", "")
	s.Require().Equal(http.StatusAccepted, code)
	q := body["quiz"].(map[string]any)
	s.Require().Equal("generating", q["status"])

	// A second one while the first generates is refused.
	code, _ = s.do(http.MethodPost, "/x/quizzes", "")
	s.Require().Equal(http.StatusConflict, code)

	close(release)
	s.srv.Wait()
	got, err := s.st.LatestQuiz(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(store.StatusFailed, got.Status)
	s.Require().Contains(got.Error, "local channels")
}

func (s *ServerSuite) TestSettings() {
	code, body := s.do(http.MethodGet, "/x/settings", "")
	s.Require().Equal(http.StatusOK, code)
	s.Require().Equal("", body["model"])
	s.Require().Equal([]any{"", "sonnet", "opus", "haiku"}, body["models"])
	s.Require().Equal([]any{"", "low", "medium", "high"}, body["efforts"])

	code, body = s.do(http.MethodPut, "/x/settings", `{"model":" opus ","effort":"high"}`)
	s.Require().Equal(http.StatusOK, code)
	s.Require().Equal("opus", body["model"])
	code, body = s.do(http.MethodGet, "/x/settings", "")
	s.Require().Equal(http.StatusOK, code)
	s.Require().Equal("high", body["effort"])

	code, _ = s.do(http.MethodPut, "/x/settings", `{"model":"gpt"}`)
	s.Require().Equal(http.StatusBadRequest, code)
	code, _ = s.do(http.MethodPut, "/x/settings", `{`)
	s.Require().Equal(http.StatusBadRequest, code)
}

func (s *ServerSuite) TestStoreFailuresAre500() {
	s.Require().NoError(s.st.Close())
	cases := []struct{ method, path, body string }{
		{http.MethodGet, "/x/quizzes/current", ""},
		{http.MethodPost, "/x/quizzes", ""},
		{http.MethodPost, "/x/attempts", `{"question_id":1,"chosen_index":0}`},
		{http.MethodGet, "/x/history", ""},
		{http.MethodGet, "/x/settings", ""},
		{http.MethodPut, "/x/settings", `{}`},
		{http.MethodPost, "/runs/r1/done", `{}`},
	}
	for _, tc := range cases {
		s.Run(tc.method+" "+tc.path, func() {
			code, body := s.do(tc.method, tc.path, tc.body)
			s.Require().Equal(http.StatusInternalServerError, code)
			s.Require().NotEmpty(body["error"])
		})
	}
}

func (s *ServerSuite) TestBackgroundErrorsAreLogged() {
	s.srv.background("x", func(context.Context) error { return errors.New("boom") })
	s.srv.Wait()
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

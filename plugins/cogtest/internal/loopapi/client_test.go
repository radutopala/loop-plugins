package loopapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type ClientSuite struct {
	suite.Suite
	ctx     context.Context
	srv     *httptest.Server
	client  *HTTPClient
	handler http.HandlerFunc
	last    *http.Request
	body    []byte
}

func TestClientSuite(t *testing.T) {
	suite.Run(t, new(ClientSuite))
}

func (s *ClientSuite) SetupTest() {
	s.ctx = context.Background()
	s.handler = nil
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.last = r
		s.body, _ = io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "bad token", http.StatusUnauthorized)
			return
		}
		s.handler(w, r)
	}))
	s.T().Cleanup(s.srv.Close)
	tokenFile := filepath.Join(s.T().TempDir(), "token")
	s.Require().NoError(os.WriteFile(tokenFile, []byte("tok\n"), 0o600))
	s.client = New(s.srv.URL+"/", tokenFile)
}

func (s *ClientSuite) reply(status int, body string) {
	s.handler = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func (s *ClientSuite) TestReads() {
	cases := []struct {
		name     string
		body     string
		call     func() (any, error)
		want     any
		wantPath string
	}{
		{
			name:     "channel",
			body:     `{"id":"c1","name":"proj","dir_path":"/p","platform":"local","model":"opus","effort":"high"}`,
			call:     func() (any, error) { return s.client.Channel(s.ctx) },
			want:     &Channel{ID: "c1", Name: "proj", DirPath: "/p", Platform: "local", Model: "opus", Effort: "high"},
			wantPath: "/api/plugin/v1/channel",
		},
		{
			name:     "file with range",
			body:     `{"path":"a.go","content":"x","start":2,"end":3,"total_lines":9}`,
			call:     func() (any, error) { return s.client.File(s.ctx, "a.go", 2, 3) },
			want:     &File{Path: "a.go", Content: "x", Start: 2, End: 3, TotalLines: 9},
			wantPath: "/api/plugin/v1/channel/files?end=3&path=a.go&start=2",
		},
		{
			name:     "whole file",
			body:     `{"path":"a b.go","content":"x","start":1,"end":1,"total_lines":1}`,
			call:     func() (any, error) { return s.client.File(s.ctx, "a b.go", 0, 0) },
			want:     &File{Path: "a b.go", Content: "x", Start: 1, End: 1, TotalLines: 1},
			wantPath: "/api/plugin/v1/channel/files?path=a+b.go",
		},
		{
			name:     "tree",
			body:     `{"files":["a.go","b/c.go"]}`,
			call:     func() (any, error) { return s.client.Tree(s.ctx, "") },
			want:     []string{"a.go", "b/c.go"},
			wantPath: "/api/plugin/v1/channel/tree",
		},
		{
			name:     "tree with glob",
			body:     `{"files":["a.go"]}`,
			call:     func() (any, error) { return s.client.Tree(s.ctx, "*.go") },
			want:     []string{"a.go"},
			wantPath: "/api/plugin/v1/channel/tree?glob=%2A.go",
		},
		{
			name:     "git recent",
			body:     `{"commits":[{"sha":"s","subject":"x","author":"a","time":"t"}],"files":[{"path":"a.go","commits":3}]}`,
			call:     func() (any, error) { return s.client.GitRecent(s.ctx, 7) },
			want:     &GitRecent{Commits: []Commit{{SHA: "s", Subject: "x", Author: "a", Time: "t"}}, Files: []FileChurn{{Path: "a.go", Commits: 3}}},
			wantPath: "/api/plugin/v1/channel/git/recent?days=7",
		},
		{
			name:     "git recent default days",
			body:     `{"commits":[],"files":[]}`,
			call:     func() (any, error) { return s.client.GitRecent(s.ctx, 0) },
			want:     &GitRecent{Commits: []Commit{}, Files: []FileChurn{}},
			wantPath: "/api/plugin/v1/channel/git/recent",
		},
		{
			name:     "hotspots",
			body:     `{"functions":[{"path":"a.go","name":"F","start_line":4,"cyclomatic":9,"score":0.5}]}`,
			call:     func() (any, error) { return s.client.Hotspots(s.ctx, 5) },
			want:     []Hotspot{{Path: "a.go", Name: "F", StartLine: 4, Cyclomatic: 9, Score: 0.5}},
			wantPath: "/api/plugin/v1/channel/quality/hotspots?limit=5",
		},
		{
			name:     "hotspots default limit",
			body:     `{"functions":[]}`,
			call:     func() (any, error) { return s.client.Hotspots(s.ctx, 0) },
			want:     []Hotspot{},
			wantPath: "/api/plugin/v1/channel/quality/hotspots",
		},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.reply(http.StatusOK, tc.body)
			got, err := tc.call()
			s.Require().NoError(err)
			s.Require().Equal(tc.want, got)
			s.Require().Equal(http.MethodGet, s.last.Method)
			s.Require().Equal(tc.wantPath, s.last.URL.RequestURI())
		})
	}
}

func (s *ClientSuite) TestWrites() {
	cases := []struct {
		name     string
		status   int
		body     string
		call     func() (any, error)
		want     any
		wantPath string
		wantBody string
	}{
		{
			name:   "start run",
			status: http.StatusAccepted,
			body:   `{"run_id":"r1","thread_id":"t1"}`,
			call: func() (any, error) {
				return s.client.StartRun(s.ctx, RunRequest{Agent: "generate", Prompt: "p", Model: "opus"})
			},
			want:     &Run{RunID: "r1", ThreadID: "t1"},
			wantPath: "/api/plugin/v1/runs",
			wantBody: `{"agent":"generate","prompt":"p","model":"opus"}`,
		},
		{
			name:     "event",
			status:   http.StatusNoContent,
			call:     func() (any, error) { return nil, s.client.Event(s.ctx, "quiz_ready", map[string]int{"quiz_id": 3}) },
			wantPath: "/api/plugin/v1/events",
			wantBody: `{"type":"quiz_ready","data":{"quiz_id":3}}`,
		},
		{
			name:     "notify",
			status:   http.StatusNoContent,
			call:     func() (any, error) { return nil, s.client.Notify(s.ctx, "T", "B") },
			wantPath: "/api/plugin/v1/notify",
			wantBody: `{"body":"B","title":"T"}`,
		},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.reply(tc.status, tc.body)
			got, err := tc.call()
			s.Require().NoError(err)
			if tc.want != nil {
				s.Require().Equal(tc.want, got)
			}
			s.Require().Equal(http.MethodPost, s.last.Method)
			s.Require().Equal(tc.wantPath, s.last.URL.Path)
			s.Require().Equal("application/json", s.last.Header.Get("Content-Type"))
			s.Require().JSONEq(tc.wantBody, string(s.body))
		})
	}
}

func (s *ClientSuite) TestErrors() {
	s.Run("status error", func() {
		s.reply(http.StatusForbidden, "capability not approved\n")
		_, err := s.client.Hotspots(s.ctx, 1)
		var se *StatusError
		s.Require().ErrorAs(err, &se)
		s.Require().Equal(http.StatusForbidden, se.Status)
		s.Require().Equal("capability not approved", se.Body)
		s.Require().True(HasStatus(err, http.StatusNotFound, http.StatusForbidden))
		s.Require().False(HasStatus(err, http.StatusNotFound))
		s.Require().False(HasStatus(errors.New("x"), http.StatusForbidden))
		s.Require().Equal("GET /channel/quality/hotspots: Loop answered 403: capability not approved", err.Error())
	})
	s.Run("status error without body", func() {
		s.reply(http.StatusNotFound, "")
		_, err := s.client.Channel(s.ctx)
		s.Require().EqualError(err, "GET /channel: Loop answered 404")
	})
	s.Run("bad json", func() {
		s.reply(http.StatusOK, `{`)
		_, err := s.client.Channel(s.ctx)
		s.Require().ErrorContains(err, "decoding GET /channel")
	})
	s.Run("run without id", func() {
		s.reply(http.StatusAccepted, `{}`)
		_, err := s.client.StartRun(s.ctx, RunRequest{})
		s.Require().ErrorContains(err, "without a run_id")
	})
	s.Run("each call surfaces errors", func() {
		s.reply(http.StatusInternalServerError, "x")
		calls := []func() error{
			func() error { _, err := s.client.File(s.ctx, "a", 0, 0); return err },
			func() error { _, err := s.client.Tree(s.ctx, ""); return err },
			func() error { _, err := s.client.GitRecent(s.ctx, 0); return err },
			func() error { _, err := s.client.StartRun(s.ctx, RunRequest{}); return err },
		}
		for _, c := range calls {
			s.Require().True(HasStatus(c(), http.StatusInternalServerError))
		}
	})
	s.Run("missing token file", func() {
		c := New(s.srv.URL, filepath.Join(s.T().TempDir(), "none"))
		_, err := c.Channel(s.ctx)
		s.Require().ErrorContains(err, "reading the plugin token")
	})
	s.Run("unencodable body", func() {
		err := s.client.Event(s.ctx, "x", make(chan int))
		s.Require().ErrorContains(err, "encoding POST /events")
	})
	s.Run("bad url", func() {
		c := New("http://bad\x7f", s.client.TokenFile)
		_, err := c.Channel(s.ctx)
		s.Require().ErrorContains(err, "building GET /channel")
	})
	s.Run("transport error", func() {
		c := New("http://127.0.0.1:1", s.client.TokenFile)
		_, err := c.Channel(s.ctx)
		s.Require().ErrorContains(err, "GET /channel")
	})
	s.Run("timeout", func() {
		s.handler = func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}
		c := New(s.srv.URL, s.client.TokenFile)
		c.Timeout = 20 * time.Millisecond
		_, err := c.Channel(s.ctx)
		s.Require().ErrorIs(err, context.DeadlineExceeded)
	})
}

func (s *ClientSuite) TestRunRequestOmitsEmptyOverrides() {
	b, err := json.Marshal(RunRequest{Agent: "a", Prompt: "p"})
	s.Require().NoError(err)
	s.Require().JSONEq(`{"agent":"a","prompt":"p"}`, string(b))
}

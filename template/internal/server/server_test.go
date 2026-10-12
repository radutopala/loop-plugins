package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/radutopala/loop/pluginsdk"

	"github.com/radutopala/loop-plugins/template/internal/store"
)

type loopMock struct {
	mock.Mock
}

func (m *loopMock) Channel(ctx context.Context) (*pluginsdk.Channel, error) {
	args := m.Called(ctx)
	ch, _ := args.Get(0).(*pluginsdk.Channel)
	return ch, args.Error(1)
}

func (m *loopMock) File(ctx context.Context, path string, start, end int) (*pluginsdk.File, error) {
	args := m.Called(ctx, path, start, end)
	f, _ := args.Get(0).(*pluginsdk.File)
	return f, args.Error(1)
}

func (m *loopMock) Tree(ctx context.Context, glob string) (*pluginsdk.Tree, error) {
	args := m.Called(ctx, glob)
	t, _ := args.Get(0).(*pluginsdk.Tree)
	return t, args.Error(1)
}

func (m *loopMock) Notify(ctx context.Context, title, body string) error {
	return m.Called(ctx, title, body).Error(0)
}

var at = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

type ServerSuite struct {
	suite.Suite
	loop  *loopMock
	store *store.Store
	srv   *Server
	dir   string
	ctx   context.Context
}

func TestServerSuite(t *testing.T) {
	suite.Run(t, new(ServerSuite))
}

func (s *ServerSuite) SetupTest() {
	s.ctx = context.Background()
	s.loop = new(loopMock)
	s.dir = s.T().TempDir()
	var err error
	s.store, err = store.Open(filepath.Join(s.dir, "state.json"))
	s.Require().NoError(err)
	s.srv = New(s.loop, s.store, "sekret", "1.2.3", slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.srv.now = func() time.Time { return at }
}

func (s *ServerSuite) TearDownTest() {
	s.loop.AssertExpectations(s.T())
}

func (s *ServerSuite) channel(glob any) *pluginsdk.Channel {
	return &pluginsdk.Channel{ID: "c1", Settings: map[string]any{"glob": glob}}
}

func (s *ServerSuite) TestCountAction() {
	cases := []struct {
		name    string
		setup   func()
		wantErr string
	}{
		{
			name: "counts and notifies",
			setup: func() {
				s.loop.On("Channel", mock.Anything).Return(s.channel("*.go"), nil)
				s.loop.On("Tree", mock.Anything, "*.go").Return(&pluginsdk.Tree{Files: []string{"a.go", "b.go"}}, nil)
				s.loop.On("Notify", mock.Anything, "Files counted", "2 files").Return(nil)
			},
		},
		{
			name:    "channel fails",
			setup:   func() { s.loop.On("Channel", mock.Anything).Return(nil, errors.New("down")) },
			wantErr: "reading the channel: down",
		},
		{
			name: "tree fails",
			setup: func() {
				s.loop.On("Channel", mock.Anything).Return(s.channel(nil), nil)
				s.loop.On("Tree", mock.Anything, "").Return(nil, &pluginsdk.StatusError{Status: http.StatusForbidden})
			},
			wantErr: "listing the files",
		},
		{
			name: "notify fails",
			setup: func() {
				s.loop.On("Channel", mock.Anything).Return(s.channel(""), nil)
				s.loop.On("Tree", mock.Anything, "").Return(&pluginsdk.Tree{}, nil)
				s.loop.On("Notify", mock.Anything, "Files counted", "0 files").Return(errors.New("429"))
			},
			wantErr: "429",
		},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			tc.setup()
			err := s.srv.countAction(s.ctx, pluginsdk.ActionRequest{Trigger: pluginsdk.TriggerManual})
			if tc.wantErr != "" {
				s.Require().ErrorContains(err, tc.wantErr)
			} else {
				s.Require().NoError(err)
			}
			s.loop.AssertExpectations(s.T())
		})
	}
}

func (s *ServerSuite) TestCountLines() {
	cases := []struct {
		name    string
		input   string
		setup   func()
		want    string
		wantErr string
	}{
		{
			name:  "counts",
			input: `{"path":"main.go"}`,
			setup: func() {
				s.loop.On("File", mock.Anything, "main.go", 0, 0).Return(&pluginsdk.File{Path: "main.go", TotalLines: 42}, nil)
			},
			want: "main.go has 42 lines",
		},
		{name: "bad input", input: `[]`, setup: func() {}, wantErr: "decoding the input"},
		{name: "no path", input: `{}`, setup: func() {}, wantErr: "path is required"},
		{
			name:  "missing file",
			input: `{"path":"gone.go"}`,
			setup: func() {
				s.loop.On("File", mock.Anything, "gone.go", 0, 0).Return(nil, &pluginsdk.StatusError{Status: http.StatusNotFound})
			},
			wantErr: "gone.go doesn't exist",
		},
		{
			name:  "read fails",
			input: `{"path":"big.go"}`,
			setup: func() {
				s.loop.On("File", mock.Anything, "big.go", 0, 0).Return(nil, &pluginsdk.StatusError{Status: http.StatusRequestEntityTooLarge})
			},
			wantErr: "reading big.go",
		},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			tc.setup()
			res, err := s.srv.countLines(s.ctx, pluginsdk.ToolRequest{Input: json.RawMessage(tc.input)})
			if tc.wantErr != "" {
				s.Require().ErrorContains(err, tc.wantErr)
			} else {
				s.Require().NoError(err)
				s.Require().Equal(tc.want, res.Content)
			}
			s.loop.AssertExpectations(s.T())
		})
	}
}

func (s *ServerSuite) serve(method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.srv.X.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func (s *ServerSuite) TestX() {
	cases := []struct {
		name       string
		method     string
		path       string
		setup      func()
		wantStatus int
		wantBody   string
	}{
		{
			name: "stats before a count", method: http.MethodGet, path: "/stats", setup: func() {},
			wantStatus: http.StatusOK, wantBody: `{"runs":0}`,
		},
		{
			name: "count", method: http.MethodPost, path: "/count",
			setup: func() {
				s.loop.On("Channel", mock.Anything).Return(s.channel("*.md"), nil)
				s.loop.On("Tree", mock.Anything, "*.md").Return(&pluginsdk.Tree{Files: []string{"README.md"}, Truncated: true}, nil)
			},
			wantStatus: http.StatusOK,
			wantBody:   `{"last":{"glob":"*.md","files":1,"truncated":true,"at":"2026-01-02T03:04:05Z"},"runs":1}`,
		},
		{
			name: "count fails", method: http.MethodPost, path: "/count",
			setup:      func() { s.loop.On("Channel", mock.Anything).Return(nil, errors.New("down")) },
			wantStatus: http.StatusInternalServerError, wantBody: `{"error":"reading the channel: down"}`,
		},
		{
			name: "stats fail", method: http.MethodGet, path: "/stats",
			setup: func() {
				s.Require().NoError(os.Mkdir(filepath.Join(s.dir, "state.json"), 0o700))
			},
			wantStatus: http.StatusInternalServerError,
		},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			tc.setup()
			rec := s.serve(tc.method, tc.path)
			s.Require().Equal(tc.wantStatus, rec.Code, rec.Body.String())
			if tc.wantBody != "" {
				s.Require().JSONEq(tc.wantBody, rec.Body.String())
			}
			s.loop.AssertExpectations(s.T())
		})
	}
}

package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

type RunSuite struct {
	suite.Suite
	dir    string
	env    map[string]string
	logger *slog.Logger
}

func TestRunSuite(t *testing.T) {
	suite.Run(t, new(RunSuite))
}

func (s *RunSuite) SetupTest() {
	s.dir = s.T().TempDir()
	secret := filepath.Join(s.dir, "secret")
	s.Require().NoError(os.WriteFile(secret, []byte("sekret\n"), 0o600))
	s.env = map[string]string{
		"LOOP_PLUGIN_SECRET_FILE": secret,
		"LOOP_DATA_DIR":           s.dir,
		"LOOP_PLUGIN_PORT":        "0",
		"LOOP_API_URL":            "http://127.0.0.1:1",
		"LOOP_PLUGIN_TOKEN_FILE":  filepath.Join(s.dir, "token"),
	}
	s.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
}

func (s *RunSuite) getenv(k string) string { return s.env[k] }

func (s *RunSuite) TestServesUntilCancelled() {
	ctx, cancel := context.WithCancel(context.Background())
	addr := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, s.getenv, s.logger, func(a string) { addr <- a }) }()

	_, port, err := net.SplitHostPort(<-addr)
	s.Require().NoError(err)
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+port+"/healthz", nil)
	s.Require().NoError(err)
	req.Header.Set("Authorization", "Bearer sekret")
	resp, err := http.DefaultClient.Do(req)
	s.Require().NoError(err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	s.Require().Equal(http.StatusOK, resp.StatusCode)
	s.Require().JSONEq(`{"api":"1","version":"`+version+`"}`, string(body))

	cancel()
	s.Require().NoError(<-done)
	s.Require().FileExists(filepath.Join(s.dir, "cogtest.db"))
}

func (s *RunSuite) TestErrors() {
	cases := []struct {
		name    string
		key     string
		value   func(dir string) string
		wantErr string
	}{
		{"missing secret", "LOOP_PLUGIN_SECRET_FILE", func(d string) string { return filepath.Join(d, "none") }, "reading the secret"},
		{"bad data dir", "LOOP_DATA_DIR", func(d string) string { return filepath.Join(d, "none", "dir") }, "unable to open database file"},
		{"bad port", "LOOP_PLUGIN_PORT", func(string) string { return "x" }, "listen"},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.env[tc.key] = tc.value(s.dir)
			err := run(context.Background(), s.getenv, s.logger, nil)
			s.Require().ErrorContains(err, tc.wantErr)
		})
	}
}

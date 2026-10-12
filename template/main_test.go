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
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/radutopala/loop/pluginsdk"
	"github.com/radutopala/loop/pluginsdk/pluginsdktest"
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

// start runs the plugin until the test ends, returning its URL.
func (s *RunSuite) start() string {
	ctx, cancel := context.WithCancel(context.Background())
	addr := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, s.getenv, "plugin.json", s.logger, func(a string) { addr <- a }) }()
	s.T().Cleanup(func() {
		cancel()
		s.Require().NoError(<-done)
	})
	_, port, err := net.SplitHostPort(<-addr)
	s.Require().NoError(err)
	return "http://127.0.0.1:" + port
}

func (s *RunSuite) TestServes() {
	req, err := http.NewRequest(http.MethodGet, s.start()+"/healthz", nil)
	s.Require().NoError(err)
	req.Header.Set("Authorization", "Bearer sekret")
	resp, err := http.DefaultClient.Do(req)
	s.Require().NoError(err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	s.Require().Equal(http.StatusOK, resp.StatusCode)
	m, err := pluginsdk.ReadManifest("plugin.json")
	s.Require().NoError(err)
	s.Require().JSONEq(`{"api":"1","version":"`+m.Version+`"}`, string(body))
}

func (s *RunSuite) TestErrors() {
	cases := []struct {
		name     string
		key      string
		value    func(dir string) string
		manifest string
		wantErr  string
	}{
		{"missing secret", "LOOP_PLUGIN_SECRET_FILE", func(d string) string { return filepath.Join(d, "none") }, "plugin.json", "reading the secret"},
		{"missing manifest", "", nil, "none.json", "reading the plugin manifest"},
		{"missing data dir", "LOOP_DATA_DIR", func(d string) string { return filepath.Join(d, "none", "dir") }, "plugin.json", "opening the store"},
		{"bad port", "LOOP_PLUGIN_PORT", func(string) string { return "x" }, "plugin.json", "listen"},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.SetupTest()
			if tc.value != nil {
				s.env[tc.key] = tc.value(s.dir)
			}
			err := run(context.Background(), s.getenv, tc.manifest, s.logger, nil)
			s.Require().ErrorContains(err, tc.wantErr)
		})
	}
}

// TestConformance drives the plugin through the Loop → plugin contract
// against a fake Loop: each trigger of the count action counts the files
// and leaves a notification.
func (s *RunSuite) TestConformance() {
	m, err := pluginsdk.ReadManifest("plugin.json")
	s.Require().NoError(err)
	loop := pluginsdktest.NewFakeLoop(s.T(), pluginsdktest.Config{
		Manifest: m,
		Channel:  pluginsdk.Channel{Name: "proj", DirPath: "/src", Platform: "local"},
		Files:    map[string]string{"main.go": "package main\n\nfunc main() {}\n", "README.md": "# proj\n"},
	})
	s.env["LOOP_API_URL"], s.env["LOOP_PLUGIN_TOKEN_FILE"] = loop.URL, loop.TokenFile

	pluginsdktest.Check(s.T(), &pluginsdktest.Plugin{URL: s.start(), Secret: "sekret"}, m, pluginsdktest.CheckOptions{
		Loop: loop,
		Settle: func() {
			s.Require().Eventually(func() bool { return len(loop.Notifications()) == 3 }, 5*time.Second, 10*time.Millisecond, "each trigger of count notifies")
		},
	})
	s.Require().Equal("2 files", loop.Notifications()[0].Body)
}

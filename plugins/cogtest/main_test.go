package main

import (
	"context"
	"encoding/json"
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

	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/quiz"
	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/store"
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
	go func() { done <- run(ctx, s.getenv, "plugin.json", s.logger, func(a string) { addr <- a }) }()

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
	m, err := pluginsdk.ReadManifest("plugin.json")
	s.Require().NoError(err)
	s.Require().JSONEq(`{"api":"1","version":"`+m.Version+`"}`, string(body))

	cancel()
	s.Require().NoError(<-done)
	s.Require().FileExists(filepath.Join(s.dir, "cogtest.db"))
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
		{"bad data dir", "LOOP_DATA_DIR", func(d string) string { return filepath.Join(d, "none", "dir") }, "plugin.json", "unable to open database file"},
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

// TestConformance drives cogtest through the Loop → plugin contract against
// a fake Loop serving a local channel: the daily action starts a generation
// run, and its end is taken.
func (s *RunSuite) TestConformance() {
	m, err := pluginsdk.ReadManifest("plugin.json")
	s.Require().NoError(err)
	loop := pluginsdktest.NewFakeLoop(s.T(), pluginsdktest.Config{
		Manifest: m,
		Channel:  pluginsdk.Channel{Name: "proj", DirPath: "/src", Platform: "local"},
		Files:    map[string]string{"main.go": "package main\n\nfunc main() {}\n"},
	})
	s.env["LOOP_API_URL"], s.env["LOOP_PLUGIN_TOKEN_FILE"] = loop.URL, loop.TokenFile

	ctx, cancel := context.WithCancel(context.Background())
	addr := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, s.getenv, "plugin.json", s.logger, func(a string) { addr <- a }) }()
	_, port, err := net.SplitHostPort(<-addr)
	s.Require().NoError(err)

	pluginsdktest.Check(s.T(), &pluginsdktest.Plugin{URL: "http://127.0.0.1:" + port, Secret: "sekret"}, m, pluginsdktest.CheckOptions{
		Loop: loop,
		Settle: func() {
			s.Require().Eventually(func() bool { return len(loop.Runs()) == 1 }, 5*time.Second, 10*time.Millisecond, "the daily action starts one run")
		},
	})
	s.Require().Equal("generate", loop.Runs()[0].Agent)

	cancel()
	s.Require().NoError(<-done)
}

// TestBadgeAndSummaryCard runs a quiz against a fake Loop holding cogtest to
// its manifest: the badge shows while the quiz waits, and answering it posts
// the declared summary card and clears the badge.
func (s *RunSuite) TestBadgeAndSummaryCard() {
	ctx := context.Background()
	m, err := pluginsdk.ReadManifest("plugin.json")
	s.Require().NoError(err)
	loop := pluginsdktest.NewFakeLoop(s.T(), pluginsdktest.Config{
		Manifest: m,
		Channel:  pluginsdk.Channel{Name: "proj", DirPath: "/src", Platform: "local"},
		Files:    map[string]string{"main.go": "package main\n\nfunc main() {}\n"},
	})
	st, err := store.Open(ctx, filepath.Join(s.dir, "cogtest.db"))
	s.Require().NoError(err)
	defer st.Close()
	svc := quiz.New(st, loop.Client(), s.logger)
	svc.Count = 1

	q, err := svc.Begin(ctx, "manual", true)
	s.Require().NoError(err)
	s.Require().NoError(st.SetRunID(ctx, q.ID, "r1", svc.Now()))
	input, err := json.Marshal(map[string]any{"questions": []map[string]any{{
		"question": "What does main do?", "choices": []string{"Nothing", "Serves HTTP", "Panics"}, "correct_index": 0,
		"explanation": "See `main.go`: its body is empty.", "anchors": []map[string]any{{"path": "main.go", "start": 1, "end": 3}},
	}}})
	s.Require().NoError(err)
	res, err := svc.Submit(ctx, "r1", input)
	s.Require().NoError(err)
	s.Require().False(res.IsError, res.Content)
	s.Require().Equal(&pluginsdktest.Badge{Text: "quiz", Title: "Today's quiz is ready"}, loop.Badge())

	qs, err := st.Questions(ctx, q.ID)
	s.Require().NoError(err)
	_, err = svc.Answer(ctx, qs[0].ID, 0)
	s.Require().NoError(err)
	s.Require().Nil(loop.Badge())
	cards := loop.Cards()
	s.Require().Len(cards, 1)
	s.Require().Equal("summary", cards[0].Card)
	s.Require().Equal("Quiz results: 1/1", cards[0].Title)
	var data quiz.SummaryCard
	s.Require().NoError(json.Unmarshal(cards[0].Data, &data))
	s.Require().Equal(quiz.Score{Total: 1, Answered: 1, Correct: 1}, data.Score)
	s.Require().Equal([]quiz.CardQuestion{{Question: "What does main do?", Area: ".", Path: "main.go", Line: 1, Correct: true}}, data.Questions)
	s.Require().Equal([]quiz.BoxMove{{Area: ".", From: 0, To: 2}}, data.Moves)
	s.Require().Equal(1, data.Streak)
}

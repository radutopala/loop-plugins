package quiz

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/radutopala/loop/pluginsdk"

	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/loopmock"
	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/store"
)

func mustDate(d string) time.Time {
	t, err := time.Parse(dateLayout, d)
	if err != nil {
		panic(err)
	}
	return t.Add(9 * time.Hour)
}

func nextMasteryFor(box int, found, correct bool, now time.Time) store.Mastery {
	return nextMastery(store.Mastery{Area: "a", Box: box}, found, correct, now)
}

type ServiceSuite struct {
	suite.Suite
	ctx  context.Context
	st   *store.Store
	loop *loopmock.Client
	svc  *Service
	now  time.Time
	// dbPath is the store's file.
	dbPath string
}

func TestServiceSuite(t *testing.T) {
	suite.Run(t, new(ServiceSuite))
}

func (s *ServiceSuite) SetupTest() {
	s.ctx = context.Background()
	s.dbPath = filepath.Join(s.T().TempDir(), "cogtest.db")
	st, err := store.Open(s.ctx, s.dbPath)
	s.Require().NoError(err)
	s.st = st
	s.loop = &loopmock.Client{}
	s.now = mustDate("2026-10-10")
	s.svc = New(st, s.loop, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.svc.Now = func() time.Time { return s.now }
	s.svc.Rand = rand.New(rand.NewPCG(1, 2))
	s.svc.Count = 2
}

func (s *ServiceSuite) TearDownTest() {
	s.loop.AssertExpectations(s.T())
	s.Require().NoError(s.st.Close())
}

func local() *pluginsdk.Channel {
	return &pluginsdk.Channel{ID: "c1", Name: "proj", DirPath: "/home/u/proj", Platform: "local"}
}

// generating creates a generating quiz for run runID.
func (s *ServiceSuite) generating(runID string) *store.Quiz {
	q, err := s.svc.Begin(s.ctx, "manual", true)
	s.Require().NoError(err)
	if runID != "" {
		s.Require().NoError(s.st.SetRunID(s.ctx, q.ID, runID, s.now))
	}
	return q
}

func (s *ServiceSuite) expectEvent(typ string) *mock.Call {
	return s.loop.On("Event", mock.Anything, typ, mock.Anything).Return(nil).Once()
}

// --- Begin ---

func (s *ServiceSuite) TestBegin() {
	s.Require().NoError(s.st.SaveSettings(s.ctx, store.Settings{Model: "opus", Effort: "low"}))
	q, err := s.svc.Begin(s.ctx, "schedule", false)
	s.Require().NoError(err)
	s.Require().Equal(store.StatusGenerating, q.Status)
	s.Require().Equal("2026-10-10", q.Date)
	s.Require().Equal("schedule", q.Trigger)
	s.Require().Equal("opus", q.Model)
	s.Require().Equal("low", q.Effort)
	s.Require().Equal(2, q.QuestionCount)

	_, err = s.svc.Begin(s.ctx, "manual", true)
	s.Require().ErrorIs(err, ErrGenerating)
}

func (s *ServiceSuite) TestBeginSkipsWhenTodayIsReady() {
	q := s.generating("r1")
	s.Require().NoError(s.st.SaveQuestions(s.ctx, q.ID, nil, s.now))

	_, err := s.svc.Begin(s.ctx, "schedule", false)
	s.Require().ErrorIs(err, ErrDone)

	// "New quiz" forces another one.
	q2, err := s.svc.Begin(s.ctx, "manual", true)
	s.Require().NoError(err)
	s.Require().NotEqual(q.ID, q2.ID)
}

func (s *ServiceSuite) TestBeginRetriesAfterFailureAndExpiresStale() {
	q := s.generating("")
	_, err := s.st.Transition(s.ctx, q.ID, store.StatusGenerating, store.StatusFailed, "x", s.now)
	s.Require().NoError(err)
	_, err = s.svc.Begin(s.ctx, "schedule", false)
	s.Require().NoError(err)

	s.now = s.now.Add(time.Hour)
	q3, err := s.svc.Begin(s.ctx, "schedule", false)
	s.Require().NoError(err)
	h, err := s.st.History(s.ctx, 5)
	s.Require().NoError(err)
	s.Require().Equal(q3.ID, h[0].ID)
	s.Require().Equal(store.StatusExpired, h[1].Status)
}

// --- Daily / Launch ---

func (s *ServiceSuite) TestDailyRefusesNonLocalChannels() {
	s.loop.On("Channel", mock.Anything).Return(&pluginsdk.Channel{Platform: "slack"}, nil).Once()
	s.Require().ErrorIs(s.svc.Daily(s.ctx, "schedule"), ErrNotLocal)
	_, err := s.st.LatestQuiz(s.ctx)
	s.Require().ErrorIs(err, store.ErrNotFound)
}

func (s *ServiceSuite) TestDailyChannelError() {
	s.loop.On("Channel", mock.Anything).Return(nil, errors.New("down")).Once()
	s.Require().ErrorContains(s.svc.Daily(s.ctx, "schedule"), "down")
}

func (s *ServiceSuite) TestDailySkipsWhenGenerating() {
	s.generating("r1")
	s.loop.On("Channel", mock.Anything).Return(local(), nil).Once()
	s.Require().ErrorIs(s.svc.Daily(s.ctx, "schedule"), ErrGenerating)
}

func (s *ServiceSuite) TestDailyStartsARun() {
	s.Require().NoError(s.st.SaveSettings(s.ctx, store.Settings{Model: "haiku"}))
	// A previous quiz gives a question to avoid and a due area.
	prev := s.generating("old")
	s.Require().NoError(s.st.SaveQuestions(s.ctx, prev.ID, []store.Question{{
		Question: "What does Old do?", Choices: []string{"a", "b", "c"}, Anchors: []store.Anchor{{Path: "due/x.go", Start: 1, End: 1}},
		Fingerprint: "fp", Area: "due",
	}}, s.now))
	qs, err := s.st.Questions(s.ctx, prev.ID)
	s.Require().NoError(err)
	_, err = s.svc.Answer(s.ctx, qs[0].ID, 1)
	s.Require().NoError(err)
	s.now = s.now.AddDate(0, 0, 1)

	s.loop.On("Channel", mock.Anything).Return(local(), nil).Twice()
	s.loop.On("Tree", mock.Anything, "").Return(&pluginsdk.Tree{Files: []string{"due/x.go", "hot/h.go", "churn/c.go", "README.md", "rand/r.go"}}, nil).Once()
	s.loop.On("GitRecent", mock.Anything, 14).Return(&pluginsdk.GitRecent{Files: []pluginsdk.FileChurn{
		{Path: "go.sum", Commits: 9}, {Path: "churn/c.go", Commits: 5},
	}}, nil).Once()
	s.loop.On("Hotspots", mock.Anything, 20).Return([]pluginsdk.Hotspot{{Path: "/home/u/proj/hot/h.go", Name: "Big", StartLine: 7}}, nil).Once()
	var prompt string
	s.loop.On("StartRun", mock.Anything, mock.MatchedBy(func(r pluginsdk.RunRequest) bool {
		prompt = r.Prompt
		return r.Agent == AgentID && r.Model == "haiku" && r.Effort == ""
	})).Return(&pluginsdk.Run{RunID: "run-9", ThreadID: "t"}, nil).Once()
	s.expectEvent(EventStarted)

	s.Require().NoError(s.svc.Daily(s.ctx, "schedule"))

	q, err := s.st.QuizByRunID(s.ctx, "run-9")
	s.Require().NoError(err)
	s.Require().Equal(store.StatusGenerating, q.Status)
	// The missed question comes back, so the agent writes one fewer.
	s.Require().Equal([]int64{qs[0].ID}, q.Reasks)
	s.Require().Equal(s.svc.Count-1, q.QuestionCount)
	s.Require().Equal([]string{"due/x.go", "churn/c.go", "hot/h.go"}, q.Focus)
	s.Require().Contains(prompt, "- `due/x.go`: its area due is due for review")
	s.Require().Contains(prompt, "- `churn/c.go`: changed in 5 recent commits")
	s.Require().Contains(prompt, "- `hot/h.go`: complex function Big at line 7")
	s.Require().NotContains(prompt, "rand/r.go")
	s.Require().Contains(prompt, "- What does Old do?")
	s.Require().NotContains(prompt, "README.md")
}

func (s *ServiceSuite) TestQuestionCount() {
	tests := []struct {
		name     string
		settings map[string]any
		want     int
	}{
		{name: "unset", want: 5},
		{name: "set", settings: map[string]any{"question_count": 8.0}, want: 8},
		{name: "not a number", settings: map[string]any{"question_count": "8"}, want: 5},
		{name: "fraction", settings: map[string]any{"question_count": 2.5}, want: 5},
		{name: "zero", settings: map[string]any{"question_count": 0.0}, want: 5},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			s.Require().Equal(tt.want, questionCount(&pluginsdk.Channel{Settings: tt.settings}, 5))
		})
	}
}

func (s *ServiceSuite) TestLaunchTakesTheQuestionCountSetting() {
	q, err := s.svc.Begin(s.ctx, "manual", true)
	s.Require().NoError(err)
	ch := local()
	ch.Settings = map[string]any{"question_count": 7.0}
	s.loop.On("Channel", mock.Anything).Return(ch, nil).Once()
	s.loop.On("Tree", mock.Anything, "").Return(&pluginsdk.Tree{Files: []string{"a.go"}}, nil).Once()
	s.loop.On("GitRecent", mock.Anything, 14).Return(&pluginsdk.GitRecent{}, nil).Once()
	s.loop.On("Hotspots", mock.Anything, 20).Return(nil, nil).Once()
	var prompt string
	s.loop.On("StartRun", mock.Anything, mock.MatchedBy(func(r pluginsdk.RunRequest) bool {
		prompt = r.Prompt
		return true
	})).Return(&pluginsdk.Run{RunID: "run-7"}, nil).Once()
	s.expectEvent(EventStarted)

	s.Require().NoError(s.svc.Launch(s.ctx, q))
	got, err := s.st.Quiz(s.ctx, q.ID)
	s.Require().NoError(err)
	s.Require().Equal(7, got.QuestionCount)
	s.Require().Contains(prompt, "## Write 7 questions")
}

func (s *ServiceSuite) TestLaunchFailures() {
	cases := []struct {
		name    string
		setup   func()
		wantErr string
	}{
		{
			name:    "channel error",
			setup:   func() { s.loop.On("Channel", mock.Anything).Return(nil, errors.New("down")).Once() },
			wantErr: "reading the channel: down",
		},
		{
			name: "not local",
			setup: func() {
				s.loop.On("Channel", mock.Anything).Return(&pluginsdk.Channel{Platform: "discord"}, nil).Once()
			},
			wantErr: `cogtest only runs in local channels (this one is "discord")`,
		},
		{
			name: "tree error",
			setup: func() {
				s.loop.On("Channel", mock.Anything).Return(local(), nil).Once()
				s.loop.On("Tree", mock.Anything, "").Return(nil, &pluginsdk.StatusError{Status: 403}).Once()
			},
			wantErr: "listing the channel's files",
		},
		{
			name: "no source files",
			setup: func() {
				s.loop.On("Channel", mock.Anything).Return(local(), nil).Once()
				s.loop.On("Tree", mock.Anything, "").Return(&pluginsdk.Tree{Files: []string{"README.md"}}, nil).Once()
			},
			wantErr: "no source files",
		},
		{
			name: "run refused",
			setup: func() {
				s.loop.On("Channel", mock.Anything).Return(local(), nil).Once()
				s.loop.On("Tree", mock.Anything, "").Return(&pluginsdk.Tree{Files: []string{"a.go"}}, nil).Once()
				s.loop.On("GitRecent", mock.Anything, 14).Return(nil, &pluginsdk.StatusError{Status: 409}).Once()
				s.loop.On("Hotspots", mock.Anything, 20).Return(nil, &pluginsdk.StatusError{Status: 404}).Once()
				s.loop.On("StartRun", mock.Anything, mock.Anything).Return(nil, errors.New("no agent")).Once()
			},
			wantErr: "starting the generation run: no agent",
		},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			q := s.generating("")
			tc.setup()
			s.loop.On("Event", mock.Anything, EventFailed, mock.MatchedBy(func(d map[string]any) bool {
				return d["quiz_id"] == q.ID && strings.Contains(d["error"].(string), tc.wantErr)
			})).Return(errors.New("events down is only logged")).Once()

			s.Require().ErrorContains(s.svc.Launch(s.ctx, q), tc.wantErr)
			got, err := s.st.Quiz(s.ctx, q.ID)
			s.Require().NoError(err)
			s.Require().Equal(store.StatusFailed, got.Status)
			s.Require().Contains(got.Error, tc.wantErr)
		})
	}
}

func (s *ServiceSuite) TestPlanFillsAtRandomAndCaps() {
	s.loop.On("Tree", mock.Anything, "").Return(&pluginsdk.Tree{Files: []string{"a.go", "b.go", "c.go", "d.go", "e_test.go"}}, nil)
	s.loop.On("GitRecent", mock.Anything, 14).Return(&pluginsdk.GitRecent{Files: []pluginsdk.FileChurn{
		{Path: "a.go", Commits: 1}, {Path: "b.go", Commits: 4}, {Path: "c.go", Commits: 3}, {Path: "gone.go", Commits: 9},
	}}, nil)
	s.loop.On("Hotspots", mock.Anything, 20).Return([]pluginsdk.Hotspot{{Path: "b.go"}, {Path: "d.go", Name: "D"}}, nil)

	got, err := s.svc.plan(s.ctx, "", "2026-10-10", 3)
	s.Require().NoError(err)
	s.Require().Equal([]FocusItem{
		{Path: "b.go", Reason: "changed in 4 recent commits"},
		{Path: "c.go", Reason: "changed in 3 recent commits"},
		{Path: "d.go", Reason: "complex function D at line 0"},
	}, got)

	got, err = s.svc.plan(s.ctx, "", "2026-10-10", 10)
	s.Require().NoError(err)
	s.Require().Equal([]string{"b.go", "c.go", "a.go", "d.go"}, paths(got))
}

func paths(items []FocusItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Path
	}
	return out
}

// --- RunDone ---

func (s *ServiceSuite) TestRunDone() {
	cases := []struct {
		name, status, errMsg, want string
	}{
		{"no submission", "done", "", noSubmission},
		{"failed with error", "failed", "container died", "container died"},
		{"failed without error", "failed", " ", "the generation run failed"},
	}
	for i, tc := range cases {
		s.Run(tc.name, func() {
			runID := fmt.Sprintf("run-%d", i)
			q := s.generating(runID)
			s.expectEvent(EventFailed)
			s.Require().NoError(s.svc.RunDone(s.ctx, runID, tc.status, tc.errMsg))
			got, err := s.st.Quiz(s.ctx, q.ID)
			s.Require().NoError(err)
			s.Require().Equal(store.StatusFailed, got.Status)
			s.Require().Equal(tc.want, got.Error)
		})
	}
}

func (s *ServiceSuite) TestRunDoneIgnoresUnknownAndFinishedRuns() {
	s.Require().NoError(s.svc.RunDone(s.ctx, "nope", "done", ""))
	q := s.generating("r1")
	s.Require().NoError(s.st.SaveQuestions(s.ctx, q.ID, nil, s.now))
	s.Require().NoError(s.svc.RunDone(s.ctx, "r1", "done", ""))
	got, err := s.st.Quiz(s.ctx, q.ID)
	s.Require().NoError(err)
	s.Require().Equal(store.StatusReady, got.Status)
}

// --- Submit ---

type item = map[string]any

func good(q string, path string) item {
	return item{
		"question": q, "choices": []string{"It retries", "It panics", "It logs and drops"}, "correct_index": 0,
		"explanation": "See `" + path + "`: the loop retries.", "anchors": []item{{"path": path, "start": 2, "end": 4}},
	}
}

func input(items ...item) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"questions": items})
	return b
}

func (s *ServiceSuite) expectFile(path string, start, end, total int) {
	s.loop.On("File", mock.Anything, path, start, end).Return(&pluginsdk.File{Path: path, Content: "code", Start: start, End: min(end, total), TotalLines: total}, nil)
}

func (s *ServiceSuite) TestSubmitStoresAValidQuiz() {
	q := s.generating("r1")
	s.expectFile("pkg/a.go", 2, 4, 40)
	s.expectFile("b.go", 2, 4, 40)
	s.expectEvent(EventReady)
	s.loop.On("Notify", mock.Anything, "Cognitive test", "Today's quiz is ready").Return(errors.New("only logged")).Once()

	res, err := s.svc.Submit(s.ctx, "r1", input(good("What does A do?", "pkg/a.go"), good("What does B do?", "./b.go")))
	s.Require().NoError(err)
	s.Require().False(res.IsError, res.Content)
	s.Require().Contains(res.Content, "Saved 2 questions")

	got, err := s.st.Quiz(s.ctx, q.ID)
	s.Require().NoError(err)
	s.Require().Equal(store.StatusReady, got.Status)
	qs, err := s.st.Questions(s.ctx, q.ID)
	s.Require().NoError(err)
	s.Require().Len(qs, 2)
	s.Require().Equal("pkg", qs[0].Area)
	s.Require().Equal(".", qs[1].Area)
	s.Require().Equal("b.go", qs[1].Anchors[0].Path)
	s.Require().Equal(hashSnippet("code"), qs[0].Anchors[0].Hash)
	s.Require().Equal(Fingerprint("What does A do?"), qs[0].Fingerprint)

	// Late calls are told to stop.
	res, err = s.svc.Submit(s.ctx, "r1", input(good("x", "a.go")))
	s.Require().NoError(err)
	s.Require().True(res.IsError)
	s.Require().Contains(res.Content, "no longer accepts questions (it is ready)")
}

func (s *ServiceSuite) TestSubmitClaimsAnUnclaimedQuiz() {
	q := s.generating("")
	s.expectFile("a.go", 2, 4, 40)
	s.expectFile("b.go", 2, 4, 40)
	s.expectEvent(EventReady)
	s.loop.On("Notify", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	res, err := s.svc.Submit(s.ctx, "early-run", input(good("A?", "a.go"), good("B?", "b.go")))
	s.Require().NoError(err)
	s.Require().False(res.IsError, res.Content)
	got, err := s.st.QuizByRunID(s.ctx, "early-run")
	s.Require().NoError(err)
	s.Require().Equal(q.ID, got.ID)
}

func (s *ServiceSuite) TestSubmitWithoutAQuiz() {
	for _, runID := range []string{"", "r1"} {
		res, err := s.svc.Submit(s.ctx, runID, input())
		s.Require().NoError(err)
		s.Require().True(res.IsError)
		s.Require().Contains(res.Content, "No quiz is waiting")
	}
}

func (s *ServiceSuite) TestSubmitBadJSON() {
	s.generating("r1")
	res, err := s.svc.Submit(s.ctx, "r1", json.RawMessage(`{"questions": 3}`))
	s.Require().NoError(err)
	s.Require().True(res.IsError)
	s.Require().Contains(res.Content, "The input is not valid")
}

func (s *ServiceSuite) TestSubmitReportsEveryProblem() {
	s.svc.Count = 1
	long := strings.Repeat("x", maxQuestionChars+1)
	cases := []struct {
		name  string
		items []item
		files func()
		want  []string
	}{
		{
			name:  "count mismatch",
			items: []item{good("A?", "a.go"), good("B?", "a.go")},
			files: func() { s.expectFile("a.go", 2, 4, 40) },
			want:  []string{"Send exactly 1 questions; you sent 2."},
		},
		{
			name: "choices",
			items: []item{func() item {
				it := good("A?", "a.go")
				it["choices"] = []string{"Same", " same ", "All of the above", ""}
				it["correct_index"] = 4
				return it
			}()},
			files: func() { s.expectFile("a.go", 2, 4, 40) },
			want: []string{
				"Question 1: choices 0 and 1 are the same.",
				`Question 1: choice 2 is a catch-all ("All of the above")`,
				"Question 1: choice 3 is empty.",
				"Question 1: correct_index 4 is not the index of a choice (0 to 3).",
			},
		},
		{
			name: "too few choices and no correct index",
			items: []item{func() item {
				it := good("A?", "a.go")
				it["choices"] = []string{"a", "b"}
				delete(it, "correct_index")
				return it
			}()},
			files: func() { s.expectFile("a.go", 2, 4, 40) },
			want:  []string{"has 2 choices; give 3 to 5", "correct_index is missing"},
		},
		{
			name: "long texts",
			items: []item{func() item {
				it := good(long, "a.go")
				it["choices"] = []string{"a", "b", strings.Repeat("c", maxChoiceChars+1)}
				it["explanation"] = "a.go " + strings.Repeat("e", maxExplainChars)
				return it
			}()},
			files: func() { s.expectFile("a.go", 2, 4, 40) },
			want: []string{
				"question is over 1000 characters",
				"choice 2 is over 400 characters",
				"explanation is over 2000 characters",
			},
		},
		{
			name: "empty question and explanation",
			items: []item{func() item {
				it := good(" ", "a.go")
				it["explanation"] = ""
				return it
			}()},
			files: func() { s.expectFile("a.go", 2, 4, 40) },
			want:  []string{"question is empty", "explanation is empty"},
		},
		{
			name: "explanation without a cited anchor",
			items: []item{func() item {
				it := good("A?", "a.go")
				it["explanation"] = "Because it does."
				return it
			}()},
			files: func() { s.expectFile("a.go", 2, 4, 40) },
			want:  []string{"explanation doesn't cite an anchor"},
		},
		{
			name: "no anchors",
			items: []item{func() item {
				it := good("A?", "a.go")
				it["anchors"] = []item{}
				return it
			}()},
			want: []string{"has 0 anchors; give 1 to 5"},
		},
		{
			name: "bad anchors",
			items: []item{func() item {
				it := good("A?", "a.go")
				it["explanation"] = "a.go gone.go short.go odd.go"
				it["anchors"] = []item{
					{"path": "/etc/passwd", "start": 1, "end": 2},
					{"path": "a.go", "start": 5, "end": 4},
					{"path": "a.go", "start": 1, "end": 1 + maxAnchorLines},
					{"path": "gone.go", "start": 1, "end": 2},
					{"path": "short.go", "start": 8, "end": 12},
				}
				return it
			}()},
			files: func() {
				s.loop.On("File", mock.Anything, "gone.go", 1, 2).Return(nil, &pluginsdk.StatusError{Status: http.StatusNotFound}).Once()
				s.expectFile("short.go", 8, 12, 10)
			},
			want: []string{
				`anchor 0: path "/etc/passwd" is not a repo-relative file path`,
				"anchor 1: lines 5-4 are not a range",
				"anchor 2: lines 1-201 span over 200 lines",
				"anchor 3: gone.go does not exist",
				"anchor 4: lines 8-12 are past the end of short.go, which has 10 lines",
				"explanation doesn't cite an anchor",
			},
		},
		{
			name: "too many anchors and unreadable ranges",
			items: []item{func() item {
				it := good("A?", "a.go")
				var anchors []item
				for i := range maxAnchors + 1 {
					anchors = append(anchors, item{"path": "a.go", "start": 50 + i, "end": 50 + i})
				}
				anchors[1] = item{"path": "x.go", "start": 1, "end": 1}
				it["anchors"] = anchors
				return it
			}()},
			files: func() {
				s.loop.On("File", mock.Anything, "a.go", mock.Anything, mock.Anything).Return(nil, &pluginsdk.StatusError{Status: http.StatusBadRequest, Body: "the file has 40 lines"})
				s.loop.On("File", mock.Anything, "x.go", 1, 1).Return(nil, errors.New("connection refused")).Once()
			},
			want: []string{
				"has 6 anchors; give 1 to 5",
				"anchor 0: lines 50-50 of a.go can't be read",
				"anchor 1: x.go can't be read right now: connection refused",
			},
		},
	}
	for i, tc := range cases {
		s.Run(tc.name, func() {
			s.loop.ExpectedCalls = nil
			runID := fmt.Sprintf("run-%d", i)
			q := s.generating(runID)
			if tc.files != nil {
				tc.files()
			}
			res, err := s.svc.Submit(s.ctx, runID, input(tc.items...))
			s.Require().NoError(err)
			s.Require().True(res.IsError)
			s.Require().Contains(res.Content, "Nothing was saved")
			for _, w := range tc.want {
				s.Require().Contains(res.Content, w)
			}
			got, err := s.st.Quiz(s.ctx, q.ID)
			s.Require().NoError(err)
			s.Require().Equal(store.StatusGenerating, got.Status)
			_, err = s.st.Transition(s.ctx, q.ID, store.StatusGenerating, store.StatusFailed, "", s.now)
			s.Require().NoError(err)
		})
	}
}

func (s *ServiceSuite) TestSubmitRejectsRepeats() {
	prev := s.generating("old")
	s.Require().NoError(s.st.SaveQuestions(s.ctx, prev.ID, []store.Question{{Question: "Old?", Fingerprint: Fingerprint("Old?")}}, s.now))
	s.generating("r1")
	s.expectFile("a.go", 2, 4, 40)

	res, err := s.svc.Submit(s.ctx, "r1", input(good("old ?", "a.go"), good("New?", "a.go"), good("new", "a.go")))
	s.Require().NoError(err)
	s.Require().True(res.IsError)
	s.Require().Contains(res.Content, "Question 1: repeats an earlier question")
	s.Require().Contains(res.Content, "Question 3: is the same question as question 2")
	s.Require().NotContains(res.Content, "Question 2:")
}

// --- Answer, Current, History, Settings ---

// readyQuiz stores a ready quiz with two questions and returns their ids.
func (s *ServiceSuite) readyQuiz() (*store.Quiz, []store.Question) {
	q := s.generating("r1")
	s.Require().NoError(s.st.SaveQuestions(s.ctx, q.ID, []store.Question{
		{Question: "A?", Choices: []string{"a", "b", "c"}, CorrectIndex: 2, Explanation: "see a.go",
			Anchors: []store.Anchor{{Path: "pkg/a.go", Start: 1, End: 2}}, Fingerprint: "fa", Area: "pkg"},
		{Question: "B?", Choices: []string{"x", "y", "z"}, CorrectIndex: 0, Explanation: "see b.go",
			Anchors: []store.Anchor{{Path: "pkg/b.go", Start: 3, End: 3}}, Fingerprint: "fb", Area: "pkg"},
	}, s.now))
	qs, err := s.st.Questions(s.ctx, q.ID)
	s.Require().NoError(err)
	return q, qs
}

func (s *ServiceSuite) TestAnswer() {
	_, qs := s.readyQuiz()

	g, err := s.svc.Answer(s.ctx, qs[0].ID, 2)
	s.Require().NoError(err)
	s.Require().Equal(&Grade{Correct: true, CorrectIndex: 2, Explanation: "see a.go", Anchors: qs[0].Anchors}, g)

	g, err = s.svc.Answer(s.ctx, qs[1].ID, 1)
	s.Require().NoError(err)
	s.Require().False(g.Correct)
	s.Require().Equal(0, g.CorrectIndex)

	ms, err := s.st.MasteryList(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(ms, 1)
	s.Require().Equal(1, ms[0].Box)
	s.Require().Equal(1, ms[0].Correct)
	s.Require().Equal(1, ms[0].Wrong)
	s.Require().Equal("2026-10-11", ms[0].Due)

	_, err = s.svc.Answer(s.ctx, qs[0].ID, 0)
	s.Require().ErrorIs(err, ErrAnswered)
	_, err = s.svc.Answer(s.ctx, qs[0].ID, 3)
	s.Require().ErrorIs(err, ErrBadChoice)
	_, err = s.svc.Answer(s.ctx, qs[0].ID, -1)
	s.Require().ErrorIs(err, ErrBadChoice)
	_, err = s.svc.Answer(s.ctx, 999, 0)
	s.Require().ErrorIs(err, ErrNotFound)
}

func (s *ServiceSuite) TestCurrent() {
	v, err := s.svc.Current(s.ctx)
	s.Require().NoError(err)
	s.Require().Nil(v)

	q, qs := s.readyQuiz()
	_, err = s.svc.Answer(s.ctx, qs[1].ID, 0)
	s.Require().NoError(err)

	v, err = s.svc.Current(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(q.ID, v.ID)
	s.Require().Equal(store.StatusReady, v.Status)
	s.Require().Equal(Score{Total: 2, Answered: 1, Correct: 1}, v.Score)
	s.Require().Nil(v.Questions[0].Answer)
	s.Require().NotNil(v.Questions[1].Answer)
	s.Require().Equal(0, v.Questions[1].Answer.ChosenIndex)
	s.Require().Equal("see b.go", v.Questions[1].Answer.Explanation)

	// Unanswered questions never carry the answer.
	b, err := json.Marshal(v.Questions[0])
	s.Require().NoError(err)
	s.Require().NotContains(string(b), "correct_index")
	s.Require().NotContains(string(b), "explanation")
	b, err = json.Marshal(v.Questions[1])
	s.Require().NoError(err)
	s.Require().Contains(string(b), `"correct_index":0`)
}

func (s *ServiceSuite) TestQuiz() {
	_, err := s.svc.Quiz(s.ctx, 999)
	s.Require().ErrorIs(err, ErrNoQuiz)

	q, qs := s.readyQuiz()
	_, err = s.svc.Answer(s.ctx, qs[0].ID, 1)
	s.Require().NoError(err)
	v, err := s.svc.Quiz(s.ctx, q.ID)
	s.Require().NoError(err)
	s.Require().Equal(q.ID, v.ID)
	s.Require().Equal(Score{Total: 2, Answered: 1, Correct: 0}, v.Score)
	s.Require().Equal(1, v.Questions[0].Answer.ChosenIndex)
	s.Require().Nil(v.Questions[1].Answer)

	s.Require().NoError(s.svc.Store.Close())
	_, err = s.svc.Quiz(s.ctx, q.ID)
	s.Require().Error(err)
	s.Require().NotErrorIs(err, ErrNoQuiz)
}

func (s *ServiceSuite) TestCurrentExpiresStaleGeneration() {
	s.generating("r1")
	s.now = s.now.Add(time.Hour)
	v, err := s.svc.Current(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(store.StatusExpired, v.Status)
	s.Require().Empty(v.Questions)
}

func (s *ServiceSuite) TestHistory() {
	v, err := s.svc.History(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(&HistoryView{Quizzes: []HistoryQuiz{}, Mastery: []MasteryView{}, Days: []DayView{}}, v)

	q, qs := s.readyQuiz()
	_, err = s.svc.Answer(s.ctx, qs[0].ID, 2)
	s.Require().NoError(err)
	v, err = s.svc.History(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(v.Quizzes, 1)
	s.Require().Equal(q.ID, v.Quizzes[0].ID)
	s.Require().Equal(Score{Total: 2, Answered: 1, Correct: 1}, v.Quizzes[0].Score)
	s.Require().Equal([]MasteryView{{Area: "pkg", Box: 2, MaxBox: 5, Due: "2026-10-12", Correct: 1}}, v.Mastery)
}

// missed stores a ready quiz dated date with qs, each answered wrong in order,
// in areas due on due, and returns the stored questions.
func (s *ServiceSuite) missed(date, due string, qs ...store.Question) []store.Question {
	return s.answered(date, due, false, qs...)
}

// answered stores a ready quiz dated date with qs, each answered (correct or
// not) in order, in areas due on due, and returns the stored questions.
func (s *ServiceSuite) answered(date, due string, correct bool, qs ...store.Question) []store.Question {
	q := &store.Quiz{Date: date, Status: store.StatusGenerating, QuestionCount: len(qs), CreatedAt: s.now}
	s.Require().NoError(s.st.CreateQuiz(s.ctx, q))
	for i := range qs {
		qs[i].Choices = []string{"a", "b"}
		qs[i].Fingerprint = qs[i].Question
	}
	s.Require().NoError(s.st.SaveQuestions(s.ctx, q.ID, qs, s.now))
	stored, err := s.st.Questions(s.ctx, q.ID)
	s.Require().NoError(err)
	for i, sq := range stored {
		set := func(m store.Mastery, _ bool) (store.Mastery, bool) {
			m.Box, m.Due, m.UpdatedAt = 1, due, s.now
			if correct {
				m.Correct++
			} else {
				m.Wrong++
			}
			return m, true
		}
		at := s.now.Add(time.Duration(i) * time.Minute)
		s.Require().NoError(s.st.RecordAttempt(s.ctx, store.Attempt{QuestionID: sq.ID, Correct: correct, AnsweredAt: at}, sq.Area, set))
	}
	return stored
}

func anchored(question, path string, end int) store.Question {
	return store.Question{Question: question, Area: "pkg", Anchors: []store.Anchor{{Path: path, Start: 1, End: end, Hash: hashSnippet("code")}}}
}

func withChoices(q store.Question) store.Question {
	q.Choices, q.Fingerprint = []string{"a", "b"}, q.Question
	return q
}

func (s *ServiceSuite) TestPickReasksChecksTheCode() {
	qs := s.missed("2026-10-09", "2026-10-10",
		anchored("Gone?", "gone.go", 2),
		anchored("Busy?", "busy.go", 2),
		anchored("Edited?", "edit.go", 2),
		anchored("Short?", "short.go", 5),
		anchored("Same?", "same.go", 2),
		store.Question{Question: "Unhashed?", Area: "pkg", Anchors: []store.Anchor{{Path: "x.go", Start: 1, End: 1}}},
		anchored("Spare?", "spare.go", 2),
	)
	s.loop.On("File", mock.Anything, "gone.go", 1, 2).Return(nil, &pluginsdk.StatusError{Status: http.StatusNotFound}).Once()
	s.loop.On("File", mock.Anything, "busy.go", 1, 2).Return(nil, errors.New("down")).Once()
	s.loop.On("File", mock.Anything, "edit.go", 1, 2).Return(&pluginsdk.File{Content: "changed", TotalLines: 9}, nil).Once()
	s.loop.On("File", mock.Anything, "short.go", 1, 5).Return(&pluginsdk.File{Content: "code", TotalLines: 3}, nil).Once()
	s.loop.On("File", mock.Anything, "same.go", 1, 2).Return(&pluginsdk.File{Content: "code", TotalLines: 9}, nil).Once()

	got, err := s.svc.pickReasks(s.ctx, 2)
	s.Require().NoError(err)
	s.Require().Len(got, 2)
	s.Require().Equal(qs[4].ID, got[0].ID)
	s.Require().Equal(qs[5].ID, got[1].ID)
	for i, wantStale := range []bool{true, false, true, true, false, false, false} {
		q, err := s.st.Question(s.ctx, qs[i].ID)
		s.Require().NoError(err)
		s.Require().Equal(wantStale, q.Stale, q.Question)
	}

	none, err := s.svc.pickReasks(s.ctx, 0)
	s.Require().NoError(err)
	s.Require().Empty(none)
}

func (s *ServiceSuite) TestPickReasksWaitsForTheAreaToBeDue() {
	s.missed("2026-10-09", "2026-10-11", anchored("Later?", "later.go", 2))
	got, err := s.svc.pickReasks(s.ctx, 2)
	s.Require().NoError(err)
	s.Require().Empty(got)
}

func (s *ServiceSuite) TestPickReasksStoreErrors() {
	s.missed("2026-10-09", "2026-10-10", anchored("Gone?", "gone.go", 2))
	s.loop.On("File", mock.Anything, "gone.go", 1, 2).Return(nil, &pluginsdk.StatusError{Status: http.StatusBadRequest}).
		Run(func(mock.Arguments) { s.Require().NoError(s.st.Close()) }).Once()
	_, err := s.svc.pickReasks(s.ctx, 1)
	s.Require().Error(err)
	_, err = s.svc.pickReasks(s.ctx, 1)
	s.Require().Error(err)

	st, err := store.Open(s.ctx, filepath.Join(s.T().TempDir(), "again.db"))
	s.Require().NoError(err)
	s.st = st
}

func (s *ServiceSuite) TestSubmitAppendsReasks() {
	old := s.missed("2026-10-09", "2026-10-10", store.Question{Question: "Old?", Area: "pkg", CorrectIndex: 1, Explanation: "because",
		Anchors: []store.Anchor{{Path: "pkg/old.go", Start: 1, End: 1}}})
	q := s.generating("r1")
	s.Require().NoError(s.st.SetPlan(s.ctx, q.ID, nil, []int64{old[0].ID, 999}, 1))
	s.expectFile("pkg/a.go", 2, 4, 40)
	s.expectEvent(EventReady)
	s.loop.On("Notify", mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()

	res, err := s.svc.Submit(s.ctx, "r1", input(good("What does A do?", "pkg/a.go")))
	s.Require().NoError(err)
	s.Require().False(res.IsError, res.Content)
	s.Require().Contains(res.Content, "Saved 1 questions")

	qs, err := s.st.Questions(s.ctx, q.ID)
	s.Require().NoError(err)
	s.Require().Len(qs, 2)
	s.Require().Zero(qs[0].ReaskOf)
	s.Require().Equal(old[0].ID, qs[1].ReaskOf)
	s.Require().Equal(1, qs[1].Index)
	s.Require().Equal("Old?", qs[1].Question)
	s.Require().Equal("because", qs[1].Explanation)

	v, err := s.svc.Current(s.ctx)
	s.Require().NoError(err)
	s.Require().False(v.Questions[0].Reask)
	s.Require().True(v.Questions[1].Reask)
}

func (s *ServiceSuite) TestSubmitReaskStoreError() {
	q := s.generating("r1")
	s.Require().NoError(s.st.SetPlan(s.ctx, q.ID, nil, []int64{1}, 1))
	s.loop.On("File", mock.Anything, "pkg/a.go", 2, 4).Return(&pluginsdk.File{Content: "code", Start: 2, End: 4, TotalLines: 40}, nil).
		Run(func(mock.Arguments) { s.Require().NoError(s.st.Close()) }).Once()
	_, err := s.svc.Submit(s.ctx, "r1", input(good("What does A do?", "pkg/a.go")))
	s.Require().Error(err)

	st, err := store.Open(s.ctx, filepath.Join(s.T().TempDir(), "again.db"))
	s.Require().NoError(err)
	s.st = st
}

func (s *ServiceSuite) TestAnswerChecksTheCode() {
	tests := []struct {
		name      string
		file      *pluginsdk.File
		fileErr   error
		noFile    bool
		markStale bool
		correct   bool
		wantStale bool
		wantBox   int
	}{
		{name: "unchanged", file: &pluginsdk.File{Content: "code", TotalLines: 9}, wantBox: 1},
		{name: "unchanged and right", file: &pluginsdk.File{Content: "code", TotalLines: 9}, correct: true, wantBox: 2},
		{name: "changed", file: &pluginsdk.File{Content: "changed", TotalLines: 9}, wantStale: true},
		{name: "changed but right", file: &pluginsdk.File{Content: "changed", TotalLines: 9}, correct: true, wantStale: true, wantBox: 2},
		{name: "deleted", fileErr: &pluginsdk.StatusError{Status: http.StatusNotFound}, wantStale: true},
		{name: "unreadable", fileErr: errors.New("down"), wantBox: 1},
		{name: "already stale", noFile: true, markStale: true, wantStale: true},
	}
	for i, tt := range tests {
		s.Run(tt.name, func() {
			area := fmt.Sprintf("area%d", i)
			path := area + "/x.go"
			q := s.generating("")
			s.Require().NoError(s.st.SaveQuestions(s.ctx, q.ID, []store.Question{{Question: tt.name, Choices: []string{"a", "b"}, CorrectIndex: 1,
				Anchors: []store.Anchor{{Path: path, Start: 1, End: 2, Hash: hashSnippet("code")}}, Fingerprint: tt.name, Area: area}}, s.now))
			qs, err := s.st.Questions(s.ctx, q.ID)
			s.Require().NoError(err)
			if tt.markStale {
				s.Require().NoError(s.st.MarkStale(s.ctx, qs[0].ID))
			}
			if !tt.noFile {
				s.loop.On("File", mock.Anything, path, 1, 2).Return(tt.file, tt.fileErr).Once()
			}
			chosen := 0
			if tt.correct {
				chosen = 1
			}

			g, err := s.svc.Answer(s.ctx, qs[0].ID, chosen)
			s.Require().NoError(err)
			s.Require().Equal(tt.correct, g.Correct)
			s.Require().Equal(tt.wantStale, g.Stale)
			stored, err := s.st.Question(s.ctx, qs[0].ID)
			s.Require().NoError(err)
			s.Require().Equal(tt.wantStale, stored.Stale)
			ms, err := s.st.MasteryList(s.ctx)
			s.Require().NoError(err)
			box := 0
			for _, m := range ms {
				if m.Area == area {
					box = m.Box
				}
			}
			s.Require().Equal(tt.wantBox, box)

			v, err := s.svc.Current(s.ctx)
			s.Require().NoError(err)
			s.Require().Equal(tt.wantStale, v.Questions[0].Answer.Stale)
		})
	}
}

func (s *ServiceSuite) TestAnswerMarkStaleError() {
	q := s.generating("")
	s.Require().NoError(s.st.SaveQuestions(s.ctx, q.ID, []store.Question{withChoices(anchored("Q?", "x.go", 2))}, s.now))
	qs, err := s.st.Questions(s.ctx, q.ID)
	s.Require().NoError(err)
	// The attempt is stored, then marking the question stale fails.
	db, err := sql.Open("sqlite", "file:"+s.dbPath)
	s.Require().NoError(err)
	defer db.Close()
	_, err = db.Exec(`CREATE TRIGGER no_stale BEFORE UPDATE OF stale ON questions BEGIN SELECT RAISE(FAIL, 'stale is locked'); END`)
	s.Require().NoError(err)
	s.loop.On("File", mock.Anything, "x.go", 1, 2).Return(&pluginsdk.File{Content: "changed", TotalLines: 9}, nil).Once()
	_, err = s.svc.Answer(s.ctx, qs[0].ID, 0)
	s.Require().ErrorContains(err, "stale is locked")
}

func (s *ServiceSuite) TestSummary() {
	v, err := s.svc.Summary(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(&SummaryView{Days: accuracyDays}, v)

	// Answered on 10-07..10-09 with a gap on 10-06; today's quiz isn't
	// answered yet, so the streak runs to yesterday.
	s.answered("2026-10-05", "2026-10-20", true, store.Question{Question: "Q5", Area: "a"})
	s.answered("2026-10-07", "2026-10-20", true, store.Question{Question: "Q7", Area: "a"})
	s.answered("2026-10-08", "2026-10-09", false, store.Question{Question: "Q8", Area: "c"})
	s.answered("2026-10-09", "2026-10-10", false, store.Question{Question: "Q9", Area: "b"})
	// Older than accuracyDays: counts in neither the accuracy nor the streak.
	s.answered("2026-08-01", "2026-10-20", true, store.Question{Question: "Q0", Area: "d"})
	today := s.generating("")
	s.Require().NoError(s.st.SaveQuestions(s.ctx, today.ID, []store.Question{{Question: "T", Choices: []string{"a", "b"}, Fingerprint: "t", Area: "a"}}, s.now))

	v, err = s.svc.Summary(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(&SummaryView{Streak: 3, Answered: 4, Correct: 2, Days: accuracyDays, DueAreas: 2, Weakest: "b"}, v)

	tv, err := s.svc.Today(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(today.ID, tv.Quiz.ID)
	s.Require().Equal(v, tv.Summary)

	// Answering today extends it.
	qs, err := s.st.Questions(s.ctx, today.ID)
	s.Require().NoError(err)
	_, err = s.svc.Answer(s.ctx, qs[0].ID, 0)
	s.Require().NoError(err)
	v, err = s.svc.Summary(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(4, v.Streak)
	s.Require().Equal(5, v.Answered)
	s.Require().Equal(3, v.Correct)

	h, err := s.svc.History(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal([]DayView{
		{Date: "2026-10-05", Score: Score{Total: 1, Answered: 1, Correct: 1}},
		{Date: "2026-10-07", Score: Score{Total: 1, Answered: 1, Correct: 1}},
		{Date: "2026-10-08", Score: Score{Total: 1, Answered: 1}},
		{Date: "2026-10-09", Score: Score{Total: 1, Answered: 1}},
		{Date: "2026-10-10", Score: Score{Total: 1, Answered: 1, Correct: 1}},
	}, h.Days[1:])
	s.Require().Equal("2026-08-01", h.Days[0].Date)
}

func (s *ServiceSuite) TestSettings() {
	v, err := s.svc.Settings(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(SettingsView{}, v)

	s.Require().NoError(s.svc.SaveSettings(s.ctx, SettingsView{Model: "sonnet", Effort: "medium"}))
	v, err = s.svc.Settings(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(SettingsView{Model: "sonnet", Effort: "medium"}, v)

	s.Require().ErrorIs(s.svc.SaveSettings(s.ctx, SettingsView{Model: "gpt"}), ErrBadSettings)
	s.Require().ErrorIs(s.svc.SaveSettings(s.ctx, SettingsView{Effort: "max"}), ErrBadSettings)
}

func (s *ServiceSuite) TestStoreErrorsPropagate() {
	s.Require().NoError(s.st.Close())
	_, err := s.svc.Begin(s.ctx, "manual", true)
	s.Require().Error(err)
	_, err = s.svc.Current(s.ctx)
	s.Require().Error(err)
	_, err = s.svc.History(s.ctx)
	s.Require().Error(err)
	_, err = s.svc.Today(s.ctx)
	s.Require().Error(err)
	_, err = s.svc.Summary(s.ctx)
	s.Require().Error(err)
	_, err = s.svc.Answer(s.ctx, 1, 0)
	s.Require().Error(err)
	s.Require().Error(s.svc.RunDone(s.ctx, "r", "done", ""))
	_, err = s.svc.Submit(s.ctx, "r", input())
	s.Require().Error(err)
	s.loop.On("Channel", mock.Anything).Return(local(), nil).Once()
	s.Require().Error(s.svc.Daily(s.ctx, "schedule"))
	// fail only logs.
	s.svc.fail(s.ctx, 1, "x")

	st, err := store.Open(s.ctx, filepath.Join(s.T().TempDir(), "again.db"))
	s.Require().NoError(err)
	s.st = st
}

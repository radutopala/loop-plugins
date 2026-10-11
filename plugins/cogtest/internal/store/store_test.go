package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type StoreSuite struct {
	suite.Suite
	ctx context.Context
	dir string
	st  *Store
	now time.Time
}

func TestStoreSuite(t *testing.T) {
	suite.Run(t, new(StoreSuite))
}

func (s *StoreSuite) SetupTest() {
	s.ctx = context.Background()
	s.dir = s.T().TempDir()
	st, err := Open(s.ctx, filepath.Join(s.dir, "cogtest.db"))
	s.Require().NoError(err)
	s.st = st
	s.now = time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
}

func (s *StoreSuite) TearDownTest() {
	s.Require().NoError(s.st.Close())
}

func (s *StoreSuite) newQuiz(status Status, created time.Time) *Quiz {
	q := &Quiz{Date: created.Format("2006-01-02"), Status: status, Trigger: "manual", QuestionCount: 2, CreatedAt: created}
	s.Require().NoError(s.st.CreateQuiz(s.ctx, q))
	return q
}

func sampleQuestions() []Question {
	return []Question{
		{Question: "Q1?", Choices: []string{"a", "b", "c"}, CorrectIndex: 1, Explanation: "see a.go",
			Anchors: []Anchor{{Path: "pkg/a.go", Start: 1, End: 3, Hash: "h1"}}, Fingerprint: "f1", Area: "pkg"},
		{Question: "Q2?", Choices: []string{"x", "y", "z"}, CorrectIndex: 0, Explanation: "see b.go",
			Anchors: []Anchor{{Path: "b.go", Start: 2, End: 2, Hash: "h2"}}, Fingerprint: "f2", Area: "."},
	}
}

func (s *StoreSuite) TestMigrationsAreRecordedAndIdempotent() {
	var version int
	s.Require().NoError(s.st.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version))
	s.Require().Equal(len(Migrations), version)

	again, err := Open(s.ctx, filepath.Join(s.dir, "cogtest.db"))
	s.Require().NoError(err)
	defer again.Close()
	var n int
	s.Require().NoError(again.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n))
	s.Require().Equal(len(Migrations), n)
}

func (s *StoreSuite) TestMigrateErrors() {
	cases := []struct {
		name       string
		migrations []Migration
		wantErr    string
	}{
		{"bad sql rolls back", []Migration{{Version: 1, SQL: `CREATE TABLE t (x INT); NOT SQL`}}, "migration 1"},
		{"version gap", []Migration{{Version: 2, SQL: `SELECT 1`}}, "migration 1 has version 2"},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			path := filepath.Join(s.T().TempDir(), "x.db")
			_, err := openWith(s.ctx, path, tc.migrations)
			s.Require().ErrorContains(err, tc.wantErr)
			db, err := sql.Open("sqlite", path)
			s.Require().NoError(err)
			defer db.Close()
			var n int
			s.Require().NoError(db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 't'`).Scan(&n))
			s.Require().Zero(n)
		})
	}
}

func (s *StoreSuite) TestNewMigrationAppliesOnTopOfOld() {
	path := filepath.Join(s.T().TempDir(), "x.db")
	st, err := openWith(s.ctx, path, Migrations)
	s.Require().NoError(err)
	s.Require().NoError(st.Close())
	next := append(append([]Migration{}, Migrations...), Migration{Version: len(Migrations) + 1, SQL: `ALTER TABLE settings ADD COLUMN note TEXT`})
	st, err = openWith(s.ctx, path, next)
	s.Require().NoError(err)
	defer st.Close()
	_, err = st.db.Exec(`UPDATE settings SET note = 'x'`)
	s.Require().NoError(err)
}

func (s *StoreSuite) TestOpenFailsOnBadPath() {
	_, err := Open(s.ctx, filepath.Join(s.dir, "missing", "dir", "x.db"))
	s.Require().Error(err)
}

func (s *StoreSuite) TestQuizLookups() {
	yesterday := s.newQuiz(StatusReady, s.now.AddDate(0, 0, -1))
	today := s.newQuiz(StatusGenerating, s.now)

	q, err := s.st.Quiz(s.ctx, yesterday.ID)
	s.Require().NoError(err)
	s.Require().Equal(yesterday.Date, q.Date)
	s.Require().Equal(StatusReady, q.Status)
	s.Require().Equal(s.now.AddDate(0, 0, -1), q.CreatedAt)
	s.Require().Equal([]string{}, q.Focus)

	latest, err := s.st.LatestQuiz(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(today.ID, latest.ID)

	on, err := s.st.LatestQuizOn(s.ctx, yesterday.Date)
	s.Require().NoError(err)
	s.Require().Equal(yesterday.ID, on.ID)

	gen, err := s.st.GeneratingQuiz(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(today.ID, gen.ID)

	un, err := s.st.UnclaimedQuiz(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(today.ID, un.ID)

	_, err = s.st.Quiz(s.ctx, 999)
	s.Require().ErrorIs(err, ErrNotFound)
	_, err = s.st.QuizByRunID(s.ctx, "")
	s.Require().ErrorIs(err, ErrNotFound)
}

func (s *StoreSuite) TestRunIDAndPlan() {
	q := s.newQuiz(StatusGenerating, s.now)
	s.Require().NoError(s.st.SetRunID(s.ctx, q.ID, "run-1", s.now))
	// A second run id doesn't replace the first.
	s.Require().NoError(s.st.SetRunID(s.ctx, q.ID, "run-2", s.now))
	got, err := s.st.QuizByRunID(s.ctx, "run-1")
	s.Require().NoError(err)
	s.Require().Equal(q.ID, got.ID)
	_, err = s.st.QuizByRunID(s.ctx, "run-2")
	s.Require().ErrorIs(err, ErrNotFound)
	_, err = s.st.UnclaimedQuiz(s.ctx)
	s.Require().ErrorIs(err, ErrNotFound)

	s.Require().Empty(q.Reasks)
	s.Require().NoError(s.st.SetPlan(s.ctx, q.ID, []string{"a.go", "b.go"}, []int64{7, 9}, 3))
	got, err = s.st.Quiz(s.ctx, q.ID)
	s.Require().NoError(err)
	s.Require().Equal([]string{"a.go", "b.go"}, got.Focus)
	s.Require().Equal([]int64{7, 9}, got.Reasks)
	s.Require().Equal(3, got.QuestionCount)
}

func (s *StoreSuite) TestTransitionAndExpire() {
	old := s.newQuiz(StatusGenerating, s.now.Add(-time.Hour))
	fresh := s.newQuiz(StatusGenerating, s.now)

	n, err := s.st.ExpireStale(s.ctx, s.now.Add(-30*time.Minute), s.now)
	s.Require().NoError(err)
	s.Require().EqualValues(1, n)
	got, err := s.st.Quiz(s.ctx, old.ID)
	s.Require().NoError(err)
	s.Require().Equal(StatusExpired, got.Status)
	s.Require().NotEmpty(got.Error)

	ok, err := s.st.Transition(s.ctx, fresh.ID, StatusGenerating, StatusFailed, "boom", s.now)
	s.Require().NoError(err)
	s.Require().True(ok)
	ok, err = s.st.Transition(s.ctx, fresh.ID, StatusGenerating, StatusFailed, "again", s.now)
	s.Require().NoError(err)
	s.Require().False(ok)
	got, err = s.st.Quiz(s.ctx, fresh.ID)
	s.Require().NoError(err)
	s.Require().Equal("boom", got.Error)
}

func (s *StoreSuite) TestSaveQuestions() {
	q := s.newQuiz(StatusGenerating, s.now)
	s.Require().NoError(s.st.SaveQuestions(s.ctx, q.ID, sampleQuestions(), s.now))
	got, err := s.st.Quiz(s.ctx, q.ID)
	s.Require().NoError(err)
	s.Require().Equal(StatusReady, got.Status)

	qs, err := s.st.Questions(s.ctx, q.ID)
	s.Require().NoError(err)
	s.Require().Len(qs, 2)
	s.Require().Equal(0, qs[0].Index)
	s.Require().Equal([]string{"a", "b", "c"}, qs[0].Choices)
	s.Require().Equal([]Anchor{{Path: "pkg/a.go", Start: 1, End: 3, Hash: "h1"}}, qs[0].Anchors)

	one, err := s.st.Question(s.ctx, qs[1].ID)
	s.Require().NoError(err)
	s.Require().Equal("Q2?", one.Question)
	_, err = s.st.Question(s.ctx, 999)
	s.Require().ErrorIs(err, ErrNotFound)

	// A ready quiz takes no more questions.
	s.Require().ErrorIs(s.st.SaveQuestions(s.ctx, q.ID, sampleQuestions(), s.now), ErrNotGenerating)

	fps, err := s.st.RecentFingerprints(s.ctx, 10)
	s.Require().NoError(err)
	s.Require().Equal([]string{"f2", "f1"}, fps)
	texts, err := s.st.RecentQuestionTexts(s.ctx, 1)
	s.Require().NoError(err)
	s.Require().Equal([]string{"Q2?"}, texts)
}

func (s *StoreSuite) TestSaveQuestionsRollsBackOnError() {
	q := s.newQuiz(StatusGenerating, s.now)
	qs := sampleQuestions()
	_, err := s.st.db.Exec(`CREATE TRIGGER no_q2 BEFORE INSERT ON questions WHEN NEW.question = 'Q2?' BEGIN SELECT RAISE(ABORT, 'nope'); END`)
	s.Require().NoError(err)
	s.Require().ErrorContains(s.st.SaveQuestions(s.ctx, q.ID, qs, s.now), "storing question 2")
	got, err := s.st.Quiz(s.ctx, q.ID)
	s.Require().NoError(err)
	s.Require().Equal(StatusGenerating, got.Status)
	stored, err := s.st.Questions(s.ctx, q.ID)
	s.Require().NoError(err)
	s.Require().Empty(stored)
}

func (s *StoreSuite) TestAttemptsAndMastery() {
	q := s.newQuiz(StatusGenerating, s.now)
	s.Require().NoError(s.st.SaveQuestions(s.ctx, q.ID, sampleQuestions(), s.now))
	qs, err := s.st.Questions(s.ctx, q.ID)
	s.Require().NoError(err)

	var seen []bool
	next := func(m Mastery, found bool) (Mastery, bool) {
		seen = append(seen, found)
		m.Box++
		m.Correct++
		m.Due = "2026-10-12"
		m.UpdatedAt = s.now
		return m, true
	}
	s.Require().NoError(s.st.RecordAttempt(s.ctx, Attempt{QuestionID: qs[0].ID, ChosenIndex: 1, Correct: true, AnsweredAt: s.now}, "pkg", next))
	s.Require().ErrorIs(s.st.RecordAttempt(s.ctx, Attempt{QuestionID: qs[0].ID, ChosenIndex: 0, AnsweredAt: s.now}, "pkg", next), ErrAlreadyAnswered)
	s.Require().NoError(s.st.RecordAttempt(s.ctx, Attempt{QuestionID: qs[1].ID, ChosenIndex: 2, AnsweredAt: s.now}, "pkg", next))
	s.Require().Equal([]bool{false, true}, seen)

	attempts, err := s.st.Attempts(s.ctx, q.ID)
	s.Require().NoError(err)
	s.Require().Len(attempts, 2)
	s.Require().True(attempts[qs[0].ID].Correct)
	s.Require().Equal(2, attempts[qs[1].ID].ChosenIndex)
	s.Require().Equal(s.now, attempts[qs[1].ID].AnsweredAt)

	ms, err := s.st.MasteryList(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal([]Mastery{{Area: "pkg", Box: 2, Due: "2026-10-12", Correct: 2, UpdatedAt: s.now}}, ms)

	due, err := s.st.DueAreas(s.ctx, "2026-10-11", 5)
	s.Require().NoError(err)
	s.Require().Empty(due)
	due, err = s.st.DueAreas(s.ctx, "2026-10-12", 5)
	s.Require().NoError(err)
	s.Require().Equal([]string{"pkg"}, due)

	err = s.st.RecordAttempt(s.ctx, Attempt{QuestionID: 999, AnsweredAt: s.now}, "pkg", next)
	s.Require().Error(err)
	s.Require().NotErrorIs(err, ErrAlreadyAnswered)
}

func (s *StoreSuite) TestHistory() {
	ready := s.newQuiz(StatusGenerating, s.now.AddDate(0, 0, -1))
	s.Require().NoError(s.st.SaveQuestions(s.ctx, ready.ID, sampleQuestions(), s.now))
	qs, err := s.st.Questions(s.ctx, ready.ID)
	s.Require().NoError(err)
	keep := func(m Mastery, _ bool) (Mastery, bool) { m.Box, m.Due = 1, "2026-10-11"; return m, true }
	s.Require().NoError(s.st.RecordAttempt(s.ctx, Attempt{QuestionID: qs[0].ID, Correct: true, AnsweredAt: s.now}, "pkg", keep))
	failed := s.newQuiz(StatusFailed, s.now)

	h, err := s.st.History(s.ctx, 10)
	s.Require().NoError(err)
	s.Require().Len(h, 2)
	s.Require().Equal(failed.ID, h[0].ID)
	s.Require().Zero(h[0].Total)
	s.Require().Equal(ready.ID, h[1].ID)
	s.Require().Equal(StatusReady, h[1].Status)
	s.Require().Equal(2, h[1].Total)
	s.Require().Equal(1, h[1].Answered)
	s.Require().Equal(1, h[1].Correct)
}

// answer records an attempt at question id that sets area's mastery due on due.
func (s *StoreSuite) answer(id int64, correct bool, at time.Time, area, due string) {
	set := func(m Mastery, _ bool) (Mastery, bool) { m.Box, m.Due, m.UpdatedAt = 1, due, at; return m, true }
	s.Require().NoError(s.st.RecordAttempt(s.ctx, Attempt{QuestionID: id, Correct: correct, AnsweredAt: at}, area, set))
}

func (s *StoreSuite) TestReaskCandidates() {
	q := s.newQuiz(StatusGenerating, s.now)
	three := append(sampleQuestions(), Question{Question: "Q3?", Choices: []string{"a", "b"}, Anchors: []Anchor{}, Fingerprint: "f3", Area: "pkg"})
	s.Require().NoError(s.st.SaveQuestions(s.ctx, q.ID, three, s.now))
	qs, err := s.st.Questions(s.ctx, q.ID)
	s.Require().NoError(err)
	// Q3 was missed first, then Q1; Q2 was right; "." isn't due yet.
	s.answer(qs[2].ID, false, s.now, "pkg", "2026-10-11")
	s.answer(qs[0].ID, false, s.now.Add(time.Minute), "pkg", "2026-10-11")
	s.answer(qs[1].ID, true, s.now, ".", "2026-10-11")

	got, err := s.st.ReaskCandidates(s.ctx, "2026-10-10", 5)
	s.Require().NoError(err)
	s.Require().Empty(got)
	got, err = s.st.ReaskCandidates(s.ctx, "2026-10-11", 5)
	s.Require().NoError(err)
	s.Require().Len(got, 2)
	s.Require().Equal(qs[2].ID, got[0].ID)
	s.Require().Equal(qs[0].ID, got[1].ID)
	s.Require().Equal(qs[0].Anchors, got[1].Anchors)
	got, err = s.st.ReaskCandidates(s.ctx, "2026-10-11", 1)
	s.Require().NoError(err)
	s.Require().Len(got, 1)

	// A stale question and one already asked again are left out.
	s.Require().NoError(s.st.MarkStale(s.ctx, qs[2].ID))
	next := s.newQuiz(StatusGenerating, s.now.AddDate(0, 0, 1))
	again := qs[0]
	again.ReaskOf = qs[0].ID
	s.Require().NoError(s.st.SaveQuestions(s.ctx, next.ID, []Question{again}, s.now))
	got, err = s.st.ReaskCandidates(s.ctx, "2026-10-11", 5)
	s.Require().NoError(err)
	s.Require().Empty(got)

	stale, err := s.st.Question(s.ctx, qs[2].ID)
	s.Require().NoError(err)
	s.Require().True(stale.Stale)
	copies, err := s.st.Questions(s.ctx, next.ID)
	s.Require().NoError(err)
	s.Require().Equal(qs[0].ID, copies[0].ReaskOf)
	s.Require().Equal("Q1?", copies[0].Question)
}

func (s *StoreSuite) TestDayScores() {
	day1 := s.newQuiz(StatusGenerating, s.now.AddDate(0, 0, -2))
	s.Require().NoError(s.st.SaveQuestions(s.ctx, day1.ID, sampleQuestions(), s.now))
	qs, err := s.st.Questions(s.ctx, day1.ID)
	s.Require().NoError(err)
	s.answer(qs[0].ID, true, s.now, "pkg", "2026-10-11")
	s.answer(qs[1].ID, false, s.now, ".", "2026-10-11")
	day2 := s.newQuiz(StatusGenerating, s.now)
	s.Require().NoError(s.st.SaveQuestions(s.ctx, day2.ID, sampleQuestions(), s.now))
	// Failed and generating quizzes don't count.
	s.newQuiz(StatusFailed, s.now)
	s.newQuiz(StatusGenerating, s.now)

	got, err := s.st.DayScores(s.ctx, "2026-10-01")
	s.Require().NoError(err)
	s.Require().Equal([]DayScore{
		{Date: "2026-10-08", Total: 2, Answered: 2, Correct: 1},
		{Date: "2026-10-10", Total: 2},
	}, got)
	got, err = s.st.DayScores(s.ctx, "2026-10-09")
	s.Require().NoError(err)
	s.Require().Equal([]DayScore{{Date: "2026-10-10", Total: 2}}, got)
}

func (s *StoreSuite) TestSettings() {
	st, err := s.st.Settings(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(Settings{}, st)
	s.Require().NoError(s.st.SaveSettings(s.ctx, Settings{Model: "opus", Effort: "high"}))
	s.Require().NoError(s.st.SaveSettings(s.ctx, Settings{Model: "haiku", Effort: ""}))
	st, err = s.st.Settings(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal(Settings{Model: "haiku"}, st)
}

func (s *StoreSuite) TestCorruptJSONIsReported() {
	q := s.newQuiz(StatusGenerating, s.now)
	s.Require().NoError(s.st.SaveQuestions(s.ctx, q.ID, sampleQuestions(), s.now))
	cases := []struct {
		name  string
		sql   string
		check func() error
	}{
		{"quiz focus", `UPDATE quizzes SET focus = 'x'`, func() error { _, err := s.st.Quiz(s.ctx, q.ID); return err }},
		{"quiz reasks", `UPDATE quizzes SET focus = '[]', reasks = 'x'`, func() error { _, err := s.st.Quiz(s.ctx, q.ID); return err }},
		{"choices", `UPDATE questions SET choices = 'x'`, func() error { _, err := s.st.Questions(s.ctx, q.ID); return err }},
		{"anchors", `UPDATE questions SET choices = '[]', anchors = 'x'`, func() error { _, err := s.st.Questions(s.ctx, q.ID); return err }},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			_, err := s.st.db.Exec(tc.sql)
			s.Require().NoError(err)
			s.Require().Error(tc.check())
		})
	}
}

func (s *StoreSuite) TestClosedDatabaseErrors() {
	s.Require().NoError(s.st.Close())
	calls := map[string]func() error{
		"CreateQuiz": func() error { return s.st.CreateQuiz(s.ctx, &Quiz{CreatedAt: s.now}) },
		"SetRunID":   func() error { return s.st.SetRunID(s.ctx, 1, "r", s.now) },
		"SetPlan":    func() error { return s.st.SetPlan(s.ctx, 1, nil, nil, 1) },
		"Transition": func() error {
			_, err := s.st.Transition(s.ctx, 1, StatusGenerating, StatusFailed, "", s.now)
			return err
		},
		"ExpireStale":         func() error { _, err := s.st.ExpireStale(s.ctx, s.now, s.now); return err },
		"SaveQuestions":       func() error { return s.st.SaveQuestions(s.ctx, 1, nil, s.now) },
		"Questions":           func() error { _, err := s.st.Questions(s.ctx, 1); return err },
		"Attempts":            func() error { _, err := s.st.Attempts(s.ctx, 1); return err },
		"RecordAttempt":       func() error { return s.st.RecordAttempt(s.ctx, Attempt{}, "a", nil) },
		"RecentFingerprints":  func() error { _, err := s.st.RecentFingerprints(s.ctx, 1); return err },
		"MasteryList":         func() error { _, err := s.st.MasteryList(s.ctx); return err },
		"History":             func() error { _, err := s.st.History(s.ctx, 1); return err },
		"Settings":            func() error { _, err := s.st.Settings(s.ctx); return err },
		"SaveSettings":        func() error { return s.st.SaveSettings(s.ctx, Settings{}) },
		"LatestQuiz":          func() error { _, err := s.st.LatestQuiz(s.ctx); return err },
		"RecentQuestionTexts": func() error { _, err := s.st.RecentQuestionTexts(s.ctx, 1); return err },
		"ReaskCandidates":     func() error { _, err := s.st.ReaskCandidates(s.ctx, "d", 1); return err },
		"MarkStale":           func() error { return s.st.MarkStale(s.ctx, 1) },
		"DayScores":           func() error { _, err := s.st.DayScores(s.ctx, "d"); return err },
	}
	for name, call := range calls {
		s.Run(name, func() {
			err := call()
			s.Require().Error(err)
			s.Require().NotErrorIs(err, ErrNotFound)
		})
	}
	// TearDownTest closes again; reopen so it has something to close.
	st, err := Open(s.ctx, filepath.Join(s.dir, "cogtest.db"))
	s.Require().NoError(err)
	s.st = st
}

func TestPrefixed(t *testing.T) {
	require.Equal(t, "z.a, z.b", prefixed("z.", "a, b"))
}

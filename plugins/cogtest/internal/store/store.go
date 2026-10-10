// Package store keeps cogtest's state in a SQLite file in the plugin's data
// volume: quizzes, their questions, the user's attempts, per-area mastery
// and the plugin's settings. One file serves one channel.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Status is where a quiz is in its life.
type Status string

const (
	StatusGenerating Status = "generating"
	StatusReady      Status = "ready"
	StatusFailed     Status = "failed"
	StatusExpired    Status = "expired"
)

var (
	// ErrNotFound is returned when a row doesn't exist.
	ErrNotFound = errors.New("not found")
	// ErrNotGenerating is returned when questions arrive for a quiz that
	// isn't waiting for them.
	ErrNotGenerating = errors.New("the quiz is not generating")
	// ErrAlreadyAnswered is returned for a second attempt at a question.
	ErrAlreadyAnswered = errors.New("the question is already answered")
)

// Anchor points a question at a range of lines in the channel's code.
type Anchor struct {
	Path  string `json:"path"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Hash  string `json:"hash,omitempty"`
}

// Quiz is one generated set of questions.
type Quiz struct {
	ID            int64
	Date          string
	Status        Status
	Trigger       string
	Model         string
	Effort        string
	RunID         string
	QuestionCount int
	Focus         []string
	Error         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Question is a multiple-choice question of a quiz.
type Question struct {
	ID           int64
	QuizID       int64
	Index        int
	Question     string
	Choices      []string
	CorrectIndex int
	Explanation  string
	Anchors      []Anchor
	Fingerprint  string
	Area         string
}

// Attempt is the user's answer to a question.
type Attempt struct {
	QuestionID  int64
	ChosenIndex int
	Correct     bool
	AnsweredAt  time.Time
}

// Mastery is the Leitner state of an area of the code.
type Mastery struct {
	Area      string
	Box       int
	Due       string
	Correct   int
	Wrong     int
	UpdatedAt time.Time
}

// Settings are the user's overrides for generation runs; empty means the
// channel's default.
type Settings struct {
	Model  string
	Effort string
}

// QuizScore is a quiz with how the user did on it.
type QuizScore struct {
	Quiz
	Total    int
	Answered int
	Correct  int
}

// Store is cogtest's database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and migrates it.
func Open(ctx context.Context, path string) (*Store, error) {
	return openWith(ctx, path, Migrations)
}

func openWith(ctx context.Context, path string, migrations []Migration) (*Store, error) {
	q := url.Values{}
	for _, p := range []string{"busy_timeout(5000)", "journal_mode(WAL)", "foreign_keys(1)", "temp_store(MEMORY)"} {
		q.Add("_pragma", p)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	// One writer at a time is plenty for one user and keeps SQLite from
	// answering "database is locked".
	db.SetMaxOpenConns(1)
	if err := migrate(ctx, db, migrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

const quizColumns = `id, date, status, trigger, model, effort, run_id, question_count, focus, error, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanQuiz(row scanner) (*Quiz, error) {
	var q Quiz
	var focus string
	var created, updated int64
	err := row.Scan(&q.ID, &q.Date, &q.Status, &q.Trigger, &q.Model, &q.Effort, &q.RunID,
		&q.QuestionCount, &focus, &q.Error, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(focus), &q.Focus); err != nil {
		return nil, fmt.Errorf("quiz %d focus: %w", q.ID, err)
	}
	q.CreatedAt = time.Unix(created, 0).UTC()
	q.UpdatedAt = time.Unix(updated, 0).UTC()
	return &q, nil
}

// CreateQuiz inserts q and sets its ID.
func (s *Store) CreateQuiz(ctx context.Context, q *Quiz) error {
	focus, err := json.Marshal(nonNil(q.Focus))
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO quizzes
		(date, status, trigger, model, effort, run_id, question_count, focus, error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		q.Date, q.Status, q.Trigger, q.Model, q.Effort, q.RunID, q.QuestionCount, string(focus), q.Error,
		q.CreatedAt.Unix(), q.CreatedAt.Unix())
	if err != nil {
		return fmt.Errorf("creating a quiz: %w", err)
	}
	q.ID, _ = res.LastInsertId()
	q.UpdatedAt = q.CreatedAt
	return nil
}

func (s *Store) quiz(ctx context.Context, where string, args ...any) (*Quiz, error) {
	return scanQuiz(s.db.QueryRowContext(ctx, `SELECT `+quizColumns+` FROM quizzes `+where+` ORDER BY id DESC LIMIT 1`, args...))
}

// Quiz returns the quiz with id.
func (s *Store) Quiz(ctx context.Context, id int64) (*Quiz, error) {
	return s.quiz(ctx, `WHERE id = ?`, id)
}

// LatestQuiz returns the newest quiz.
func (s *Store) LatestQuiz(ctx context.Context) (*Quiz, error) {
	return s.quiz(ctx, ``)
}

// LatestQuizOn returns the newest quiz created on date (YYYY-MM-DD).
func (s *Store) LatestQuizOn(ctx context.Context, date string) (*Quiz, error) {
	return s.quiz(ctx, `WHERE date = ?`, date)
}

// GeneratingQuiz returns the newest quiz still generating.
func (s *Store) GeneratingQuiz(ctx context.Context) (*Quiz, error) {
	return s.quiz(ctx, `WHERE status = ?`, StatusGenerating)
}

// QuizByRunID returns the quiz the agent run runID generates.
func (s *Store) QuizByRunID(ctx context.Context, runID string) (*Quiz, error) {
	if runID == "" {
		return nil, ErrNotFound
	}
	return s.quiz(ctx, `WHERE run_id = ?`, runID)
}

// UnclaimedQuiz returns the newest generating quiz with no run id yet: one
// whose run was started but whose run id isn't stored yet.
func (s *Store) UnclaimedQuiz(ctx context.Context) (*Quiz, error) {
	return s.quiz(ctx, `WHERE status = ? AND run_id = ''`, StatusGenerating)
}

// SetRunID records the run generating quiz id. It does nothing when the quiz
// already has another run id.
func (s *Store) SetRunID(ctx context.Context, id int64, runID string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE quizzes SET run_id = ?, updated_at = ? WHERE id = ? AND run_id IN ('', ?)`,
		runID, now.Unix(), id, runID)
	return err
}

// SetFocus records the files quiz id was planned around.
func (s *Store) SetFocus(ctx context.Context, id int64, focus []string) error {
	b, err := json.Marshal(nonNil(focus))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE quizzes SET focus = ? WHERE id = ?`, string(b), id)
	return err
}

// Transition moves quiz id from status from to status to, recording errMsg.
// It reports whether the quiz was in status from.
func (s *Store) Transition(ctx context.Context, id int64, from, to Status, errMsg string, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE quizzes SET status = ?, error = ?, updated_at = ? WHERE id = ? AND status = ?`,
		to, errMsg, now.Unix(), id, from)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ExpireStale marks quizzes generating since before cutoff as expired and
// returns how many it marked.
func (s *Store) ExpireStale(ctx context.Context, cutoff, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE quizzes SET status = ?, error = ?, updated_at = ? WHERE status = ? AND created_at < ?`,
		StatusExpired, "generation took too long", now.Unix(), StatusGenerating, cutoff.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SaveQuestions stores the questions of quiz quizID and marks it ready, if
// it is still generating.
func (s *Store) SaveQuestions(ctx context.Context, quizID int64, qs []Question, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE quizzes SET status = ?, error = '', updated_at = ? WHERE id = ? AND status = ?`,
		StatusReady, now.Unix(), quizID, StatusGenerating)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotGenerating
	}
	for i, q := range qs {
		choices, err := json.Marshal(nonNil(q.Choices))
		if err != nil {
			return err
		}
		anchors, err := json.Marshal(nonNil(q.Anchors))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO questions
			(quiz_id, idx, question, choices, correct_index, explanation, anchors, fingerprint, area)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			quizID, i, q.Question, string(choices), q.CorrectIndex, q.Explanation, string(anchors), q.Fingerprint, q.Area); err != nil {
			return fmt.Errorf("storing question %d: %w", i+1, err)
		}
	}
	return tx.Commit()
}

const questionColumns = `id, quiz_id, idx, question, choices, correct_index, explanation, anchors, fingerprint, area`

func scanQuestion(row scanner) (*Question, error) {
	var q Question
	var choices, anchors string
	err := row.Scan(&q.ID, &q.QuizID, &q.Index, &q.Question, &choices, &q.CorrectIndex, &q.Explanation, &anchors, &q.Fingerprint, &q.Area)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(choices), &q.Choices); err != nil {
		return nil, fmt.Errorf("question %d choices: %w", q.ID, err)
	}
	if err := json.Unmarshal([]byte(anchors), &q.Anchors); err != nil {
		return nil, fmt.Errorf("question %d anchors: %w", q.ID, err)
	}
	return &q, nil
}

// Question returns the question with id.
func (s *Store) Question(ctx context.Context, id int64) (*Question, error) {
	return scanQuestion(s.db.QueryRowContext(ctx, `SELECT `+questionColumns+` FROM questions WHERE id = ?`, id))
}

// Questions returns the questions of quiz quizID in order.
func (s *Store) Questions(ctx context.Context, quizID int64) ([]Question, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+questionColumns+` FROM questions WHERE quiz_id = ? ORDER BY idx`, quizID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Question
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *q)
	}
	return out, rows.Err()
}

// Attempts returns the user's answers to the questions of quiz quizID, by
// question id.
func (s *Store) Attempts(ctx context.Context, quizID int64) (map[int64]Attempt, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.question_id, a.chosen_index, a.correct, a.answered_at
		FROM attempts a JOIN questions q ON q.id = a.question_id WHERE q.quiz_id = ?`, quizID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Attempt{}
	for rows.Next() {
		var a Attempt
		var at int64
		if err := rows.Scan(&a.QuestionID, &a.ChosenIndex, &a.Correct, &at); err != nil {
			return nil, err
		}
		a.AnsweredAt = time.Unix(at, 0).UTC()
		out[a.QuestionID] = a
	}
	return out, rows.Err()
}

// RecordAttempt stores a and the area's new mastery, which next computes
// from the current one (found is false for an area seen the first time).
func (s *Store) RecordAttempt(ctx context.Context, a Attempt, area string, next func(m Mastery, found bool) Mastery) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO attempts (question_id, chosen_index, correct, answered_at) VALUES (?, ?, ?, ?)`,
		a.QuestionID, a.ChosenIndex, a.Correct, a.AnsweredAt.Unix()); err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrAlreadyAnswered
		}
		return err
	}
	m := Mastery{Area: area}
	var updated int64
	err = tx.QueryRowContext(ctx, `SELECT box, due, correct, wrong, updated_at FROM mastery WHERE area = ?`, area).
		Scan(&m.Box, &m.Due, &m.Correct, &m.Wrong, &updated)
	found := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	m.UpdatedAt = time.Unix(updated, 0).UTC()
	m = next(m, found)
	if _, err := tx.ExecContext(ctx, `INSERT INTO mastery (area, box, due, correct, wrong, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (area) DO UPDATE SET box = excluded.box, due = excluded.due, correct = excluded.correct,
		wrong = excluded.wrong, updated_at = excluded.updated_at`,
		area, m.Box, m.Due, m.Correct, m.Wrong, m.UpdatedAt.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// RecentFingerprints returns the fingerprints of the newest limit questions.
func (s *Store) RecentFingerprints(ctx context.Context, limit int) ([]string, error) {
	return s.strings(ctx, `SELECT fingerprint FROM questions ORDER BY id DESC LIMIT ?`, limit)
}

// RecentQuestionTexts returns the text of the newest limit questions.
func (s *Store) RecentQuestionTexts(ctx context.Context, limit int) ([]string, error) {
	return s.strings(ctx, `SELECT question FROM questions ORDER BY id DESC LIMIT ?`, limit)
}

// DueAreas returns up to limit areas due on or before date, the weakest
// first.
func (s *Store) DueAreas(ctx context.Context, date string, limit int) ([]string, error) {
	return s.strings(ctx, `SELECT area FROM mastery WHERE due <= ? ORDER BY box, due, area LIMIT ?`, date, limit)
}

func (s *Store) strings(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// MasteryList returns every area's mastery, the weakest first.
func (s *Store) MasteryList(ctx context.Context) ([]Mastery, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT area, box, due, correct, wrong, updated_at FROM mastery ORDER BY box, area`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Mastery
	for rows.Next() {
		var m Mastery
		var updated int64
		if err := rows.Scan(&m.Area, &m.Box, &m.Due, &m.Correct, &m.Wrong, &updated); err != nil {
			return nil, err
		}
		m.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

// History returns the newest limit quizzes with their scores.
func (s *Store) History(ctx context.Context, limit int) ([]QuizScore, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+prefixed("z.", quizColumns)+`,
		(SELECT COUNT(*) FROM questions q WHERE q.quiz_id = z.id),
		(SELECT COUNT(*) FROM attempts a JOIN questions q ON q.id = a.question_id WHERE q.quiz_id = z.id),
		(SELECT COUNT(*) FROM attempts a JOIN questions q ON q.id = a.question_id WHERE q.quiz_id = z.id AND a.correct)
		FROM quizzes z ORDER BY z.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QuizScore
	for rows.Next() {
		var qs QuizScore
		var focus string
		var created, updated int64
		if err := rows.Scan(&qs.ID, &qs.Date, &qs.Status, &qs.Trigger, &qs.Model, &qs.Effort, &qs.RunID,
			&qs.QuestionCount, &focus, &qs.Error, &created, &updated, &qs.Total, &qs.Answered, &qs.Correct); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(focus), &qs.Focus)
		qs.CreatedAt = time.Unix(created, 0).UTC()
		qs.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, qs)
	}
	return out, rows.Err()
}

func prefixed(prefix, columns string) string {
	parts := strings.Split(columns, ", ")
	for i, p := range parts {
		parts[i] = prefix + p
	}
	return strings.Join(parts, ", ")
}

// Settings returns the stored settings.
func (s *Store) Settings(ctx context.Context) (Settings, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings WHERE key IN ('model', 'effort')`)
	if err != nil {
		return Settings{}, err
	}
	defer rows.Close()
	var st Settings
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return Settings{}, err
		}
		if k == "model" {
			st.Model = v
		} else {
			st.Effort = v
		}
	}
	return st, rows.Err()
}

// SaveSettings stores st.
func (s *Store) SaveSettings(ctx context.Context, st Settings) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES ('model', ?), ('effort', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, st.Model, st.Effort)
	return err
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

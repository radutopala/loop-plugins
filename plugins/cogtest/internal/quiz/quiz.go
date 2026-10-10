// Package quiz is cogtest's logic: it plans and starts quiz generation runs,
// validates the questions the agent submits, grades answers and tracks
// mastery per area of the code.
package quiz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/loopapi"
	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/store"
)

const (
	// AgentID is the manifest's generating agent.
	AgentID = "generate"
	// ToolName is the manifest's tool the agent submits questions with.
	ToolName = "submit_questions"

	// Event types sent to the panel.
	EventStarted = "quiz_started"
	EventReady   = "quiz_ready"
	EventFailed  = "quiz_failed"

	dateLayout       = "2006-01-02"
	historyLimit     = 200
	avoidLimit       = 30
	historyQuizzes   = 60
	defaultCount     = 5
	defaultStaleness = 30 * time.Minute
	noSubmission     = "the agent ended without submitting questions"
)

// Models and Efforts are the values the settings accept; "" is the
// channel's default.
var (
	Models  = []string{"", "sonnet", "opus", "haiku"}
	Efforts = []string{"", "low", "medium", "high"}
)

var (
	// ErrGenerating is returned when a quiz is already being generated.
	ErrGenerating = errors.New("a quiz is already being generated")
	// ErrDone is returned by the daily action when today's quiz is ready.
	ErrDone = errors.New("today's quiz is already ready")
	// ErrNotLocal is returned for channels that aren't local.
	ErrNotLocal = errors.New("cogtest only runs in local channels")
	// ErrNotFound is returned for an unknown question.
	ErrNotFound = errors.New("question not found")
	// ErrBadChoice is returned for an answer that isn't one of the choices.
	ErrBadChoice = errors.New("chosen_index is not one of the choices")
	// ErrAnswered is returned for a second answer to a question.
	ErrAnswered = errors.New("the question is already answered")
	// ErrBadSettings is returned for settings outside Models and Efforts.
	ErrBadSettings = errors.New("invalid settings")
)

// Service runs cogtest for one channel.
type Service struct {
	Store  *store.Store
	Loop   loopapi.Client
	Logger *slog.Logger
	// Now is the clock; dates are taken in its location.
	Now func() time.Time
	// Rand picks random files for the focus plan.
	Rand *rand.Rand
	// Count is how many questions a quiz has.
	Count int
	// StaleAfter is how long a quiz may generate before it expires.
	StaleAfter time.Duration

	genMu  sync.Mutex
	randMu sync.Mutex
}

// New returns a Service with the default clock, randomness and sizes.
func New(st *store.Store, loop loopapi.Client, logger *slog.Logger) *Service {
	return &Service{
		Store:      st,
		Loop:       loop,
		Logger:     logger,
		Now:        time.Now,
		Rand:       rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
		Count:      defaultCount,
		StaleAfter: defaultStaleness,
	}
}

func (s *Service) today() string { return s.Now().Format(dateLayout) }

func (s *Service) intn(n int) int {
	s.randMu.Lock()
	defer s.randMu.Unlock()
	return s.Rand.IntN(n)
}

func (s *Service) shuffle(v []string) {
	s.randMu.Lock()
	defer s.randMu.Unlock()
	s.Rand.Shuffle(len(v), func(i, j int) { v[i], v[j] = v[j], v[i] })
}

// Begin expires stale quizzes and creates a generating one. Unless force is
// set, it returns ErrDone when today's quiz is ready. It always returns
// ErrGenerating while another quiz is generating.
func (s *Service) Begin(ctx context.Context, trigger string, force bool) (*store.Quiz, error) {
	s.genMu.Lock()
	defer s.genMu.Unlock()
	now := s.Now()
	if n, err := s.Store.ExpireStale(ctx, now.Add(-s.StaleAfter), now); err != nil {
		return nil, err
	} else if n > 0 {
		s.Logger.Info("expired stale quizzes", "count", n)
	}
	if _, err := s.Store.GeneratingQuiz(ctx); err == nil {
		return nil, ErrGenerating
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if !force {
		q, err := s.Store.LatestQuizOn(ctx, s.today())
		switch {
		case err == nil && q.Status == store.StatusReady:
			return nil, ErrDone
		case err != nil && !errors.Is(err, store.ErrNotFound):
			return nil, err
		}
	}
	st, err := s.Store.Settings(ctx)
	if err != nil {
		return nil, err
	}
	q := &store.Quiz{
		Date: now.Format(dateLayout), Status: store.StatusGenerating, Trigger: trigger,
		Model: st.Model, Effort: st.Effort, QuestionCount: s.Count, CreatedAt: now,
	}
	if err := s.Store.CreateQuiz(ctx, q); err != nil {
		return nil, err
	}
	return q, nil
}

// Daily is the daily action: it starts today's quiz unless the channel isn't
// local or today's quiz is ready or generating.
func (s *Service) Daily(ctx context.Context, trigger string) error {
	ch, err := s.Loop.Channel(ctx)
	if err != nil {
		return fmt.Errorf("reading the channel: %w", err)
	}
	if ch.Platform != "local" {
		return ErrNotLocal
	}
	q, err := s.Begin(ctx, trigger, false)
	if err != nil {
		return err
	}
	return s.Launch(ctx, q)
}

// Launch plans quiz q and starts its agent run. On failure it marks the quiz
// failed and tells the panel.
func (s *Service) Launch(ctx context.Context, q *store.Quiz) error {
	if err := s.launch(ctx, q); err != nil {
		s.fail(ctx, q.ID, err.Error())
		return err
	}
	return nil
}

func (s *Service) launch(ctx context.Context, q *store.Quiz) error {
	ch, err := s.Loop.Channel(ctx)
	if err != nil {
		return fmt.Errorf("reading the channel: %w", err)
	}
	if ch.Platform != "local" {
		return fmt.Errorf("%w (this one is %q)", ErrNotLocal, ch.Platform)
	}
	focus, err := s.plan(ctx, ch.DirPath, s.today(), q.QuestionCount+2)
	if err != nil {
		return err
	}
	avoid, err := s.Store.RecentQuestionTexts(ctx, avoidLimit)
	if err != nil {
		return err
	}
	prompt, err := renderPrompt(promptData{
		ChannelName: ch.Name, Count: q.QuestionCount, MaxAnchorLines: maxAnchorLines, Focus: focus, Avoid: avoid,
	})
	if err != nil {
		return err
	}
	paths := make([]string, len(focus))
	for i, f := range focus {
		paths[i] = f.Path
	}
	if err := s.Store.SetFocus(ctx, q.ID, paths); err != nil {
		return err
	}
	run, err := s.Loop.StartRun(ctx, loopapi.RunRequest{Agent: AgentID, Prompt: prompt, Model: q.Model, Effort: q.Effort})
	if err != nil {
		return fmt.Errorf("starting the generation run: %w", err)
	}
	if err := s.Store.SetRunID(ctx, q.ID, run.RunID, s.Now()); err != nil {
		return err
	}
	s.event(ctx, EventStarted, map[string]any{"quiz_id": q.ID})
	return nil
}

// fail marks quiz id failed with msg if it is still generating.
func (s *Service) fail(ctx context.Context, id int64, msg string) {
	ok, err := s.Store.Transition(ctx, id, store.StatusGenerating, store.StatusFailed, msg, s.Now())
	if err != nil {
		s.Logger.Error("marking a quiz failed", "quiz", id, "err", err)
		return
	}
	if ok {
		s.event(ctx, EventFailed, map[string]any{"quiz_id": id, "error": msg})
	}
}

func (s *Service) event(ctx context.Context, typ string, data any) {
	if err := s.Loop.Event(ctx, typ, data); err != nil {
		s.Logger.Warn("sending an event", "type", typ, "err", err)
	}
}

// RunDone handles the end of agent run runID: a quiz still generating failed.
func (s *Service) RunDone(ctx context.Context, runID, status, errMsg string) error {
	q, err := s.Store.QuizByRunID(ctx, runID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if q.Status != store.StatusGenerating {
		return nil
	}
	msg := strings.TrimSpace(errMsg)
	switch {
	case msg != "":
	case status == "failed":
		msg = "the generation run failed"
	default:
		msg = noSubmission
	}
	s.fail(ctx, q.ID, msg)
	return nil
}

// ToolResult is what the agent sees from a tool call.
type ToolResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

func toolError(format string, args ...any) ToolResult {
	return ToolResult{Content: fmt.Sprintf(format, args...), IsError: true}
}

// quizForRun returns the generating quiz of run runID, claiming the newest
// unclaimed one when the run's id isn't stored yet.
func (s *Service) quizForRun(ctx context.Context, runID string) (*store.Quiz, error) {
	q, err := s.Store.QuizByRunID(ctx, runID)
	if !errors.Is(err, store.ErrNotFound) || runID == "" {
		return q, err
	}
	q, err = s.Store.UnclaimedQuiz(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Store.SetRunID(ctx, q.ID, runID, s.Now()); err != nil {
		return nil, err
	}
	return s.Store.QuizByRunID(ctx, runID)
}

// Submit is the submit_questions tool: it validates the agent's questions
// and, if they all pass, stores them and marks the quiz ready.
func (s *Service) Submit(ctx context.Context, runID string, input json.RawMessage) (ToolResult, error) {
	q, err := s.quizForRun(ctx, runID)
	if errors.Is(err, store.ErrNotFound) {
		return toolError("No quiz is waiting for questions from this run. Stop."), nil
	}
	if err != nil {
		return ToolResult{}, err
	}
	if q.Status != store.StatusGenerating {
		return toolError("This quiz no longer accepts questions (it is %s). Stop.", q.Status), nil
	}
	var in SubmitInput
	if err := json.Unmarshal(input, &in); err != nil {
		return toolError("The input is not valid: %v. Send {\"questions\": [...]}.", err), nil
	}
	fps, err := s.Store.RecentFingerprints(ctx, historyLimit)
	if err != nil {
		return ToolResult{}, err
	}
	c := &checker{loop: s.Loop, history: map[string]bool{}, files: map[rangeKey]checkedRange{}}
	for _, fp := range fps {
		c.history[fp] = true
	}

	var problems []string
	if len(in.Questions) != q.QuestionCount {
		problems = append(problems, fmt.Sprintf("- Send exactly %d questions; you sent %d.", q.QuestionCount, len(in.Questions)))
	}
	batch := map[string]int{}
	questions := make([]store.Question, 0, len(in.Questions))
	for i, item := range in.Questions {
		qq, errs := c.checkItem(ctx, item, batch, i)
		for _, e := range errs {
			problems = append(problems, fmt.Sprintf("- Question %d: %s.", i+1, e))
		}
		questions = append(questions, qq)
	}
	if len(problems) > 0 {
		return toolError("Nothing was saved. Fix these problems and call plugin_%s again with all %d questions:\n%s",
			ToolName, q.QuestionCount, strings.Join(problems, "\n")), nil
	}

	if err := s.Store.SaveQuestions(ctx, q.ID, questions, s.Now()); errors.Is(err, store.ErrNotGenerating) {
		return toolError("This quiz no longer accepts questions. Stop."), nil
	} else if err != nil {
		return ToolResult{}, err
	}
	s.event(ctx, EventReady, map[string]any{"quiz_id": q.ID})
	if err := s.Loop.Notify(ctx, "Cognitive test", "Today's quiz is ready"); err != nil {
		s.Logger.Warn("sending the notification", "err", err)
	}
	return ToolResult{Content: fmt.Sprintf("Saved %d questions; the quiz is ready. You are done.", len(questions))}, nil
}

// leitnerDays is how long each Leitner box waits before an area is due.
var leitnerDays = []int{1, 2, 4, 8, 16}

// nextMastery moves m one box up on a correct answer and back to the first
// box on a wrong one.
func nextMastery(m store.Mastery, found, correct bool, now time.Time) store.Mastery {
	if !found || m.Box < 1 {
		m.Box = 1
	}
	if correct {
		m.Box = min(m.Box+1, len(leitnerDays))
		m.Correct++
	} else {
		m.Box = 1
		m.Wrong++
	}
	m.Due = now.AddDate(0, 0, leitnerDays[m.Box-1]).Format(dateLayout)
	m.UpdatedAt = now
	return m
}

// Grade is the answer to an attempt.
type Grade struct {
	Correct      bool           `json:"correct"`
	CorrectIndex int            `json:"correct_index"`
	Explanation  string         `json:"explanation"`
	Anchors      []store.Anchor `json:"anchors"`
}

// Answer grades the user's answer to question id and updates the mastery of
// its area.
func (s *Service) Answer(ctx context.Context, id int64, chosen int) (*Grade, error) {
	q, err := s.Store.Question(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if chosen < 0 || chosen >= len(q.Choices) {
		return nil, ErrBadChoice
	}
	now := s.Now()
	correct := chosen == q.CorrectIndex
	err = s.Store.RecordAttempt(ctx, store.Attempt{QuestionID: id, ChosenIndex: chosen, Correct: correct, AnsweredAt: now}, q.Area,
		func(m store.Mastery, found bool) store.Mastery { return nextMastery(m, found, correct, now) })
	if errors.Is(err, store.ErrAlreadyAnswered) {
		return nil, ErrAnswered
	}
	if err != nil {
		return nil, err
	}
	return &Grade{Correct: correct, CorrectIndex: q.CorrectIndex, Explanation: q.Explanation, Anchors: q.Anchors}, nil
}

// QuizView is a quiz as the panel sees it.
type QuizView struct {
	ID            int64          `json:"id"`
	Date          string         `json:"date"`
	Status        store.Status   `json:"status"`
	Error         string         `json:"error,omitempty"`
	Trigger       string         `json:"trigger"`
	Model         string         `json:"model"`
	Effort        string         `json:"effort"`
	QuestionCount int            `json:"question_count"`
	CreatedAt     time.Time      `json:"created_at"`
	Questions     []QuestionView `json:"questions"`
	Score         Score          `json:"score"`
}

// QuestionView is a question as the panel sees it: the answer is only there
// once the user answered.
type QuestionView struct {
	ID       int64       `json:"id"`
	Index    int         `json:"index"`
	Question string      `json:"question"`
	Choices  []string    `json:"choices"`
	Area     string      `json:"area"`
	Answer   *AnswerView `json:"answer,omitempty"`
}

// AnswerView is the user's answer to a question with the grading.
type AnswerView struct {
	ChosenIndex int       `json:"chosen_index"`
	AnsweredAt  time.Time `json:"answered_at"`
	Grade
}

// Score counts a quiz's questions, answers and correct answers.
type Score struct {
	Total    int `json:"total"`
	Answered int `json:"answered"`
	Correct  int `json:"correct"`
}

// Current returns the newest quiz, or nil when there is none.
func (s *Service) Current(ctx context.Context) (*QuizView, error) {
	now := s.Now()
	if _, err := s.Store.ExpireStale(ctx, now.Add(-s.StaleAfter), now); err != nil {
		return nil, err
	}
	q, err := s.Store.LatestQuiz(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	questions, err := s.Store.Questions(ctx, q.ID)
	if err != nil {
		return nil, err
	}
	attempts, err := s.Store.Attempts(ctx, q.ID)
	if err != nil {
		return nil, err
	}
	v := &QuizView{
		ID: q.ID, Date: q.Date, Status: q.Status, Error: q.Error, Trigger: q.Trigger, Model: q.Model, Effort: q.Effort,
		QuestionCount: q.QuestionCount, CreatedAt: q.CreatedAt, Questions: []QuestionView{},
		Score: Score{Total: len(questions)},
	}
	for _, qq := range questions {
		qv := QuestionView{ID: qq.ID, Index: qq.Index, Question: qq.Question, Choices: qq.Choices, Area: qq.Area}
		if a, ok := attempts[qq.ID]; ok {
			qv.Answer = &AnswerView{
				ChosenIndex: a.ChosenIndex, AnsweredAt: a.AnsweredAt,
				Grade: Grade{Correct: a.Correct, CorrectIndex: qq.CorrectIndex, Explanation: qq.Explanation, Anchors: qq.Anchors},
			}
			v.Score.Answered++
			if a.Correct {
				v.Score.Correct++
			}
		}
		v.Questions = append(v.Questions, qv)
	}
	return v, nil
}

// HistoryView is the panel's History tab.
type HistoryView struct {
	Quizzes []HistoryQuiz `json:"quizzes"`
	Mastery []MasteryView `json:"mastery"`
}

// HistoryQuiz is a past quiz and its score.
type HistoryQuiz struct {
	ID        int64        `json:"id"`
	Date      string       `json:"date"`
	Status    store.Status `json:"status"`
	Error     string       `json:"error,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
	Score
}

// MasteryView is an area's mastery.
type MasteryView struct {
	Area    string `json:"area"`
	Box     int    `json:"box"`
	MaxBox  int    `json:"max_box"`
	Due     string `json:"due"`
	Correct int    `json:"correct"`
	Wrong   int    `json:"wrong"`
}

// History returns the past quizzes with scores and the mastery per area.
func (s *Service) History(ctx context.Context) (*HistoryView, error) {
	quizzes, err := s.Store.History(ctx, historyQuizzes)
	if err != nil {
		return nil, err
	}
	mastery, err := s.Store.MasteryList(ctx)
	if err != nil {
		return nil, err
	}
	v := &HistoryView{Quizzes: []HistoryQuiz{}, Mastery: []MasteryView{}}
	for _, q := range quizzes {
		v.Quizzes = append(v.Quizzes, HistoryQuiz{
			ID: q.ID, Date: q.Date, Status: q.Status, Error: q.Error, CreatedAt: q.CreatedAt,
			Score: Score{Total: q.Total, Answered: q.Answered, Correct: q.Correct},
		})
	}
	for _, m := range mastery {
		v.Mastery = append(v.Mastery, MasteryView{
			Area: m.Area, Box: m.Box, MaxBox: len(leitnerDays), Due: m.Due, Correct: m.Correct, Wrong: m.Wrong,
		})
	}
	return v, nil
}

// SettingsView is the model and effort generation runs use.
type SettingsView struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

// Settings returns the stored settings.
func (s *Service) Settings(ctx context.Context) (SettingsView, error) {
	st, err := s.Store.Settings(ctx)
	return SettingsView{Model: st.Model, Effort: st.Effort}, err
}

// SaveSettings validates and stores v.
func (s *Service) SaveSettings(ctx context.Context, v SettingsView) error {
	if !slices.Contains(Models, v.Model) {
		return fmt.Errorf("%w: model %q is not one of %q", ErrBadSettings, v.Model, Models)
	}
	if !slices.Contains(Efforts, v.Effort) {
		return fmt.Errorf("%w: effort %q is not one of %q", ErrBadSettings, v.Effort, Efforts)
	}
	return s.Store.SaveSettings(ctx, store.Settings{Model: v.Model, Effort: v.Effort})
}

package quiz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/radutopala/loop/pluginsdk"

	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/store"
)

const (
	minChoices       = 3
	maxChoices       = 5
	maxAnchors       = 5
	maxAnchorLines   = 200
	maxQuestionChars = 1000
	maxChoiceChars   = 400
	maxExplainChars  = 2000
)

// SubmitInput is the input of the submit_questions tool.
type SubmitInput struct {
	Questions []ItemInput `json:"questions"`
}

// ItemInput is one question as the agent sends it.
type ItemInput struct {
	Question     string        `json:"question"`
	Choices      []string      `json:"choices"`
	CorrectIndex *int          `json:"correct_index"`
	Explanation  string        `json:"explanation"`
	Anchors      []AnchorInput `json:"anchors"`
}

// AnchorInput is an anchor as the agent sends it.
type AnchorInput struct {
	Path  string `json:"path"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

var catchAllRe = regexp.MustCompile(`(?i)\b(all|none|both|neither)\s+of\s+(the\s+)?(above|these|those|them|the\s+(other\s+)?(choices|options|answers))\b`)

// normalize lowercases s and reduces it to words separated by single
// spaces, so trivially different texts compare equal.
func normalize(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
}

// Fingerprint identifies a question by its normalized text.
func Fingerprint(question string) string {
	sum := sha256.Sum256([]byte(normalize(question)))
	return hex.EncodeToString(sum[:16])
}

func hashSnippet(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:8])
}

// cleanPath returns p as a clean repo-relative path, or "" when it isn't
// one.
func cleanPath(p string) string {
	p = strings.TrimPrefix(strings.TrimSpace(p), "./")
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, `\`) {
		return ""
	}
	c := path.Clean(p)
	if c != p || c == "." || c == ".." || strings.HasPrefix(c, "../") || c == ".git" || strings.HasPrefix(c, ".git/") {
		return ""
	}
	return c
}

type rangeKey struct {
	path       string
	start, end int
}

// checker validates one submission, reading each anchor once.
type checker struct {
	loop    pluginsdk.API
	history map[string]bool
	files   map[rangeKey]checkedRange
}

type checkedRange struct {
	hash string
	err  string
}

// checkItem validates item and returns the question to store, or the
// problems found.
func (c *checker) checkItem(ctx context.Context, item ItemInput, batch map[string]int, n int) (store.Question, []string) {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	q := store.Question{Question: strings.TrimSpace(item.Question), Explanation: strings.TrimSpace(item.Explanation)}
	switch {
	case q.Question == "":
		add("question is empty")
	case len(q.Question) > maxQuestionChars:
		add("question is over %d characters", maxQuestionChars)
	}

	if len(item.Choices) < minChoices || len(item.Choices) > maxChoices {
		add("has %d choices; give %d to %d", len(item.Choices), minChoices, maxChoices)
	}
	seen := map[string]int{}
	for i, ch := range item.Choices {
		ch = strings.TrimSpace(ch)
		q.Choices = append(q.Choices, ch)
		norm := normalize(ch)
		switch {
		case ch == "":
			add("choice %d is empty", i)
		case len(ch) > maxChoiceChars:
			add("choice %d is over %d characters", i, maxChoiceChars)
		case catchAllRe.MatchString(ch):
			add("choice %d is a catch-all (%q); every choice must stand on its own", i, ch)
		}
		if j, dup := seen[norm]; dup && ch != "" {
			add("choices %d and %d are the same", j, i)
		} else {
			seen[norm] = i
		}
	}

	switch {
	case item.CorrectIndex == nil:
		add("correct_index is missing")
	case *item.CorrectIndex < 0 || *item.CorrectIndex >= len(item.Choices):
		add("correct_index %d is not the index of a choice (0 to %d)", *item.CorrectIndex, len(item.Choices)-1)
	default:
		q.CorrectIndex = *item.CorrectIndex
	}

	if q.Explanation == "" {
		add("explanation is empty")
	} else if len(q.Explanation) > maxExplainChars {
		add("explanation is over %d characters", maxExplainChars)
	}

	if len(item.Anchors) == 0 || len(item.Anchors) > maxAnchors {
		add("has %d anchors; give 1 to %d", len(item.Anchors), maxAnchors)
	}
	cited := false
	for i, a := range item.Anchors {
		p := cleanPath(a.Path)
		if p == "" {
			add("anchor %d: path %q is not a repo-relative file path", i, a.Path)
			continue
		}
		if a.Start < 1 || a.End < a.Start {
			add("anchor %d: lines %d-%d are not a range (1-based, start <= end)", i, a.Start, a.End)
			continue
		}
		if a.End-a.Start+1 > maxAnchorLines {
			add("anchor %d: lines %d-%d span over %d lines; point at the code that proves the answer", i, a.Start, a.End, maxAnchorLines)
			continue
		}
		r := c.read(ctx, p, a.Start, a.End)
		if r.err != "" {
			add("anchor %d: %s", i, r.err)
			continue
		}
		q.Anchors = append(q.Anchors, store.Anchor{Path: p, Start: a.Start, End: a.End, Hash: r.hash})
		if strings.Contains(q.Explanation, p) {
			cited = true
		}
	}
	if q.Explanation != "" && len(item.Anchors) > 0 && !cited {
		add("explanation doesn't cite an anchor; mention the path of one of its anchors")
	}

	if q.Question != "" {
		q.Fingerprint = Fingerprint(q.Question)
		if c.history[q.Fingerprint] {
			add("repeats an earlier question; ask something else")
		} else if j, dup := batch[q.Fingerprint]; dup {
			add("is the same question as question %d", j+1)
		} else {
			batch[q.Fingerprint] = n
		}
	}
	if len(q.Anchors) > 0 {
		q.Area = Area(q.Anchors[0].Path)
	}
	return q, errs
}

// read reads lines start..end of p, once per range.
func (c *checker) read(ctx context.Context, p string, start, end int) checkedRange {
	k := rangeKey{p, start, end}
	if r, ok := c.files[k]; ok {
		return r
	}
	var r checkedRange
	f, err := c.loop.File(ctx, p, start, end)
	switch {
	case pluginsdk.HasStatus(err, http.StatusNotFound):
		r.err = fmt.Sprintf("%s does not exist", p)
	case pluginsdk.HasStatus(err, http.StatusBadRequest, http.StatusRequestedRangeNotSatisfiable, http.StatusUnprocessableEntity):
		r.err = fmt.Sprintf("lines %d-%d of %s can't be read: %v", start, end, p, err)
	case err != nil:
		r.err = fmt.Sprintf("%s can't be read right now: %v", p, err)
	case f.TotalLines < end:
		r.err = fmt.Sprintf("lines %d-%d are past the end of %s, which has %d lines", start, end, p, f.TotalLines)
	default:
		r.hash = hashSnippet(f.Content)
	}
	c.files[k] = r
	return r
}

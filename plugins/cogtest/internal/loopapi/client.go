// Package loopapi calls Loop's Plugin API v1 with the plugin's
// channel-scoped token.
package loopapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Channel is the channel the plugin's container serves.
type Channel struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	DirPath  string `json:"dir_path"`
	Platform string `json:"platform"`
	Model    string `json:"model"`
	Effort   string `json:"effort"`
	// Settings are the plugin's settings in the channel's project.
	Settings map[string]any `json:"settings"`
}

// File is a range of lines of a file in the channel.
type File struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	Start      int    `json:"start"`
	End        int    `json:"end"`
	TotalLines int    `json:"total_lines"`
}

// Commit is a recent commit.
type Commit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
	Author  string `json:"author"`
	Time    string `json:"time"`
}

// FileChurn is how many recent commits touched a file.
type FileChurn struct {
	Path    string `json:"path"`
	Commits int    `json:"commits"`
}

// GitRecent is the channel's recent git history.
type GitRecent struct {
	Commits []Commit    `json:"commits"`
	Files   []FileChurn `json:"files"`
}

// Hotspot is a function from the latest quality scan.
type Hotspot struct {
	Path       string  `json:"path"`
	Name       string  `json:"name"`
	StartLine  int     `json:"start_line"`
	Cyclomatic int     `json:"cyclomatic"`
	Cognitive  int     `json:"cognitive"`
	MaxNesting int     `json:"max_nesting"`
	ParamCount int     `json:"param_count"`
	LOC        int     `json:"loc"`
	Score      float64 `json:"score"`
}

// RunRequest asks Loop for a run of one of the plugin's agents.
type RunRequest struct {
	Agent  string `json:"agent"`
	Prompt string `json:"prompt"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

// Run is a started agent run.
type Run struct {
	RunID    string `json:"run_id"`
	ThreadID string `json:"thread_id"`
}

// Client is the Plugin API.
type Client interface {
	Channel(ctx context.Context) (*Channel, error)
	File(ctx context.Context, path string, start, end int) (*File, error)
	Tree(ctx context.Context, glob string) ([]string, error)
	GitRecent(ctx context.Context, days int) (*GitRecent, error)
	Hotspots(ctx context.Context, limit int) ([]Hotspot, error)
	StartRun(ctx context.Context, req RunRequest) (*Run, error)
	Event(ctx context.Context, typ string, data any) error
	Notify(ctx context.Context, title, body string) error
}

// StatusError is a non-2xx answer from Loop.
type StatusError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	msg := fmt.Sprintf("%s %s: Loop answered %d", e.Method, e.Path, e.Status)
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

// HasStatus reports whether err is a StatusError with one of codes.
func HasStatus(err error, codes ...int) bool {
	var se *StatusError
	return errors.As(err, &se) && slices.Contains(codes, se.Status)
}

// Doer sends HTTP requests.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// HTTPClient is the Client over HTTP.
type HTTPClient struct {
	// BaseURL is LOOP_API_URL.
	BaseURL string
	// TokenFile is LOOP_PLUGIN_TOKEN_FILE, read on every call.
	TokenFile string
	// HTTP sends the requests.
	HTTP Doer
	// Timeout caps each call.
	Timeout time.Duration
}

// New returns an HTTPClient for Loop at baseURL.
func New(baseURL, tokenFile string) *HTTPClient {
	return &HTTPClient{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		TokenFile: tokenFile,
		HTTP:      &http.Client{},
		Timeout:   20 * time.Second,
	}
}

const maxBody = 8 << 20

func (c *HTTPClient) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	token, err := os.ReadFile(c.TokenFile)
	if err != nil {
		return fmt.Errorf("reading the plugin token: %w", err)
	}
	u := c.BaseURL + "/api/plugin/v1" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encoding %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return fmt.Errorf("building %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return &StatusError{Method: method, Path: path, Status: resp.StatusCode, Body: strings.TrimSpace(string(msg))}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBody))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
		return fmt.Errorf("decoding %s %s: %w", method, path, err)
	}
	return nil
}

// Channel returns the plugin's channel.
func (c *HTTPClient) Channel(ctx context.Context) (*Channel, error) {
	var ch Channel
	if err := c.do(ctx, http.MethodGet, "/channel", nil, nil, &ch); err != nil {
		return nil, err
	}
	return &ch, nil
}

// File reads lines start..end of path; zero leaves a bound open.
func (c *HTTPClient) File(ctx context.Context, path string, start, end int) (*File, error) {
	q := url.Values{"path": {path}}
	if start > 0 {
		q.Set("start", strconv.Itoa(start))
	}
	if end > 0 {
		q.Set("end", strconv.Itoa(end))
	}
	var f File
	if err := c.do(ctx, http.MethodGet, "/channel/files", q, nil, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// Tree lists the channel's files, filtered by glob when set.
func (c *HTTPClient) Tree(ctx context.Context, glob string) ([]string, error) {
	var q url.Values
	if glob != "" {
		q = url.Values{"glob": {glob}}
	}
	var out struct {
		Files []string `json:"files"`
	}
	if err := c.do(ctx, http.MethodGet, "/channel/tree", q, nil, &out); err != nil {
		return nil, err
	}
	return out.Files, nil
}

// GitRecent returns the last days of history; zero is Loop's default.
func (c *HTTPClient) GitRecent(ctx context.Context, days int) (*GitRecent, error) {
	var q url.Values
	if days > 0 {
		q = url.Values{"days": {strconv.Itoa(days)}}
	}
	var out GitRecent
	if err := c.do(ctx, http.MethodGet, "/channel/git/recent", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Hotspots returns up to limit functions of the latest quality scan.
func (c *HTTPClient) Hotspots(ctx context.Context, limit int) ([]Hotspot, error) {
	var q url.Values
	if limit > 0 {
		q = url.Values{"limit": {strconv.Itoa(limit)}}
	}
	var out struct {
		Functions []Hotspot `json:"functions"`
	}
	if err := c.do(ctx, http.MethodGet, "/channel/quality/hotspots", q, nil, &out); err != nil {
		return nil, err
	}
	return out.Functions, nil
}

// StartRun starts an agent run.
func (c *HTTPClient) StartRun(ctx context.Context, req RunRequest) (*Run, error) {
	var run Run
	if err := c.do(ctx, http.MethodPost, "/runs", nil, req, &run); err != nil {
		return nil, err
	}
	if run.RunID == "" {
		return nil, errors.New("POST /runs: Loop answered without a run_id")
	}
	return &run, nil
}

// Event sends an event to the plugin's panels in the channel.
func (c *HTTPClient) Event(ctx context.Context, typ string, data any) error {
	return c.do(ctx, http.MethodPost, "/events", nil, map[string]any{"type": typ, "data": data}, nil)
}

// Notify shows a desktop notification.
func (c *HTTPClient) Notify(ctx context.Context, title, body string) error {
	return c.do(ctx, http.MethodPost, "/notify", nil, map[string]string{"title": title, "body": body}, nil)
}

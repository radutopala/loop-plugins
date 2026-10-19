// Package loopmock holds a testify mock of Loop's Plugin API.
package loopmock

import (
	"context"

	"github.com/stretchr/testify/mock"

	"github.com/radutopala/loop/pluginsdk"
)

// Client is a mock pluginsdk.API.
type Client struct {
	mock.Mock
}

var _ pluginsdk.API = (*Client)(nil)

func (c *Client) Channel(ctx context.Context) (*pluginsdk.Channel, error) {
	args := c.Called(ctx)
	ch, _ := args.Get(0).(*pluginsdk.Channel)
	return ch, args.Error(1)
}

func (c *Client) File(ctx context.Context, path string, start, end int) (*pluginsdk.File, error) {
	args := c.Called(ctx, path, start, end)
	f, _ := args.Get(0).(*pluginsdk.File)
	return f, args.Error(1)
}

func (c *Client) Tree(ctx context.Context, glob string) (*pluginsdk.Tree, error) {
	args := c.Called(ctx, glob)
	t, _ := args.Get(0).(*pluginsdk.Tree)
	return t, args.Error(1)
}

func (c *Client) GitRecent(ctx context.Context, days int) (*pluginsdk.GitRecent, error) {
	args := c.Called(ctx, days)
	g, _ := args.Get(0).(*pluginsdk.GitRecent)
	return g, args.Error(1)
}

func (c *Client) Hotspots(ctx context.Context, limit int) ([]pluginsdk.Hotspot, error) {
	args := c.Called(ctx, limit)
	h, _ := args.Get(0).([]pluginsdk.Hotspot)
	return h, args.Error(1)
}

func (c *Client) HotspotsPage(ctx context.Context, limit, offset int) (*pluginsdk.Hotspots, error) {
	args := c.Called(ctx, limit, offset)
	h, _ := args.Get(0).(*pluginsdk.Hotspots)
	return h, args.Error(1)
}

func (c *Client) StartRun(ctx context.Context, req pluginsdk.RunRequest) (*pluginsdk.Run, error) {
	args := c.Called(ctx, req)
	r, _ := args.Get(0).(*pluginsdk.Run)
	return r, args.Error(1)
}

func (c *Client) Event(ctx context.Context, typ string, data any) error {
	return c.Called(ctx, typ, data).Error(0)
}

func (c *Client) Notify(ctx context.Context, title, body string) error {
	return c.Called(ctx, title, body).Error(0)
}

func (c *Client) WriteMemory(ctx context.Context, name, content string) error {
	return c.Called(ctx, name, content).Error(0)
}

func (c *Client) DeleteMemory(ctx context.Context, name string) error {
	return c.Called(ctx, name).Error(0)
}

func (c *Client) RunUI(ctx context.Context, cmd pluginsdk.UICommand) (*pluginsdk.UIReply, error) {
	args := c.Called(ctx, cmd)
	r, _ := args.Get(0).(*pluginsdk.UIReply)
	return r, args.Error(1)
}

func (c *Client) SetBadge(ctx context.Context, text, title string) error {
	return c.Called(ctx, text, title).Error(0)
}

func (c *Client) ClearBadge(ctx context.Context) error {
	return c.Called(ctx).Error(0)
}

func (c *Client) PostCard(ctx context.Context, card, title string, data any) (int64, error) {
	args := c.Called(ctx, card, title, data)
	return args.Get(0).(int64), args.Error(1)
}

func (c *Client) GitHead(ctx context.Context) (*pluginsdk.GitHead, error) {
	args := c.Called(ctx)
	g, _ := args.Get(0).(*pluginsdk.GitHead)
	return g, args.Error(1)
}

func (c *Client) RunState(ctx context.Context, runID string) (*pluginsdk.RunState, error) {
	args := c.Called(ctx, runID)
	r, _ := args.Get(0).(*pluginsdk.RunState)
	return r, args.Error(1)
}

func (c *Client) CancelRun(ctx context.Context, runID string) error {
	return c.Called(ctx, runID).Error(0)
}

func (c *Client) OpenFile(ctx context.Context, path string, line int) (*pluginsdk.UIReply, error) {
	args := c.Called(ctx, path, line)
	r, _ := args.Get(0).(*pluginsdk.UIReply)
	return r, args.Error(1)
}

func (c *Client) SetSwitch(ctx context.Context, id, threadID string, state pluginsdk.SwitchState) error {
	return c.Called(ctx, id, threadID, state).Error(0)
}

func (c *Client) ResetSwitch(ctx context.Context, id, threadID string) error {
	return c.Called(ctx, id, threadID).Error(0)
}

func (c *Client) LiveControl(ctx context.Context, threadID string, request map[string]any) (map[string]any, error) {
	args := c.Called(ctx, threadID, request)
	r, _ := args.Get(0).(map[string]any)
	return r, args.Error(1)
}

func (c *Client) HoldLive(ctx context.Context, threadID string) error {
	return c.Called(ctx, threadID).Error(0)
}

func (c *Client) ReleaseLive(ctx context.Context, threadID string) error {
	return c.Called(ctx, threadID).Error(0)
}

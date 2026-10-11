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

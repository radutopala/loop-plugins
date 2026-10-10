// Package loopapitest holds a testify mock of the Plugin API client.
package loopapitest

import (
	"context"

	"github.com/stretchr/testify/mock"

	"github.com/radutopala/loop-plugins/plugins/cogtest/internal/loopapi"
)

// Client is a mock loopapi.Client.
type Client struct {
	mock.Mock
}

var _ loopapi.Client = (*Client)(nil)

func (c *Client) Channel(ctx context.Context) (*loopapi.Channel, error) {
	args := c.Called(ctx)
	ch, _ := args.Get(0).(*loopapi.Channel)
	return ch, args.Error(1)
}

func (c *Client) File(ctx context.Context, path string, start, end int) (*loopapi.File, error) {
	args := c.Called(ctx, path, start, end)
	f, _ := args.Get(0).(*loopapi.File)
	return f, args.Error(1)
}

func (c *Client) Tree(ctx context.Context, glob string) ([]string, error) {
	args := c.Called(ctx, glob)
	files, _ := args.Get(0).([]string)
	return files, args.Error(1)
}

func (c *Client) GitRecent(ctx context.Context, days int) (*loopapi.GitRecent, error) {
	args := c.Called(ctx, days)
	g, _ := args.Get(0).(*loopapi.GitRecent)
	return g, args.Error(1)
}

func (c *Client) Hotspots(ctx context.Context, limit int) ([]loopapi.Hotspot, error) {
	args := c.Called(ctx, limit)
	h, _ := args.Get(0).([]loopapi.Hotspot)
	return h, args.Error(1)
}

func (c *Client) StartRun(ctx context.Context, req loopapi.RunRequest) (*loopapi.Run, error) {
	args := c.Called(ctx, req)
	r, _ := args.Get(0).(*loopapi.Run)
	return r, args.Error(1)
}

func (c *Client) Event(ctx context.Context, typ string, data any) error {
	return c.Called(ctx, typ, data).Error(0)
}

func (c *Client) Notify(ctx context.Context, title, body string) error {
	return c.Called(ctx, title, body).Error(0)
}

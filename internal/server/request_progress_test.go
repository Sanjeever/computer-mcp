package server

import (
	"context"
	"testing"
	"time"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
)

type blockedScreenProvider struct {
	*fakeDesktop
	release chan struct{}
}

func (b *blockedScreenProvider) CaptureWindow(ctx context.Context, _ desktop.Window, _ desktop.Rect, _ int) (desktop.Capture, error) {
	desktop.ReportStage(ctx, "capture_image")
	<-b.release
	return desktop.Capture{}, ctx.Err()
}

func TestCaptureTimeoutReportsActiveStageWhileWorkerRemainsOccupied(t *testing.T) {
	_, fake := setup()
	b := &blockedScreenProvider{fakeDesktop: fake, release: make(chan struct{})}
	e := newEngine(context.Background(), func() (desktop.Backend, error) { return b, nil })
	defer e.Close()
	defer close(b.release)

	request := func(ctx context.Context, s *state) response {
		return observe(ctx, s, ObserveArgs{Screenshot: true})
	}
	for _, want := range []string{"capture_image", "queue"} {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		r := e.call(ctx, "computer_observe", request)
		cancel()
		requireCode(t, r, "timeout")
		if r.timing == nil || r.timing.Stage != want || r.timing.ElapsedMS <= 0 {
			t.Fatalf("timed out in %+v, want %s", r.timing, want)
		}
	}
}

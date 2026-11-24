package server

import (
	"context"
	"slices"
	"time"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
)

type MouseObservation struct {
	Screenshot bool `json:"screenshot,omitempty"`
}

type mouseObservationResult struct {
	InputDelivered   bool         `json:"input_delivered"`
	Observation      *Snapshot    `json:"observation,omitempty"`
	ObservationError *toolFailure `json:"observation_error,omitempty"`
	err              error
}

func mouseObservationFailed(err error) response {
	return response{value: mouseObservationResult{InputDelivered: true, err: err}}
}

func observeAfterMouseInput(ctx context.Context, s *state, source *Snapshot, options MouseObservation) response {
	if s.inputDelivered != nil {
		s.inputDelivered.Store(true)
	}
	// Native events can be queued before the application processes them.
	desktop.ReportStage(ctx, "observation_delay")
	select {
	case <-ctx.Done():
		return mouseObservationFailed(ctx.Err())
	case <-time.After(100 * time.Millisecond):
	}
	a := ObserveArgs{Desktop: source.Window == nil, Screenshot: options.Screenshot}
	var target *desktop.Window
	if a.Desktop {
		desktop.ReportStage(ctx, "display_layout")
		if err := checkDisplayLayout(s.backend, source.Displays); err != nil {
			return mouseObservationFailed(err)
		}
		a.Region = &source.Region
		a.MaxWidth = source.maxWidth
	} else {
		desktop.ReportStage(ctx, "window_list")
		w, err := mouseObservationWindow(s.backend, *source.Window)
		if err != nil {
			return mouseObservationFailed(err)
		}
		target = &w
		if a.Screenshot && w.ID == source.Window.ID {
			a.CaptureMode = source.CaptureMode
			a.MaxWidth = source.maxWidth
			if source.Scope.Region != nil && source.ImageWidth > 0 {
				region := source.Region
				region.X += w.Bounds.X - source.Window.Bounds.X
				region.Y += w.Bounds.Y - source.Window.Bounds.Y
				if rectWithin(region, w.Bounds) {
					a.Region = &region
				}
			}
		}
	}
	base := s.previous
	r := observeTarget(ctx, s, a, target)
	if r.err != nil {
		return mouseObservationFailed(r.err)
	}
	var err error
	if target == nil {
		desktop.ReportStage(ctx, "display_layout")
		err = checkDisplayLayout(s.backend, source.Displays)
	} else {
		var current desktop.Window
		desktop.ReportStage(ctx, "window_list")
		current, err = mouseObservationWindow(s.backend, *source.Window)
		if err == nil && (current.ID != target.ID || current.Bounds != target.Bounds || current.OwnerID != target.OwnerID) {
			err = desktop.Fail("stale_snapshot", "target changed during follow-up observation")
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		s.snapshot, s.previous = nil, base
		return mouseObservationFailed(err)
	}
	return response{value: mouseObservationResult{InputDelivered: true, Observation: s.snapshot}, image: r.image}
}

func checkDisplayLayout(b desktop.Backend, displays []desktop.Display) error {
	status, err := b.Status()
	if err != nil {
		return err
	}
	if !slices.Equal(status.Displays, displays) {
		return desktop.Fail("stale_snapshot", "display layout changed")
	}
	return nil
}

func mouseObservationWindow(b desktop.Backend, source desktop.Window) (desktop.Window, error) {
	windows, err := b.Windows()
	if err != nil {
		return desktop.Window{}, err
	}
	var same desktop.Window
	for _, w := range windows {
		if w.PID != source.PID || w.BlockedBy != "" {
			continue
		}
		if w.ID == source.ID {
			if (w.Active || w.MouseActive) && w.OwnerID == source.OwnerID {
				same = w
			}
		} else if w.Active && (w.OwnerID == source.ID || w.ID == source.OwnerID) {
			return w, nil
		}
	}
	if same.ID != "" {
		return same, nil
	}
	return desktop.Window{}, desktop.Fail("not_active", windowFocusDetail(source.ID, windows))
}

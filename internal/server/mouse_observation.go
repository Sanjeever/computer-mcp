package server

import (
	"context"
	"slices"
	"time"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
)

type MouseObservation struct {
	Screenshot bool `json:"screenshot,omitempty"`
	Focused    bool `json:"focused,omitempty"`
}

func (o *MouseObservation) validate(source *Snapshot) error {
	if o == nil || source.Window != nil {
		return nil
	}
	if !o.Screenshot {
		return desktop.Fail("invalid_argument", "desktop observe_after requires screenshot=true")
	}
	if o.Focused {
		return desktop.Fail("invalid_argument", "observe_after.focused requires a window snapshot")
	}
	return nil
}

type mouseObservationResult struct {
	InputDelivered    bool              `json:"input_delivered"`
	Hit               *desktop.ClickHit `json:"hit,omitempty"`
	Observation       *Snapshot         `json:"observation,omitempty"`
	ObservationError  *toolFailure      `json:"observation_error,omitempty"`
	Transition        *transitionResult `json:"transition,omitempty"`
	TransitionError   *toolFailure      `json:"transition_error,omitempty"`
	err               error
	suggestedRegion   *desktop.Rect
	transitionErr     error
	transitionImage   []byte
	transitionPending bool
}

func (r mouseObservationResult) failed(err error, region *desktop.Rect) mouseObservationResult {
	r.err, r.suggestedRegion = err, region
	return r
}

func (s *state) confirmMouseInput(hit *desktop.ClickHit, transitionPending bool) mouseObservationResult {
	return s.publishMouseResult(mouseObservationResult{InputDelivered: true, Hit: hit, transitionPending: transitionPending})
}

func (s *state) publishMouseResult(result mouseObservationResult) mouseObservationResult {
	if s.mouseDelivery != nil {
		s.mouseDelivery.Store(&result)
	}
	return result
}

func observeAfterMouseInput(ctx context.Context, s *state, source *Snapshot, options MouseObservation, result mouseObservationResult) response {
	followUp, image := captureAfterMouseInput(ctx, s, source, options, mouseObservationWindow)
	result.Observation, result.err, result.suggestedRegion = followUp.Observation, followUp.err, followUp.suggestedRegion
	return response{value: result, image: image}
}

func captureAfterMouseInput(ctx context.Context, s *state, source *Snapshot, options MouseObservation, resolveWindow func(desktop.Backend, desktop.Window) (desktop.Window, *desktop.Rect, error)) (mouseObservationResult, []byte) {
	result := mouseObservationResult{InputDelivered: true}
	// Native events can be queued before the application processes them.
	desktop.ReportStage(ctx, "observation_delay")
	select {
	case <-ctx.Done():
		return result.failed(ctx.Err(), nil), nil
	case <-time.After(100 * time.Millisecond):
	}
	a := ObserveArgs{Desktop: source.Window == nil, Screenshot: options.Screenshot}
	var target *desktop.Window
	if a.Desktop {
		desktop.ReportStage(ctx, "display_layout")
		if err := checkDisplayLayout(s.backend, source.Displays); err != nil {
			return result.failed(err, nil), nil
		}
		a.Region = &source.Region
		a.MaxWidth = source.maxWidth
	} else {
		desktop.ReportStage(ctx, "window_list")
		w, region, err := resolveWindow(s.backend, *source.Window)
		if err != nil {
			return result.failed(err, region), nil
		}
		target = &w
		if a.Screenshot && w.Menu {
			a.MaxWidth = source.maxWidth
		} else if a.Screenshot && w.ID == source.Window.ID {
			a.CaptureMode = source.CaptureMode
			a.MaxWidth = source.maxWidth
			a.ScreenshotOnly = source.ScreenshotOnly
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
		if target != nil && target.Menu {
			return result.failed(r.err, &target.Bounds), nil
		}
		return result.failed(r.err, nil), nil
	}
	if options.Focused && target != nil && target.Active && ctx.Err() == nil {
		desktop.ReportStage(ctx, "focus")
		focused, err := s.backend.Focus(*target)
		if err != nil {
			s.snapshot.FocusError = clip(err.Error(), 512)
		} else if focused.Focused {
			summary := &desktop.ControlSummary{Role: clip(focused.Role, 128), Bounds: focused.Bounds}
			if !focused.Password {
				summary.Name = clip(focused.Name, 512)
			}
			s.snapshot.Focused = summary
		}
	}
	var err error
	if target == nil {
		desktop.ReportStage(ctx, "display_layout")
		err = checkDisplayLayout(s.backend, source.Displays)
	} else {
		var current desktop.Window
		desktop.ReportStage(ctx, "window_list")
		current, _, err = resolveWindow(s.backend, *source.Window)
		if err == nil && (current.ID != target.ID || current.Bounds != target.Bounds || current.OwnerID != target.OwnerID) {
			err = desktop.Fail("stale_snapshot", "target changed during follow-up observation")
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		s.snapshot, s.previous = nil, base
		return result.failed(err, nil), nil
	}
	result.Observation = s.snapshot
	return result, r.image
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

func observeMenu(b desktop.Backend, source desktop.Window) (desktop.Window, *desktop.Rect) {
	observer, ok := b.(desktop.MenuObserver)
	if !ok {
		return desktop.Window{}, nil
	}
	if source.Menu {
		source.ID = source.OwnerID
	}
	return observer.ActiveMenu(source)
}

func mouseObservationWindow(b desktop.Backend, source desktop.Window) (desktop.Window, *desktop.Rect, error) {
	menu, region := observeMenu(b, source)
	if menu.ID != "" {
		return menu, nil, nil
	}
	if region != nil {
		return desktop.Window{}, region, desktop.Fail("not_active", "menu ownership could not be verified; target_window_id="+source.ID)
	}
	windows, err := b.Windows()
	if err != nil {
		return desktop.Window{}, region, err
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
			return w, nil, nil
		}
	}
	if same.ID != "" {
		return same, nil, nil
	}
	return desktop.Window{}, region, desktop.Fail("not_active", windowFocusDetail(source.ID, windows))
}

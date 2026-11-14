package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
)

func activateWindow(ctx context.Context, b desktop.Backend, w desktop.Window, timeout int) (desktop.Window, error) {
	if w.BlockedBy != "" {
		return w, desktop.Fail("window_blocked", w.BlockedBy)
	}
	if err := ctx.Err(); err != nil {
		return w, err
	}
	if err := b.Window(w, "activate", desktop.Rect{}); err != nil {
		return w, err
	}
	if timeout == 0 {
		timeout = 2000
	}
	deadline := time.NewTimer(time.Duration(timeout) * time.Millisecond)
	defer deadline.Stop()
	for {
		current, err := findWindow(b, w.ID, true)
		if err == nil {
			return current, ctx.Err()
		}
		var de *desktop.Error
		if !errors.As(err, &de) || (de.Code != "not_active" && de.Code != "window_not_found") {
			return current, err
		}
		select {
		case <-ctx.Done():
			return current, ctx.Err()
		case <-deadline.C:
			return current, desktop.Fail("activation_failed", de.Detail)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func prepareKeyboard(ctx context.Context, s *state, id string, auto bool, focusName string, timeout int) error {
	if timeout < 0 || timeout > 10000 || len(focusName) > 1024 {
		return desktop.Fail("invalid_argument", "timeout_ms: 1..10000; focus_name <=1024 bytes")
	}
	if timeout == 0 {
		timeout = 2000
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()
	w, err := findWindow(s.backend, id, !auto)
	if err != nil {
		return err
	}
	if auto && !w.Active {
		s.snapshot = nil
		w, err = activateWindow(ctx, s.backend, w, timeout)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) && parent.Err() == nil {
				return desktop.Fail("activation_failed", "target="+id+"; no input sent")
			}
			return err
		}
	}
	for {
		w, err = findWindow(s.backend, id, true)
		if err != nil {
			return err
		}
		if focusName == "" {
			return ctx.Err()
		}
		focused, err := s.backend.Focus(w)
		if err != nil {
			return err
		}
		if focused.Focused && focused.Name == focusName {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			if parent.Err() != nil {
				return parent.Err()
			}
			return desktop.Fail("focus_timeout", fmt.Sprintf("expected_name=%q; actual_name=%q; actual_role=%q; focused=%t", focusName, focused.Name, focused.Role, focused.Focused))
		case <-time.After(50 * time.Millisecond):
		}
	}
}

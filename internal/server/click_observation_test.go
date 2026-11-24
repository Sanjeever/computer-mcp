package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
)

type clickDesktop struct {
	*fakeDesktop
	afterInput    func()
	duringObserve func()
	captureErr    error
}

func (b *clickDesktop) Input(ctx context.Context, op string, in desktop.Input) error {
	err := b.fakeDesktop.Input(ctx, op, in)
	if err == nil && b.afterInput != nil {
		b.afterInput()
	}
	return err
}

func (b *clickDesktop) Observe(ctx context.Context, w desktop.Window, o desktop.ObserveOptions) ([]desktop.Element, bool, error) {
	if b.duringObserve != nil {
		b.duringObserve()
	}
	return b.fakeDesktop.Observe(ctx, w, o)
}

func (b *clickDesktop) CaptureWindow(ctx context.Context, w desktop.Window, r desktop.Rect, width int) (desktop.Capture, error) {
	if b.captureErr != nil {
		return desktop.Capture{}, b.captureErr
	}
	return b.fakeDesktop.CaptureWindow(ctx, w, r, width)
}

func setupClickDesktop() (*state, *clickDesktop) {
	s, fake := setup()
	b := &clickDesktop{fakeDesktop: fake}
	s.backend = b
	return s, b
}

func requireClickObservation(t *testing.T, r response) *Snapshot {
	t.Helper()
	result, ok := r.value.(mouseObservationResult)
	if r.err != nil || !ok || !result.InputDelivered || result.Observation == nil || result.err != nil {
		t.Fatalf("click observation: %+v, %v", r.value, r.err)
	}
	return result.Observation
}

func TestClickObservationFollowsOwnedDialog(t *testing.T) {
	s, b := setupClickDesktop()
	ctx := context.Background()
	main := b.windows[0]
	dialog := desktop.Window{ID: "dialog", OwnerID: main.ID, PID: main.PID, Active: true, Bounds: main.Bounds}
	open := b.nodes[0]
	closeButton := open
	closeButton.ID = "close"
	closeButton.Name = "Close dialog"
	if r := observe(ctx, s, ObserveArgs{}); r.err != nil {
		t.Fatal(r.err)
	}
	args := ClickArgs{SnapshotID: s.snapshot.ID, ElementID: open.ID, ObserveAfter: &MouseObservation{}}
	b.afterInput = func() {
		blocked := main
		blocked.Active, blocked.BlockedBy = false, dialog.ID
		b.windows = []desktop.Window{blocked, dialog}
		b.nodes = []desktop.Element{closeButton}
	}
	opened := requireClickObservation(t, click(ctx, s, args))
	if opened.Window.ID != dialog.ID || opened.ID == args.SnapshotID || len(opened.Elements) != 1 || opened.Elements[0].ID != closeButton.ID {
		t.Fatalf("opened observation: %+v", opened)
	}
	requireCode(t, click(ctx, s, args), "stale_snapshot")
	b.afterInput = func() {
		b.windows = []desktop.Window{main}
		b.nodes = []desktop.Element{open}
	}
	closed := requireClickObservation(t, click(ctx, s, ClickArgs{SnapshotID: opened.ID, ElementID: closeButton.ID, ObserveAfter: &MouseObservation{}}))
	if closed.Window.ID != main.ID || closed != s.snapshot || closed.ID == opened.ID || b.inputs != 2 {
		t.Fatalf("closed observation: %+v, inputs=%d", closed, b.inputs)
	}
}

func TestClickObservationReusesImageCoordinates(t *testing.T) {
	s, b := setupClickDesktop()
	ctx := context.Background()
	region := desktop.Rect{X: 0, Y: 120, Width: 500, Height: 200}
	if r := observe(ctx, s, ObserveArgs{Screenshot: true, CaptureMode: "screen", Region: &region, MaxWidth: 500}); r.err != nil {
		t.Fatal(r.err)
	}
	old := s.snapshot
	b.afterInput = func() { b.windows[0].Bounds.X += 40 }
	point := desktop.Point{X: 100, Y: 50}
	r := click(ctx, s, ClickArgs{SnapshotID: old.ID, Point: &point, ObserveAfter: &MouseObservation{Screenshot: true}})
	next := requireClickObservation(t, r)
	region.X += 40
	if next.ID == old.ID || next.Region != region || next.CaptureMode != "screen" || next.maxWidth != 500 || len(r.image) == 0 {
		t.Fatalf("follow-up image: %+v", next)
	}
	b.afterInput = nil
	if r := click(ctx, s, ClickArgs{SnapshotID: next.ID, Point: &point}); r.err != nil {
		t.Fatal(r.err)
	}
	if b.input.Point != (desktop.Point{X: 140, Y: 160}) || s.snapshot != nil {
		t.Fatalf("second click: %+v, current=%+v", b.input.Point, s.snapshot)
	}
}

func TestDesktopClickObservationRequiresImage(t *testing.T) {
	s, b := setupClickDesktop()
	b.displays = []desktop.Display{{ID: "main", Bounds: desktop.Rect{X: -800, Width: 800, Height: 400}}}
	ctx := context.Background()
	if r := observe(ctx, s, ObserveArgs{Desktop: true, MaxWidth: 500}); r.err != nil {
		t.Fatal(r.err)
	}
	old := s.snapshot
	args := ClickArgs{SnapshotID: old.ID, Point: &desktop.Point{X: 10, Y: 10}, ObserveAfter: &MouseObservation{}}
	requireCode(t, click(ctx, s, args), "invalid_argument")
	if b.inputs != 0 || s.snapshot != old {
		t.Fatal("invalid options consumed the desktop snapshot")
	}
	args.ObserveAfter.Screenshot = true
	r := click(ctx, s, args)
	next := requireClickObservation(t, r)
	if next.Window != nil || next.ID == old.ID || next.Region != old.Region || next.maxWidth != old.maxWidth || len(r.image) == 0 {
		t.Fatalf("desktop follow-up: %+v", next)
	}
}

func TestClickObservationFailurePreservesDelivery(t *testing.T) {
	for _, failure := range []string{"unrelated_before_capture", "unrelated_during_capture", "capture_error"} {
		t.Run(failure, func(t *testing.T) {
			s, b := setupClickDesktop()
			ctx := context.Background()
			if r := observe(ctx, s, ObserveArgs{}); r.err != nil {
				t.Fatal(r.err)
			}
			old := s.snapshot
			changeForeground := func() {
				// Keep the PID to verify that process identity alone is insufficient.
				b.windows[0].ID = "unrelated"
			}
			wantCode := "not_active"
			switch failure {
			case "unrelated_before_capture":
				b.afterInput = changeForeground
			case "unrelated_during_capture":
				b.duringObserve = changeForeground
			case "capture_error":
				wantCode = "native_error"
				b.captureErr = desktop.Fail(wantCode, "capture failed")
			}
			r := click(ctx, s, ClickArgs{SnapshotID: old.ID, ElementID: "0", ObserveAfter: &MouseObservation{Screenshot: true}})
			result, ok := r.value.(mouseObservationResult)
			if r.err != nil || !ok || !result.InputDelivered || result.Observation != nil || len(r.image) != 0 {
				t.Fatalf("failed observation: %+v, %v", r.value, r.err)
			}
			requireCode(t, response{err: result.err}, wantCode)
			if wantCode == "not_active" {
				_, detail := errorCode(result.err)
				if !strings.Contains(detail, "target_window_id="+old.Window.ID) || !strings.Contains(detail, "active_window_id=unrelated") {
					t.Fatalf("follow-up focus diagnostic: %s", detail)
				}
			}
			if b.inputs != 1 || s.snapshot != nil || s.previous != old {
				t.Fatalf("failure changed delivery or observation state: inputs=%d current=%+v previous=%+v", b.inputs, s.snapshot, s.previous)
			}
			if failure == "unrelated_before_capture" && b.calls != 1 {
				t.Fatal("observed the unrelated window")
			}
		})
	}
}

func TestClickObservationCancellationWhileProviderIsBlocked(t *testing.T) {
	_, b := setupClickDesktop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e := newEngine(ctx, func() (desktop.Backend, error) { return b, nil })
	defer e.Close()
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	r := e.call(ctx, "computer_observe", func(ctx context.Context, s *state) response { return observe(ctx, s, ObserveArgs{}) })
	if r.err != nil {
		t.Fatal(r.err)
	}
	snapshot := r.value.(*Snapshot)
	b.duringObserve = func() {
		close(started)
		<-release
	}
	callCtx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan response, 1)
	go func() {
		done <- e.call(callCtx, "computer_click", func(ctx context.Context, s *state) response {
			return click(ctx, s, ClickArgs{SnapshotID: snapshot.ID, ElementID: "0", ObserveAfter: &MouseObservation{}})
		})
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("follow-up never entered the provider")
	}
	stop()
	select {
	case r := <-done:
		result, ok := r.value.(mouseObservationResult)
		if r.err != nil || !ok || !result.InputDelivered || result.Observation != nil || !errors.Is(result.err, context.Canceled) {
			t.Fatalf("canceled click: %+v, %v", r.value, r.err)
		}
		if r.timing == nil || r.timing.Stage != "accessibility" {
			t.Fatalf("missing follow-up stage: %+v", r.timing)
		}
	case <-ctx.Done():
		t.Fatal("canceled click still waiting for the provider")
	}
}

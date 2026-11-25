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
	focusErr      error
	focusCalls    int
	menu          desktop.Window
	menuRegion    *desktop.Rect
}

type hitDesktop struct {
	*clickDesktop
	windowHits int
	duringHit  func()
}

func (b *hitDesktop) ClickWindowHit(w *desktop.Window, p desktop.Point) (desktop.ClickHit, error) {
	b.windowHits++
	if w != nil {
		if err := b.CheckPoint(*w, p, false); err != nil {
			return desktop.ClickHit{}, err
		}
	}
	return desktop.ClickHit{DesktopPoint: p, WindowID: "w1"}, nil
}

func (b *hitDesktop) ClickHit(_ context.Context, p desktop.Point) desktop.ClickHit {
	if b.duringHit != nil {
		b.duringHit()
	}
	return desktop.ClickHit{DesktopPoint: p, WindowID: "w1", Control: &desktop.ControlSummary{Role: "pane", Name: "Ribbon"}}
}

func (b *clickDesktop) Focus(w desktop.Window) (desktop.Element, error) {
	b.focusCalls++
	if b.focusErr != nil {
		return desktop.Element{}, b.focusErr
	}
	return b.fakeDesktop.Focus(w)
}

func (b *clickDesktop) ActiveMenu(owner desktop.Window) (desktop.Window, *desktop.Rect) {
	if b.menu.OwnerID == owner.ID {
		return b.menu, nil
	}
	return desktop.Window{}, b.menuRegion
}

func (b *clickDesktop) Input(ctx context.Context, op string, in desktop.Input) error {
	err := b.fakeDesktop.Input(ctx, op, in)
	if err == nil && b.afterInput != nil {
		b.afterInput()
	}
	return err
}

func (b *clickDesktop) Observe(ctx context.Context, w desktop.Window, o desktop.ObserveOptions) ([]desktop.Element, desktop.Truncation, error) {
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

func TestMouseObservationAddsFocusToScreenshotOnly(t *testing.T) {
	s, b := setupClickDesktop()
	ctx := context.Background()
	if r := observe(ctx, s, ObserveArgs{ScreenshotOnly: true}); r.err != nil {
		t.Fatal(r.err)
	}
	point := desktop.Point{X: 100, Y: 50}
	b.afterInput = func() {
		b.focus = desktop.Element{Role: "pane", Name: "Use rectangle on canvas", Bounds: b.windows[0].Bounds, Focused: true}
	}
	options := &MouseObservation{Screenshot: true, Focused: true}
	r := click(ctx, s, ClickArgs{SnapshotID: s.snapshot.ID, Point: &point, ObserveAfter: options})
	next := requireClickObservation(t, r)
	if b.focusCalls != 1 || b.calls != 0 || len(r.image) == 0 || !next.ScreenshotOnly ||
		next.Focused == nil || next.Focused.Name != b.focus.Name || next.Focused.Bounds != b.focus.Bounds {
		t.Fatalf("screenshot with focus: %+v, focus calls=%d, tree calls=%d", next, b.focusCalls, b.calls)
	}
	b.focusErr = desktop.Fail("native_error", "HRESULT 0x80004005")
	r = drag(ctx, s, DragArgs{SnapshotID: next.ID, From: &point, To: &desktop.Point{X: 120, Y: 60}, ObserveAfter: options})
	next = requireClickObservation(t, r)
	if b.focusCalls != 2 || b.calls != 0 || len(r.image) == 0 || next.Focused != nil || !strings.Contains(next.FocusError, "HRESULT 0x80004005") {
		t.Fatalf("screenshot with focus lookup error: %+v", next)
	}
}

func TestClickObservationFollowsOwnedMenu(t *testing.T) {
	for _, screenshot := range []bool{false, true} {
		s, b := setupClickDesktop()
		ctx := context.Background()
		main := b.windows[0]
		menu := desktop.Window{ID: "menu", OwnerID: main.ID, PID: main.PID, Menu: true, MouseActive: true, Bounds: b.nodes[0].Bounds}
		if r := observe(ctx, s, ObserveArgs{}); r.err != nil {
			t.Fatal(r.err)
		}
		b.afterInput = func() {
			b.windows[0].Active = false
			b.menu = menu
		}
		r := click(ctx, s, ClickArgs{SnapshotID: s.snapshot.ID, ElementID: "0", ObserveAfter: &MouseObservation{Screenshot: screenshot}})
		next := requireClickObservation(t, r)
		if *next.Window != menu || next.Region != menu.Bounds || (len(r.image) > 0) != screenshot || screenshot && (next.CaptureMode != "screen" || b.calls != 1) {
			t.Fatalf("menu observation screenshot=%t: %+v", screenshot, next)
		}
		b.menu = desktop.Window{}
		requireCode(t, click(ctx, s, ClickArgs{SnapshotID: next.ID, Point: &desktop.Point{X: 5, Y: 5}}), "stale_snapshot")
		if b.inputs != 1 {
			t.Fatal("sent input to a closed menu")
		}
	}
}

func TestUnverifiedMenuOffersRegionWithoutCapturing(t *testing.T) {
	s, b := setupClickDesktop()
	ctx := context.Background()
	if r := observe(ctx, s, ObserveArgs{}); r.err != nil {
		t.Fatal(r.err)
	}
	region := b.nodes[0].Bounds
	b.afterInput = func() { b.menuRegion = &region }
	r := click(ctx, s, ClickArgs{SnapshotID: s.snapshot.ID, ElementID: "0", ObserveAfter: &MouseObservation{Screenshot: true}})
	result := r.value.(mouseObservationResult)
	if !result.InputDelivered || result.suggestedRegion == nil || *result.suggestedRegion != region || result.Observation != nil || len(r.image) != 0 || b.inputs != 1 {
		t.Fatalf("unverified menu result: %+v", result)
	}
	requireCode(t, response{err: result.err}, "not_active")
}

func TestClickReportsHitAndRechecksWindowBeforeInput(t *testing.T) {
	s, b := setupClickDesktop()
	ctx := context.Background()
	if r := observe(ctx, s, ObserveArgs{Screenshot: true}); r.err != nil {
		t.Fatal(r.err)
	}
	reportHit := true
	args := ClickArgs{SnapshotID: s.snapshot.ID, Point: &desktop.Point{X: 125, Y: 50}, ReportHit: &reportHit}
	requireCode(t, click(ctx, s, args), "unsupported_operation")
	probe := &hitDesktop{clickDesktop: b, duringHit: func() { b.windows[0].Bounds.X++ }}
	s.backend = probe
	requireCode(t, click(ctx, s, args), "stale_snapshot")
	if b.inputs != 0 {
		t.Fatal("sent input after the target changed during hit testing")
	}
	probe.duringHit = nil
	b.windows[0].Bounds.X--
	r := click(ctx, s, args)
	result, ok := r.value.(mouseObservationResult)
	if r.err != nil || !ok || !result.InputDelivered || result.Hit == nil || result.Hit.Control == nil || result.Hit.Control.Name != "Ribbon" || result.Hit.DesktopPoint != (desktop.Point{X: 150, Y: 200}) || b.inputs != 1 {
		t.Fatalf("click with hit: %+v, %v", r.value, r.err)
	}
}

func TestCoordinateClickReturnsWindowHitByDefault(t *testing.T) {
	s, b := setupClickDesktop()
	probe := &hitDesktop{clickDesktop: b, duringHit: func() { t.Fatal("queried UIA for a default hit") }}
	s.backend = probe
	ctx := context.Background()
	point := desktop.Point{X: 125, Y: 50}
	if r := observe(ctx, s, ObserveArgs{Screenshot: true}); r.err != nil {
		t.Fatal(r.err)
	}
	r := click(ctx, s, ClickArgs{SnapshotID: s.snapshot.ID, Point: &point})
	result, ok := r.value.(mouseObservationResult)
	if r.err != nil || !ok || !result.InputDelivered || result.Hit == nil || result.Hit.Control != nil ||
		result.Hit.WindowID != "w1" || result.Hit.DesktopPoint != (desktop.Point{X: 150, Y: 200}) || probe.windowHits != 1 {
		t.Fatalf("default coordinate click: %+v, %v", r.value, r.err)
	}
	if r := observe(ctx, s, ObserveArgs{Screenshot: true}); r.err != nil {
		t.Fatal(r.err)
	}
	reportHit := false
	r = click(ctx, s, ClickArgs{SnapshotID: s.snapshot.ID, Point: &point, ReportHit: &reportHit})
	if r.err != nil || probe.windowHits != 1 || b.inputs != 2 {
		t.Fatalf("opted-out coordinate click: %+v, %v", r.value, r.err)
	}
	if value, ok := r.value.(map[string]any); !ok || value["input_delivered"] != true {
		t.Fatalf("expected delivery acknowledgement only: %+v", r.value)
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
	args.ObserveAfter.Focused = true
	requireCode(t, click(ctx, s, args), "invalid_argument")
	if b.inputs != 0 || s.snapshot != old {
		t.Fatal("invalid focus option consumed the desktop snapshot")
	}
	args.ObserveAfter.Focused = false
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
	e := newEngine(ctx, func() (desktop.Backend, error) { return &hitDesktop{clickDesktop: b}, nil })
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
			reportHit := true
			return click(ctx, s, ClickArgs{SnapshotID: snapshot.ID, ElementID: "0", ReportHit: &reportHit, ObserveAfter: &MouseObservation{}})
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
		if r.err != nil || !ok || !result.InputDelivered || result.Hit == nil || result.Hit.WindowID != "w1" || result.Observation != nil || !errors.Is(result.err, context.Canceled) {
			t.Fatalf("canceled click: %+v, %v", r.value, r.err)
		}
		if r.timing == nil || r.timing.Stage != "accessibility" {
			t.Fatalf("missing follow-up stage: %+v", r.timing)
		}
	case <-ctx.Done():
		t.Fatal("canceled click still waiting for the provider")
	}
}

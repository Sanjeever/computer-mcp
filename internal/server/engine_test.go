package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
)

type fakeDesktop struct {
	windows      []desktop.Window
	nodes        []desktop.Element
	inputs       int
	input        desktop.Input
	inputOp      string
	calls        int
	err          error
	inputErr     error
	windowErr    error
	displays     []desktop.Display
	focus        desktop.Element
	activate     bool
	pointErr     error
	pointChecks  []desktop.Point
	pointFailAt  int
	captureMode  string
	ocr          []desktop.RecognizedText
	ocrSupported bool
}

func (b *fakeDesktop) Status() (desktop.Status, error) {
	return desktop.Status{Displays: b.displays, OCR: b.ocrSupported}, nil
}
func (b *fakeDesktop) Windows() ([]desktop.Window, error) { return b.windows, b.err }
func (b *fakeDesktop) Observe(context.Context, desktop.Window, desktop.ObserveOptions) ([]desktop.Element, bool, error) {
	b.calls++
	return b.nodes, false, b.err
}
func (b *fakeDesktop) Resolve(id string) (desktop.Element, error) {
	for _, n := range b.nodes {
		if n.ID == id {
			return n, nil
		}
	}
	return desktop.Element{}, desktop.Fail("stale_snapshot", id)
}
func (b *fakeDesktop) Window(w desktop.Window, a string, r desktop.Rect) error {
	if b.windowErr != nil {
		return b.windowErr
	}
	if b.activate && a == "activate" {
		for i := range b.windows {
			b.windows[i].Active = b.windows[i].ID == w.ID
		}
	}
	return b.err
}
func (b *fakeDesktop) Focus(desktop.Window) (desktop.Element, error) { return b.focus, b.err }
func (b *fakeDesktop) Input(_ context.Context, op string, in desktop.Input) error {
	b.inputs++
	b.input = in
	b.inputOp = op
	return b.inputErr
}
func (b *fakeDesktop) Capture(context.Context, desktop.Rect, int) (desktop.Capture, error) {
	b.captureMode = "screen"
	return desktop.Capture{Data: []byte("test-image"), Width: 500, Height: 250}, b.err
}
func (b *fakeDesktop) CaptureWindow(context.Context, desktop.Window, desktop.Rect, int) (desktop.Capture, error) {
	b.captureMode = "window"
	return desktop.Capture{Data: []byte("test-image"), Width: 500, Height: 250}, b.err
}
func (b *fakeDesktop) CheckPoint(_ desktop.Window, p desktop.Point, _ bool) error {
	b.pointChecks = append(b.pointChecks, p)
	if b.pointFailAt > 0 && len(b.pointChecks) == b.pointFailAt {
		return desktop.Fail("target_obscured", "intermediate point")
	}
	return b.pointErr
}
func (b *fakeDesktop) ElementClickPoint(_ context.Context, w desktop.Window, e desktop.Element) (desktop.Point, error) {
	p := e.Bounds.Center()
	if !w.Bounds.Contains(p) {
		return desktop.Point{}, desktop.Fail("stale_snapshot", "element is outside window")
	}
	return p, b.CheckPoint(w, p, false)
}
func (b *fakeDesktop) Recognize(context.Context, desktop.Capture, int) ([]desktop.RecognizedText, bool, error) {
	return b.ocr, false, b.err
}
func (b *fakeDesktop) Close() {}
func setup() (*state, *fakeDesktop) {
	b := &fakeDesktop{windows: []desktop.Window{{ID: "w1", PID: 10, Active: true, Bounds: desktop.Rect{X: -100, Y: 100, Width: 1000, Height: 500}}}, nodes: []desktop.Element{{ID: "0", Role: "button", Name: "Save", Enabled: true, Bounds: desktop.Rect{X: 100, Y: 120, Width: 40, Height: 20}}}}
	return &state{backend: b}, b
}
func requireCode(t *testing.T, r response, want string) {
	t.Helper()
	if r.err == nil {
		t.Fatalf("expected %s, got success", want)
	}
	code, _ := errorCode(r.err)
	if code != want {
		t.Fatalf("got %s: %v, want %s", code, r.err, want)
	}
}
func TestSnapshotCoordinatesAndInvalidation(t *testing.T) {
	s, b := setup()
	ctx := context.Background()
	r := observe(ctx, s, ObserveArgs{Screenshot: true})
	if r.err != nil {
		t.Fatal(r.err)
	}
	snap := s.snapshot
	p := desktop.Point{X: 250, Y: 125}
	r = click(ctx, s, ClickArgs{SnapshotID: snap.ID, Point: &p})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if b.input.Point != (desktop.Point{X: 400, Y: 350}) {
		t.Fatalf("coordinate transform: %+v", b.input.Point)
	}
	requireCode(t, click(ctx, s, ClickArgs{SnapshotID: snap.ID, Point: &p}), "stale_snapshot")
	if b.inputs != 1 {
		t.Fatalf("input repeated: %d", b.inputs)
	}
}

func TestStaleSnapshotExplainsRecovery(t *testing.T) {
	for _, reason := range []string{"desktop", "window", "consumed", "expired"} {
		t.Run(reason, func(t *testing.T) {
			s, b := setup()
			ctx := context.Background()
			if r := observe(ctx, s, ObserveArgs{Screenshot: true}); r.err != nil {
				t.Fatal(r.err)
			}
			id := s.snapshot.ID
			var want string
			switch reason {
			case "desktop":
				b.displays = []desktop.Display{{ID: "main", Bounds: desktop.Rect{Width: 1000, Height: 500}}}
				if r := observe(ctx, s, ObserveArgs{Desktop: true}); r.err != nil {
					t.Fatal(r.err)
				}
				want = "latest observation is desktop"
			case "window":
				if r := observe(ctx, s, ObserveArgs{}); r.err != nil {
					t.Fatal(r.err)
				}
				want = "latest observation is window_id=w1"
			case "consumed":
				if r := wait(ctx, s, WaitArgs{WindowID: "w1"}); r.err != nil {
					t.Fatal(r.err)
				}
				want = "no current snapshot"
			case "expired":
				s.snapshot.Created = time.Now().Add(-61 * time.Second)
				want = "60-second lifetime"
			}
			r := click(ctx, s, ClickArgs{SnapshotID: id, Point: &desktop.Point{X: 10, Y: 10}})
			requireCode(t, r, "stale_snapshot")
			_, detail := errorCode(r.err)
			if !strings.Contains(detail, want) || b.inputs != 0 {
				t.Fatalf("unsafe or unclear stale rejection: inputs=%d detail=%s", b.inputs, detail)
			}
		})
	}
}
func TestStaleElementDoesNotClick(t *testing.T) {
	for _, change := range []string{"geometry", "name", "disabled", "expired", "window", "focus", "reobserve"} {
		t.Run(change, func(t *testing.T) {
			s, b := setup()
			ctx := context.Background()
			if r := observe(ctx, s, ObserveArgs{}); r.err != nil {
				t.Fatal(r.err)
			}
			id := s.snapshot.ID
			switch change {
			case "geometry":
				b.nodes[0].Bounds.X++
			case "name":
				b.nodes[0].Name = "Delete"
			case "disabled":
				b.nodes[0].Enabled = false
			case "expired":
				s.snapshot.Created = time.Now().Add(-time.Minute * 2)
			case "window":
				b.windows[0].Bounds.X++
			case "focus":
				b.windows[0].Active = false
			case "reobserve":
				observe(ctx, s, ObserveArgs{})
			}
			r := click(ctx, s, ClickArgs{SnapshotID: id, ElementID: "0"})
			if r.err == nil {
				t.Fatal("stale click accepted")
			}
			if b.inputs != 0 {
				t.Fatal("input delivered to stale target")
			}
		})
	}
}
func TestValidationBeforeNativeInput(t *testing.T) {
	s, b := setup()
	ctx := context.Background()
	requireCode(t, key(ctx, s, KeyArgs{WindowID: "w1", Keys: []string{"ctrl", "a", "b"}}), "invalid_argument")
	requireCode(t, key(ctx, s, KeyArgs{WindowID: "w1", Keys: []string{"a", "ctrl"}}), "invalid_argument")
	requireCode(t, typeText(ctx, s, TypeArgs{WindowID: "w1", Text: "\x00"}), "invalid_argument")
	requireCode(t, click(ctx, s, ClickArgs{}), "invalid_argument")
	requireCode(t, observe(ctx, s, ObserveArgs{MaxElements: 1001}), "invalid_argument")
	requireCode(t, window(ctx, s, WindowArgs{Action: "move", WindowID: "w1"}), "invalid_argument")
	if b.inputs != 0 || b.calls != 0 {
		t.Fatal("validation reached native backend")
	}
}
func TestInputFailureIsNotRetried(t *testing.T) {
	s, b := setup()
	ctx := context.Background()
	observe(ctx, s, ObserveArgs{})
	id := s.snapshot.ID
	b.inputErr = errors.New("provider failed after dispatch")
	requireCode(t, click(ctx, s, ClickArgs{SnapshotID: id, ElementID: "0"}), "native_error")
	if b.inputs != 1 || s.snapshot != nil {
		t.Fatal("input retried or snapshot retained after uncertain failure")
	}
}

func TestRejectedClickPreservesSnapshot(t *testing.T) {
	for _, rejection := range []string{"arguments", "occlusion", "focus"} {
		t.Run(rejection, func(t *testing.T) {
			s, b := setup()
			ctx := context.Background()
			if r := observe(ctx, s, ObserveArgs{}); r.err != nil {
				t.Fatal(r.err)
			}
			snapshot := s.snapshot
			args := ClickArgs{SnapshotID: snapshot.ID, ElementID: "0"}
			wantCode := "invalid_argument"
			switch rejection {
			case "arguments":
				args.ElementID = ""
			case "occlusion":
				b.pointErr = desktop.Fail("target_obscured", "fixture cover")
				wantCode = "target_obscured"
			case "focus":
				b.windows[0].Active = false
				wantCode = "not_active"
			}
			requireCode(t, click(ctx, s, args), wantCode)
			if s.snapshot != snapshot || b.inputs != 0 {
				t.Fatal("rejection consumed the snapshot or dispatched input")
			}
			b.pointErr = nil
			b.windows[0].Active = true
			if r := click(ctx, s, ClickArgs{SnapshotID: snapshot.ID, ElementID: "0"}); r.err != nil {
				t.Fatal(r.err)
			}
			if s.snapshot != nil || b.inputs != 1 {
				t.Fatal("validated click did not consume the snapshot exactly once")
			}
		})
	}
}

func TestWaitDistinguishesTimeoutFromCancellation(t *testing.T) {
	s, _ := setup()
	r := wait(context.Background(), s, WaitArgs{WindowID: "missing", Timeout: 1})
	if r.err != nil || r.value.(map[string]any)["matched"] != false {
		t.Fatalf("timeout: %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	requireCode(t, wait(ctx, s, WaitArgs{WindowID: "w1", Timeout: 1}), "canceled")
}
func TestPasswordRedactionAndWait(t *testing.T) {
	s, b := setup()
	ctx := context.Background()
	b.nodes[0].Password = true
	b.nodes[0].Value = "secret"
	r := observe(ctx, s, ObserveArgs{})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if s.snapshot.Elements[0].Value != "" {
		t.Fatal("password leaked")
	}
	r = wait(ctx, s, WaitArgs{WindowID: "w1", Text: "Save", Timeout: 100})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !r.value.(map[string]any)["matched"].(bool) {
		t.Fatal("expected match")
	}
	if s.snapshot != nil {
		t.Fatal("wait retained invalid element references")
	}
}
func TestImageBounds(t *testing.T) {
	snap := &Snapshot{Region: desktop.Rect{Width: 100, Height: 50}, ImageWidth: 100, ImageHeight: 50}
	for _, p := range []desktop.Point{{X: -1}, {X: 100}, {Y: 50}} {
		if _, err := imagePoint(snap, p); err == nil {
			t.Fatal("out of bounds accepted")
		}
	}
}

func TestWindowSelectors(t *testing.T) {
	s, b := setup()
	b.windows[0].App, b.windows[0].AppID, b.windows[0].Title = "Editor", "org.example.editor", "Document A"
	b.windows = append(b.windows, desktop.Window{ID: "w2", App: "Editor", Title: "Document B"})
	ctx := context.Background()
	r := window(ctx, s, WindowArgs{Action: "list", ByApp: "EDITOR", ByTitle: "document"})
	if r.err != nil || len(r.value.(map[string]any)["windows"].([]desktop.Window)) != 2 {
		t.Fatalf("filtered list: %+v", r)
	}
	requireCode(t, window(ctx, s, WindowArgs{Action: "activate", ByApp: "Editor"}), "ambiguous_window")
	requireCode(t, window(ctx, s, WindowArgs{Action: "activate", ByTitle: "missing"}), "window_not_found")
	requireCode(t, window(ctx, s, WindowArgs{Action: "activate", WindowID: "w1", ByApp: "Editor"}), "invalid_argument")
	r = window(ctx, s, WindowArgs{Action: "activate", ByApp: "org.example.editor"})
	if r.err != nil || r.value.(desktop.Window).ID != "w1" {
		t.Fatalf("app id selection: %+v", r)
	}
}

func TestDesktopSnapshotWithoutWindows(t *testing.T) {
	b := &fakeDesktop{displays: []desktop.Display{{ID: "main", Bounds: desktop.Rect{Width: 1000, Height: 500}}}}
	s := &state{backend: b}
	ctx := context.Background()
	r := observe(ctx, s, ObserveArgs{Desktop: true})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if s.snapshot.Window != nil || b.calls != 0 || len(r.image) == 0 {
		t.Fatal("desktop mode accessed a window or omitted the image")
	}
	id := s.snapshot.ID
	p := desktop.Point{X: 250, Y: 125}
	r = click(ctx, s, ClickArgs{SnapshotID: id, Point: &p})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if b.input.Point != (desktop.Point{X: 500, Y: 250}) {
		t.Fatalf("desktop point: %+v", b.input.Point)
	}
	observe(ctx, s, ObserveArgs{Desktop: true})
	id = s.snapshot.ID
	b.displays[0].Bounds.X = 100
	requireCode(t, click(ctx, s, ClickArgs{SnapshotID: id, Point: &p}), "stale_snapshot")
}

func TestCloseCancelsActiveWorker(t *testing.T) {
	e := NewEngine(context.Background())
	started := make(chan struct{})
	cleaned := make(chan struct{})
	result := make(chan response, 1)
	go func() {
		result <- e.call(context.Background(), "probe", func(ctx context.Context, _ *state) response {
			close(started)
			<-ctx.Done()
			close(cleaned)
			return response{err: ctx.Err()}
		})
	}()
	select {
	case <-started:
	case r := <-result:
		t.Fatalf("worker did not start: %v", r.err)
	case <-time.After(3 * time.Second):
		t.Fatal("worker start timeout")
	}
	e.Close()
	select {
	case <-cleaned:
	default:
		t.Fatal("shutdown returned before cooperative cleanup")
	}
	requireCode(t, <-result, "canceled")
}

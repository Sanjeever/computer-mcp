package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
)

type fakeDesktop struct {
	windows []desktop.Window
	nodes   []desktop.Element
	inputs  int
	input   desktop.Input
	calls   int
	err     error
}

func (b *fakeDesktop) Status() (desktop.Status, error)    { return desktop.Status{}, nil }
func (b *fakeDesktop) Windows() ([]desktop.Window, error) { return b.windows, b.err }
func (b *fakeDesktop) Observe(context.Context, desktop.Window, int) ([]desktop.Element, bool, error) {
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
func (b *fakeDesktop) Window(w desktop.Window, a string, r desktop.Rect) error { return b.err }
func (b *fakeDesktop) Input(_ context.Context, _ string, in desktop.Input) error {
	b.inputs++
	b.input = in
	return b.err
}
func (b *fakeDesktop) Capture(context.Context, desktop.Rect, int) (desktop.Capture, error) {
	return desktop.Capture{Data: []byte("test-image"), Width: 500, Height: 250}, b.err
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
	b.err = errors.New("provider failed")
	b.windows = nil
	requireCode(t, click(ctx, s, ClickArgs{SnapshotID: id, ElementID: "0"}), "native_error")
	if b.inputs != 0 {
		t.Fatal("input sent after validation failure")
	}
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

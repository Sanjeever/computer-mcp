package server

import (
	"context"
	"slices"
	"testing"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
)

func TestObservationDiffAfterInput(t *testing.T) {
	s, b := setup()
	ctx := context.Background()
	b.nodes = append(b.nodes, desktop.Element{ID: "removed", Name: "Old"})
	if r := observe(ctx, s, ObserveArgs{}); r.err != nil {
		t.Fatal(r.err)
	}
	base := s.snapshot.ID
	if r := typeText(ctx, s, TypeArgs{WindowID: "w1", Text: "hello"}); r.err != nil {
		t.Fatal(r.err)
	}
	b.nodes = b.nodes[:1]
	b.nodes[0].Value = "new"
	b.nodes = append(b.nodes, desktop.Element{ID: "added", Name: "New"})
	r := observe(ctx, s, ObserveArgs{SinceSnapshotID: base})
	if r.err != nil {
		t.Fatal(r.err)
	}
	d := r.value.(SnapshotDiff)
	if len(d.Elements) != 2 || len(d.Removed) != 1 || d.Removed[0] != "removed" || len(d.Order) != 2 || d.ID == base || !d.Complete {
		t.Fatalf("diff: %+v", d)
	}
	r = observe(ctx, s, ObserveArgs{SinceSnapshotID: d.ID})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if len(r.value.(SnapshotDiff).Elements) != 0 || len(s.snapshot.Elements) != 2 {
		t.Fatal("unchanged elements lost from actionable snapshot")
	}
	requireCode(t, observe(ctx, s, ObserveArgs{SinceSnapshotID: base}), "stale_snapshot")
}

func TestSubtreeAndDiffScopeValidation(t *testing.T) {
	s, _ := setup()
	ctx := context.Background()
	observe(ctx, s, ObserveArgs{})
	id := s.snapshot.ID
	requireCode(t, observe(ctx, s, ObserveArgs{RootID: "missing", SnapshotID: id}), "stale_snapshot")
	requireCode(t, observe(ctx, s, ObserveArgs{RootID: "0"}), "invalid_argument")
	requireCode(t, observe(ctx, s, ObserveArgs{RootID: "0", SnapshotID: id, WindowID: "other"}), "invalid_argument")
	requireCode(t, observe(ctx, s, ObserveArgs{SinceSnapshotID: id, Text: "different scope"}), "invalid_argument")
}

func TestTruncatedDiffIsNotComplete(t *testing.T) {
	old := &Snapshot{ID: "old", Truncated: true, Elements: []desktop.Element{{ID: "gone"}}}
	d := diffSnapshots(old, &Snapshot{ID: "new"})
	if d.Complete || len(d.Removed) != 1 {
		t.Fatalf("truncated diff: %+v", d)
	}
}

func TestObservationReportsTraversalLimits(t *testing.T) {
	s, b := setup()
	b.truncation = desktop.TruncatedTimeLimit | desktop.TruncatedProviderError
	r := observe(context.Background(), s, ObserveArgs{})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !s.snapshot.Truncated || !slices.Equal(s.snapshot.TruncationReasons, []string{"time_limit", "provider_error"}) {
		t.Fatalf("truncation: %+v", s.snapshot)
	}
}

func TestWindowScreenshotOnlySkipsTreeAndKeepsGuard(t *testing.T) {
	s, b := setupClickDesktop()
	ctx := context.Background()
	region := desktop.Rect{X: 0, Y: 120, Width: 500, Height: 250}
	r := observe(ctx, s, ObserveArgs{WindowID: "w1", Region: &region, ScreenshotOnly: true})
	if r.err != nil {
		t.Fatal(r.err)
	}
	snap := s.snapshot
	if b.calls != 0 || !snap.ScreenshotOnly || snap.Truncated || snap.Region != region || len(snap.Elements) != 0 || snap.ImageWidth == 0 || len(r.image) == 0 {
		t.Fatalf("image-only observation: %+v, traversals=%d", snap, b.calls)
	}
	requireCode(t, observe(ctx, s, ObserveArgs{SinceSnapshotID: snap.ID}), "invalid_argument")
	requireCode(t, observe(ctx, s, ObserveArgs{ScreenshotOnly: true, Role: "button"}), "invalid_argument")
	point := &desktop.Point{X: 100, Y: 30}
	args := ClickArgs{SnapshotID: snap.ID, Point: point, ObserveAfter: &MouseObservation{Screenshot: true}}
	b.pointErr = desktop.Fail("target_obscured", "cover")
	requireCode(t, click(ctx, s, args), "target_obscured")
	b.pointErr = nil
	next := requireClickObservation(t, click(ctx, s, args))
	if b.calls != 0 || b.inputs != 1 || !next.ScreenshotOnly || next.Region != region || b.input.Point != (desktop.Point{X: 100, Y: 150}) {
		t.Fatalf("image-only follow-up: %+v, traversals=%d, input=%+v", next, b.calls, b.input)
	}
}

func TestCaptureModesAndOcclusionGuard(t *testing.T) {
	s, b := setup()
	ctx := context.Background()
	if r := observe(ctx, s, ObserveArgs{Screenshot: true}); r.err != nil {
		t.Fatal(r.err)
	}
	if b.captureMode != "window" || s.snapshot.CaptureMode != "window" {
		t.Fatal("window capture not selected")
	}
	b.pointErr = desktop.Fail("target_obscured", "cover")
	requireCode(t, click(ctx, s, ClickArgs{SnapshotID: s.snapshot.ID, ElementID: "0"}), "target_obscured")
	if b.inputs != 0 {
		t.Fatal("clicked through cover")
	}
	if r := observe(ctx, s, ObserveArgs{Screenshot: true, CaptureMode: "screen"}); r.err != nil {
		t.Fatal(r.err)
	}
	if b.captureMode != "screen" {
		t.Fatal("explicit screen mode ignored")
	}
	requireCode(t, observe(ctx, s, ObserveArgs{CaptureMode: "window"}), "invalid_argument")
	requireCode(t, observe(ctx, s, ObserveArgs{Desktop: true, CaptureMode: "window"}), "invalid_argument")
}

type popupDesktop struct{ *fakeDesktop }

func (b popupDesktop) ElementClickPoint(_ context.Context, w desktop.Window, e desktop.Element) (desktop.Point, error) {
	p := e.Bounds.Center()
	return p, b.CheckPoint(w, p, false)
}

func TestClickAcceptsBackendValidatedElementOutsideWindow(t *testing.T) {
	s, b := setup()
	s.backend = popupDesktop{b}
	ctx := context.Background()
	b.nodes[0].Bounds.X = b.windows[0].Bounds.X - b.nodes[0].Bounds.Width
	if r := observe(ctx, s, ObserveArgs{}); r.err != nil {
		t.Fatal(r.err)
	}
	args := ClickArgs{SnapshotID: s.snapshot.ID, ElementID: b.nodes[0].ID}
	if r := click(ctx, s, args); r.err != nil || b.inputs != 1 || b.input.Point != b.nodes[0].Bounds.Center() {
		t.Fatalf("expected one click at the validated popup point, got %d at %+v, err=%v", b.inputs, b.input.Point, r.err)
	}
	requireCode(t, click(ctx, s, args), "stale_snapshot")
}

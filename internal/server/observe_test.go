package server

import (
	"context"
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

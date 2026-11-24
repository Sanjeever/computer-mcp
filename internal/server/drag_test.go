package server

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
)

func TestDragCoordinatesAndLegacyInput(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		s, b := setup()
		ctx := context.Background()
		region := desktop.Rect{X: -50, Y: 150, Width: 800, Height: 400}
		if r := observe(ctx, s, ObserveArgs{WindowID: "w1", Screenshot: true, Region: &region}); r.err != nil {
			t.Fatal(r.err)
		}
		from, to := desktop.Point{X: 10, Y: 10}, desktop.Point{X: 400, Y: 200}
		args := DragArgs{SnapshotID: s.snapshot.ID, Path: []desktop.Point{from, {X: 200, Y: 125}, to}}
		want := []desktop.Point{{X: -34, Y: 166}, {X: 270, Y: 350}, {X: 590, Y: 470}}
		if legacy {
			args.Path = nil
			args.From = &from
			args.To = &to
			want = []desktop.Point{want[0], want[2]}
		}
		if r := drag(ctx, s, args); r.err != nil {
			t.Fatal(r.err)
		}
		if b.inputs != 1 || s.snapshot != nil || b.input.Duration != 500 || !slices.Equal(b.input.Path, want) {
			t.Fatalf("drag: inputs=%d input=%+v", b.inputs, b.input)
		}
		requireCode(t, drag(ctx, s, args), "stale_snapshot")
		if b.inputs != 1 {
			t.Fatal("drag repeated")
		}
	}
}

func TestDragRejectsInvalidPathsBeforeInput(t *testing.T) {
	p := desktop.Point{X: 10, Y: 10}
	for _, args := range []DragArgs{
		{}, {From: &p}, {To: &p}, {Path: []desktop.Point{}}, {Path: []desktop.Point{p}},
		{Path: make([]desktop.Point, 129)}, {From: &p, To: &p, Path: []desktop.Point{p, p}},
		{Path: []desktop.Point{p, {X: 500}, p}}, {Path: []desktop.Point{p, {X: math.NaN()}, p}},
		{Path: []desktop.Point{p, p}, Duration: 3001},
	} {
		s, b := setup()
		ctx := context.Background()
		if r := observe(ctx, s, ObserveArgs{Screenshot: true}); r.err != nil {
			t.Fatal(r.err)
		}
		args.SnapshotID = s.snapshot.ID
		requireCode(t, drag(ctx, s, args), "invalid_argument")
		if b.inputs != 0 {
			t.Fatal("invalid drag reached input")
		}
	}
}

func TestDragChecksIntermediateTargetsAndDoesNotRetry(t *testing.T) {
	s, b := setup()
	ctx := context.Background()
	if r := observe(ctx, s, ObserveArgs{Screenshot: true}); r.err != nil {
		t.Fatal(r.err)
	}
	args := DragArgs{SnapshotID: s.snapshot.ID, Path: []desktop.Point{{X: 10, Y: 10}, {X: 200, Y: 125}, {X: 400, Y: 200}}}
	b.pointFailAt = 2
	requireCode(t, drag(ctx, s, args), "target_obscured")
	if b.inputs != 0 || len(b.pointChecks) != 2 {
		t.Fatal("obscured intermediate target was not checked before input")
	}
	b.pointFailAt = 0
	b.inputErr = errors.New("uncertain delivery")
	requireCode(t, drag(ctx, s, args), "native_error")
	if b.inputs != 1 || s.snapshot != nil {
		t.Fatal("uncertain input retried or snapshot retained")
	}
}

func TestDragObservationChainsStrokesAndReportsCaptureFailure(t *testing.T) {
	s, b := setupClickDesktop()
	ctx := context.Background()
	region := desktop.Rect{X: -50, Y: 150, Width: 800, Height: 400}
	if r := observe(ctx, s, ObserveArgs{Screenshot: true, Region: &region, MaxWidth: 500}); r.err != nil {
		t.Fatal(r.err)
	}
	args := DragArgs{
		SnapshotID: s.snapshot.ID, Path: []desktop.Point{{X: 10, Y: 10}, {X: 400, Y: 200}},
		ObserveAfter: &MouseObservation{Screenshot: true},
	}
	first := drag(ctx, s, args)
	next, ok := first.value.(mouseObservationResult)
	if first.err != nil || !ok || !next.InputDelivered || next.Observation == nil ||
		next.Observation.ID == args.SnapshotID || next.Observation.Region != region ||
		next.Observation.maxWidth != 500 || len(first.image) == 0 {
		t.Fatalf("drag follow-up: %+v", first)
	}
	requireCode(t, drag(ctx, s, args), "stale_snapshot")
	args.SnapshotID = next.Observation.ID
	b.captureErr = desktop.Fail("native_error", "capture failed")
	second := drag(ctx, s, args)
	failed, ok := second.value.(mouseObservationResult)
	if second.err != nil || !ok || !failed.InputDelivered || failed.Observation != nil ||
		len(second.image) != 0 || s.snapshot != nil || s.previous != next.Observation || b.inputs != 2 {
		t.Fatalf("failed drag follow-up: %+v, inputs=%d", second, b.inputs)
	}
	requireCode(t, response{err: failed.err}, "native_error")
}

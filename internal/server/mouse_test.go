package server

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
)

func TestMouseHoldKeys(t *testing.T) {
	p := desktop.Point{X: 10, Y: 10}
	for _, action := range []struct {
		name string
		run  func(context.Context, *state, string, []string) response
	}{
		{"click", func(ctx context.Context, s *state, id string, keys []string) response {
			return click(ctx, s, ClickArgs{SnapshotID: id, Point: &p, HoldKeys: keys})
		}},
		{"scroll", func(ctx context.Context, s *state, id string, keys []string) response {
			return scroll(ctx, s, ScrollArgs{SnapshotID: id, Point: p, DY: 1, HoldKeys: keys})
		}},
		{"drag", func(ctx context.Context, s *state, id string, keys []string) response {
			return drag(ctx, s, DragArgs{SnapshotID: id, From: &p, To: &p, HoldKeys: keys})
		}},
	} {
		t.Run(action.name, func(t *testing.T) {
			for _, test := range []struct {
				keys    []string
				desktop bool
				panel   bool
				code    string
			}{
				{keys: []string{"ctrl", "alt", "shift", "meta", "space"}},
				{keys: []string{"shift", "shift"}, code: "invalid_argument"},
				{keys: []string{"enter"}, code: "invalid_argument"},
				{keys: []string{"Shift"}, code: "invalid_argument"},
				{keys: []string{"space"}, desktop: true, code: "invalid_argument"},
				{keys: []string{"ctrl"}, panel: true, code: "not_active"},
			} {
				s, b := setup()
				b.displays = []desktop.Display{{ID: "d1", Bounds: desktop.Rect{Width: 1000, Height: 500}}}
				ctx := context.Background()
				if r := observe(ctx, s, ObserveArgs{Desktop: test.desktop, Screenshot: true}); r.err != nil {
					t.Fatal(r.err)
				}
				id := s.snapshot.ID
				if test.panel {
					b.windows[0].Active = false
					b.windows[0].MouseActive = true
				}
				r := action.run(ctx, s, id, test.keys)
				if test.code != "" {
					requireCode(t, r, test.code)
					if b.inputs != 0 || s.snapshot == nil {
						t.Fatal("invalid held keys dispatched input or consumed snapshot")
					}
					continue
				}
				if r.err != nil || b.inputs != 1 || b.inputOp != action.name || !slices.Equal(b.input.HoldKeys, test.keys) || s.snapshot != nil {
					t.Fatalf("combined input: response=%+v input=%+v", r, b.input)
				}
				requireCode(t, action.run(ctx, s, id, test.keys), "stale_snapshot")
				if b.inputs != 1 {
					t.Fatal("consumed combined input repeated")
				}
			}
		})
	}
}

func TestMoveCoordinatesAndValidation(t *testing.T) {
	for _, test := range []struct {
		point desktop.Point
		hover int
		code  string
	}{
		{point: desktop.Point{X: 10, Y: 10}},
		{point: desktop.Point{X: -1}, code: "invalid_argument"},
		{point: desktop.Point{X: 500}, code: "invalid_argument"},
		{point: desktop.Point{Y: math.NaN()}, code: "invalid_argument"},
		{hover: -1, code: "invalid_argument"},
		{hover: 3001, code: "invalid_argument"},
	} {
		s, b := setup()
		ctx := context.Background()
		region := desktop.Rect{X: -50, Y: 150, Width: 800, Height: 400}
		if r := observe(ctx, s, ObserveArgs{Screenshot: true, Region: &region}); r.err != nil {
			t.Fatal(r.err)
		}
		id := s.snapshot.ID
		r := move(ctx, s, MoveArgs{SnapshotID: id, Point: test.point, Hover: test.hover})
		if test.code != "" {
			requireCode(t, r, test.code)
			if b.inputs != 0 || s.snapshot == nil {
				t.Fatal("invalid movement dispatched input or consumed snapshot")
			}
			continue
		}
		want := desktop.Point{X: -34, Y: 166}
		if r.err != nil || b.inputs != 1 || b.inputOp != "move" || b.input.Point != want || !slices.Equal(b.pointChecks, []desktop.Point{want}) || s.snapshot != nil {
			t.Fatalf("movement mapping: response=%+v input=%+v", r, b.input)
		}
		requireCode(t, move(ctx, s, MoveArgs{SnapshotID: id, Point: test.point}), "stale_snapshot")
	}
}

func TestMoveOcclusionAndUncertainDelivery(t *testing.T) {
	s, b := setup()
	ctx := context.Background()
	if r := observe(ctx, s, ObserveArgs{Screenshot: true}); r.err != nil {
		t.Fatal(r.err)
	}
	a := MoveArgs{SnapshotID: s.snapshot.ID, Point: desktop.Point{X: 10, Y: 10}}
	b.pointErr = desktop.Fail("target_obscured", "fixture covered")
	requireCode(t, move(ctx, s, a), "target_obscured")
	if b.inputs != 0 || s.snapshot == nil {
		t.Fatal("obscured movement dispatched input or consumed snapshot")
	}
	b.pointErr = nil
	b.inputErr = errors.New("uncertain delivery")
	requireCode(t, move(ctx, s, a), "native_error")
	if b.inputs != 1 || s.snapshot != nil {
		t.Fatal("uncertain movement retried or retained snapshot")
	}
}

func TestMoveHoverAndCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		s, b := setup()
		b.displays = []desktop.Display{{ID: "d1", Bounds: desktop.Rect{Width: 1000, Height: 500}}}
		ctx := context.Background()
		if r := observe(ctx, s, ObserveArgs{Desktop: true}); r.err != nil {
			t.Fatal(r.err)
		}
		a := MoveArgs{SnapshotID: s.snapshot.ID, Point: desktop.Point{X: 10, Y: 10}, Hover: 30}
		if canceled {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			defer cancel()
			stop := time.AfterFunc(20*time.Millisecond, cancel)
			defer stop.Stop()
			a.Hover = 3000
		}
		start := time.Now()
		r := move(ctx, s, a)
		if canceled {
			if !errors.Is(r.err, context.Canceled) {
				t.Fatalf("hover cancellation: %v", r.err)
			}
		} else if r.err != nil || time.Since(start) < 30*time.Millisecond {
			t.Fatalf("hover returned early: %+v", r)
		}
		if b.inputs != 1 || b.inputOp != "move" || s.snapshot != nil || len(b.pointChecks) != 0 {
			t.Fatal("desktop hover repeated input or retained snapshot")
		}
	}
}

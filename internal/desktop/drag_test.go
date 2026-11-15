package desktop

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestMoveDragVisitsEveryVertex(t *testing.T) {
	path := []Point{{X: -20, Y: 10}, {X: 40, Y: 50}, {X: 10, Y: -5}, {X: -20, Y: 10}}
	var visited []Point
	last, err := moveDrag(context.Background(), Input{Path: path, Duration: 100}, func(p Point) error {
		visited = append(visited, p)
		return nil
	})
	if err != nil || last != path[len(path)-1] {
		t.Fatalf("last=%v err=%v", last, err)
	}
	position := 0
	for _, vertex := range path[1:] {
		index := slices.Index(visited[position:], vertex)
		if index < 0 {
			t.Fatalf("vertex %v was skipped: %v", vertex, visited)
		}
		position += index + 1
	}
	if len(visited) <= len(path)-1 {
		t.Fatal("path was not interpolated")
	}
}

func TestMoveDragDoesNotSkipDenseVertices(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	path := make([]Point, 128)
	for i := range path {
		path[i] = Point{X: float64(i), Y: float64(i % 2)}
	}
	var visited []Point
	_, err := moveDrag(ctx, Input{Path: path, Duration: 100}, func(p Point) error { visited = append(visited, p); return nil })
	if err != nil || !slices.Equal(visited, path[1:]) {
		t.Fatalf("dense path lost vertices: len=%d err=%v", len(visited), err)
	}
}

func TestMoveDragStopsOnCancellationAndFailure(t *testing.T) {
	for _, failure := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		want := errors.New("movement delivery uncertain")
		last, err := moveDrag(ctx, Input{Path: []Point{{}, {X: 10}, {X: 20}}, Duration: 100}, func(p Point) error {
			calls++
			if failure {
				return want
			}
			cancel()
			return nil
		})
		cancel()
		if !failure {
			want = context.Canceled
		}
		if !errors.Is(err, want) || calls != 1 || last.X == 0 {
			t.Fatalf("movement retried or lost release position: last=%v calls=%d err=%v", last, calls, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := moveDrag(ctx, Input{Path: []Point{{}, {X: 10}}, Duration: 100}, func(Point) error { t.Fatal("moved after cancellation"); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

package server

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testNativeDrawingBatch(t *testing.T, ctx context.Context, fixture desktop.Window) {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "computer-mcp")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/computer-mcp")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("server build: %v\n%s", err, output)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "native-drawing-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.CommandContext(ctx, binary)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	var listed struct{ Windows []desktop.Window }
	r := batchTestCall(t, session, "computer_window", map[string]any{"action": "list", "by_title": fixture.Title}, &listed)
	var id string
	for _, window := range listed.Windows {
		if window.PID == fixture.PID {
			id = window.ID
		}
	}
	if r.IsError || id == "" {
		t.Fatalf("drawing fixture unavailable: %+v", listed)
	}
	var activated desktop.Window
	r = batchTestCall(t, session, "computer_window", map[string]any{"action": "activate", "window_id": id}, &activated)
	if r.IsError || activated.ID != id || !activated.Active {
		t.Fatalf("drawing fixture activation: %+v, content=%+v", activated, r.Content)
	}
	var window Snapshot
	r = batchTestCall(t, session, "computer_observe", map[string]any{"window_id": id}, &window)
	var region desktop.Rect
	for _, element := range window.Elements {
		if element.Name == "Fixture path target" {
			region = element.Bounds
			break
		}
	}
	if r.IsError || !region.Valid() {
		t.Fatal("fixture drawing region unavailable")
	}

	observeCanvas := func() Snapshot {
		var snap Snapshot
		r := batchTestCall(t, session, "computer_observe", map[string]any{
			"window_id": id, "region": region, "max_width": 320, "screenshot_only": true,
		}, &snap)
		if r.IsError || snap.ID == "" || snap.Region != region || snap.ImageWidth == 0 || snap.ImageHeight == 0 {
			t.Fatalf("drawing observation: %+v", snap)
		}
		requireDrawingJPEG(t, r, &snap)
		return snap
	}

	const order = "LRRLLRLR"
	const pointsPerStroke = 32
	const strokeDurationMS = 200
	source := observeCanvas()
	steps := make([]batchStepArgs, len(order))
	for i, direction := range order {
		left, right := float64(source.ImageWidth)*0.25, float64(source.ImageWidth)*0.75
		if direction == 'R' {
			left, right = right, left
		}
		path := make([]desktop.Point, pointsPerStroke)
		for j := range path {
			progress := float64(j) / float64(pointsPerStroke-1)
			path[j] = desktop.Point{X: left + (right-left)*progress, Y: float64(source.ImageHeight) * (0.25 + 0.5*float64(j%2))}
		}
		steps[i] = batchStepArgs{Op: "drag", Path: path, Duration: strokeDurationMS}
	}
	checkStrokes := func(count int) {
		var evidence struct{ Matched bool }
		want := fmt.Sprintf("strokes=%d recent=%s", count, order)
		r := batchTestCall(t, session, "computer_wait", map[string]any{"window_id": id, "text": want, "timeout_ms": 2000}, &evidence)
		if r.IsError || !evidence.Matched {
			t.Fatalf("native down/drag/up sequence did not reach %q", want)
		}
	}
	observeAfter := map[string]any{"screenshot": true}
	start := time.Now()
	current := &source
	for _, step := range steps {
		var result mouseObservationResult
		r := batchTestCall(t, session, "computer_drag", map[string]any{
			"snapshot_id": current.ID, "path": step.Path, "duration_ms": step.Duration, "observe_after": observeAfter,
		}, &result)
		if r.IsError || !result.InputDelivered || result.Observation == nil || result.Observation.ID == "" || result.Observation.ID == current.ID {
			t.Fatalf("single stroke: %+v", result)
		}
		current = result.Observation
		requireDrawingJPEG(t, r, current)
	}
	singleTime := time.Since(start)
	checkStrokes(len(steps))

	next := observeCanvas()
	if next.ImageWidth != source.ImageWidth || next.ImageHeight != source.ImageHeight {
		t.Fatalf("fixture image dimensions changed: %dx%d to %dx%d", source.ImageWidth, source.ImageHeight, next.ImageWidth, next.ImageHeight)
	}
	start = time.Now()
	var result batchTestResult
	r = batchTestCall(t, session, "computer_batch", map[string]any{
		"snapshot_id": next.ID, "steps": steps, "observe_after": observeAfter,
	}, &result)
	if r.IsError || result.Observation == nil || result.Observation.ID == "" || result.Observation.ID == next.ID ||
		result.Observation.Window == nil || result.Observation.Window.ID != id ||
		result.Observation.Region != region || !result.Observation.ScreenshotOnly || len(result.Steps) != len(steps) {
		t.Fatalf("batch: %+v", result)
	}
	for i, step := range result.Steps {
		if step.Status != "completed" || step.InputDelivery != "sent" {
			t.Fatalf("stroke %d: %+v", i, step)
		}
	}
	requireDrawingJPEG(t, r, result.Observation)
	batchTime := time.Since(start)
	checkStrokes(2 * len(steps))
	t.Logf("%d strokes, %d points, %d ms planned drag time: single stroke calls=%d images=%d elapsed=%s, batch calls=1 images=1 elapsed=%s",
		len(steps), len(steps)*pointsPerStroke, len(steps)*strokeDurationMS, len(steps), len(steps), singleTime.Round(time.Millisecond), batchTime.Round(time.Millisecond))
}

func requireDrawingJPEG(t *testing.T, r *mcp.CallToolResult, snap *Snapshot) {
	t.Helper()
	if len(r.Content) != 2 {
		t.Fatalf("expected one JPEG, got %d content blocks", len(r.Content))
	}
	image, ok := r.Content[1].(*mcp.ImageContent)
	if !ok || image.MIMEType != "image/jpeg" {
		t.Fatalf("expected JPEG content, got %T", r.Content[1])
	}
	img, err := jpeg.DecodeConfig(bytes.NewReader(image.Data))
	if err != nil || img.Width != snap.ImageWidth || img.Height != snap.ImageHeight {
		t.Fatalf("JPEG does not match snapshot: %+v, %v", img, err)
	}
}

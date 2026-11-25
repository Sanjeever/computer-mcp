package server

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
	"github.com/Sanjeever/computer-mcp/internal/i18n"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type batchTestDesktop struct {
	*fakeDesktop
	paths      [][]desktop.Point
	afterInput func(context.Context, int) error
}

func (b *batchTestDesktop) Input(ctx context.Context, op string, in desktop.Input) error {
	if op == "drag" {
		b.paths = append(b.paths, in.Path)
	}
	if err := b.fakeDesktop.Input(ctx, op, in); err != nil || b.afterInput == nil {
		return err
	}
	return b.afterInput(ctx, b.inputs)
}

func connectBatchTest(t *testing.T, b desktop.Backend) (*mcp.ClientSession, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	e := newEngine(ctx, func() (desktop.Backend, error) { return b, nil })
	t.Cleanup(e.Close)
	t.Cleanup(cancel)
	catalog, err := i18n.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	st, ct := mcp.NewInMemoryTransports()
	ss, err := New("test", catalog, e).Connect(t.Context(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, cancel
}

func batchTestCall(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) *mcp.CallToolResult {
	t.Helper()
	r, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if err := json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), out); err != nil {
		t.Fatalf("%s result: %v", name, err)
	}
	return r
}

func batchTestObserve(t *testing.T, cs *mcp.ClientSession) Snapshot {
	t.Helper()
	var snap Snapshot
	r := batchTestCall(t, cs, "computer_observe", map[string]any{
		"window_id": "w1", "screenshot_only": true,
		"region": desktop.Rect{X: -50, Y: 150, Width: 800, Height: 400},
	}, &snap)
	if r.IsError || snap.ID == "" || len(r.Content) != 2 {
		t.Fatalf("observe: %+v", r)
	}
	return snap
}

type batchTestStep struct {
	Status        string `json:"status"`
	InputDelivery string `json:"input_delivery"`
}

type batchTestResult struct {
	Steps            []batchTestStep `json:"steps"`
	StoppedAt        *int            `json:"stopped_at"`
	Failure          *toolFailure    `json:"error"`
	Observation      *Snapshot       `json:"observation"`
	ObservationError *toolFailure    `json:"observation_error"`
}

func batchTestStroke(x float64) map[string]any {
	return map[string]any{"op": "drag", "path": []desktop.Point{{X: x, Y: 10}, {X: x + 10, Y: 20}}}
}

func TestBatchSendsStrokesInOrderAndConsumesSnapshot(t *testing.T) {
	_, fake := setup()
	b := &batchTestDesktop{fakeDesktop: fake}
	cs, _ := connectBatchTest(t, b)
	snap := batchTestObserve(t, cs)
	args := map[string]any{
		"snapshot_id": snap.ID,
		"steps":       []map[string]any{batchTestStroke(10), batchTestStroke(100)},
	}
	var got batchTestResult
	r := batchTestCall(t, cs, "computer_batch", args, &got)
	want := []batchTestStep{{"completed", "sent"}, {"completed", "sent"}}
	if r.IsError || !reflect.DeepEqual(got.Steps, want) || len(r.Content) != 1 {
		t.Fatalf("batch result = %+v, content=%+v", got, r.Content)
	}
	var replay toolFailure
	r = batchTestCall(t, cs, "computer_batch", args, &replay)
	if !r.IsError || replay.Code != "stale_snapshot" {
		t.Fatalf("replay: %+v", replay)
	}
	wantPaths := [][]desktop.Point{
		{{X: -34, Y: 166}, {X: -18, Y: 182}},
		{{X: 110, Y: 166}, {X: 126, Y: 182}},
	}
	if !reflect.DeepEqual(b.paths, wantPaths) {
		t.Fatalf("native drag order or points: got %v, want %v", b.paths, wantPaths)
	}
}

func TestBatchRejectsInvalidLateStepBeforeAnyInput(t *testing.T) {
	_, fake := setup()
	cs, _ := connectBatchTest(t, fake)
	snap := batchTestObserve(t, cs)
	args := map[string]any{
		"snapshot_id": snap.ID,
		"steps":       []map[string]any{batchTestStroke(10), batchTestStroke(495)},
	}
	var got toolFailure
	r := batchTestCall(t, cs, "computer_batch", args, &got)
	if !r.IsError || got.Code != "invalid_argument" || fake.inputs != 0 || len(fake.pointChecks) != 0 {
		t.Fatalf("invalid late step: result=%+v inputs=%d checks=%v", got, fake.inputs, fake.pointChecks)
	}
	args["steps"] = []map[string]any{batchTestStroke(10), batchTestStroke(100)}
	var valid batchTestResult
	if r := batchTestCall(t, cs, "computer_batch", args, &valid); r.IsError || fake.inputs != 2 {
		t.Fatalf("preflight should leave the original snapshot usable: %+v", r)
	}
}

func TestBatchStopsAfterObscuredStroke(t *testing.T) {
	_, fake := setup()
	fake.pointFailAt = 4
	cs, _ := connectBatchTest(t, fake)
	snap := batchTestObserve(t, cs)
	var got batchTestResult
	r := batchTestCall(t, cs, "computer_batch", map[string]any{
		"snapshot_id": snap.ID,
		"steps":       []map[string]any{batchTestStroke(10), batchTestStroke(100), batchTestStroke(200)},
	}, &got)
	want := []batchTestStep{{"completed", "sent"}, {"failed", "not_sent"}, {"not_run", "not_sent"}}
	if !r.IsError || r.StructuredContent == nil || got.StoppedAt == nil || *got.StoppedAt != 1 ||
		got.Failure == nil || got.Failure.Code != "target_obscured" || !reflect.DeepEqual(got.Steps, want) || fake.inputs != 1 {
		t.Fatalf("obscured stroke: result=%+v structured=%+v inputs=%d", got, r.StructuredContent, fake.inputs)
	}
}

func TestBatchBoundsTotalWork(t *testing.T) {
	_, fake := setup()
	cs, _ := connectBatchTest(t, fake)
	snap := batchTestObserve(t, cs)
	dense := batchTestStroke(10)
	dense["path"] = make([]desktop.Point, 128)
	long := batchTestStroke(10)
	long["duration_ms"] = 3000
	many := make([]map[string]any, 9)
	for i := range many {
		many[i] = batchTestStroke(10)
	}
	for label, steps := range map[string][]map[string]any{
		"steps":    many,
		"points":   {dense, dense, batchTestStroke(10)},
		"duration": {long, long, batchTestStroke(10)},
	} {
		t.Run(label, func(t *testing.T) {
			var got toolFailure
			r := batchTestCall(t, cs, "computer_batch", map[string]any{"snapshot_id": snap.ID, "steps": steps}, &got)
			if !r.IsError || got.Code != "invalid_argument" || fake.inputs != 0 {
				t.Fatalf("batch limit: result=%+v inputs=%d", got, fake.inputs)
			}
		})
	}
}

func TestBatchReturnsOneFreshObservation(t *testing.T) {
	_, fake := setup()
	cs, _ := connectBatchTest(t, fake)
	snap := batchTestObserve(t, cs)
	args := map[string]any{
		"snapshot_id":   snap.ID,
		"steps":         []map[string]any{batchTestStroke(10), batchTestStroke(100)},
		"observe_after": map[string]any{"screenshot": true},
	}
	var got batchTestResult
	r := batchTestCall(t, cs, "computer_batch", args, &got)
	next := got.Observation
	if r.IsError || next == nil || next.Window == nil || next.ID == snap.ID || next.Window.ID != snap.Window.ID ||
		next.Region != snap.Region || next.CaptureMode != snap.CaptureMode || !next.ScreenshotOnly ||
		len(r.Content) != 2 {
		t.Fatalf("follow-up: %+v content=%+v", got, r.Content)
	}
	if image, ok := r.Content[1].(*mcp.ImageContent); !ok || image.MIMEType != "image/jpeg" || len(image.Data) == 0 {
		t.Fatalf("expected a JPEG, got %+v", r.Content[1])
	}
	args["snapshot_id"] = next.ID
	delete(args, "observe_after")
	if r := batchTestCall(t, cs, "computer_batch", args, &got); r.IsError || fake.inputs != 4 {
		t.Fatalf("next snapshot should be usable: %+v", r)
	}
}

func TestBatchCaptureErrorPreservesDeliveredStrokes(t *testing.T) {
	_, b := setupClickDesktop()
	cs, _ := connectBatchTest(t, b)
	snap := batchTestObserve(t, cs)
	b.captureErr = context.DeadlineExceeded
	var got batchTestResult
	r := batchTestCall(t, cs, "computer_batch", map[string]any{
		"snapshot_id":   snap.ID,
		"steps":         []map[string]any{batchTestStroke(10), batchTestStroke(100)},
		"observe_after": map[string]any{"screenshot": true},
	}, &got)
	want := []batchTestStep{{"completed", "sent"}, {"completed", "sent"}}
	if r.IsError || got.Observation != nil || len(r.Content) != 1 || !reflect.DeepEqual(got.Steps, want) ||
		got.ObservationError == nil || got.ObservationError.Code != "timeout" ||
		got.ObservationError.Timing == nil || got.ObservationError.Timing.Stage != "capture" {
		t.Fatalf("capture failure: result=%+v, content=%+v", got, r.Content)
	}
}

func TestBatchFollowUpDoesNotSwitchToOwnedWindow(t *testing.T) {
	_, fake := setup()
	b := &batchTestDesktop{fakeDesktop: fake, afterInput: func(context.Context, int) error {
		fake.windows[0].Active = false
		fake.windows = append(fake.windows, desktop.Window{ID: "dialog", OwnerID: "w1", PID: 10, Active: true})
		return nil
	}}
	cs, _ := connectBatchTest(t, b)
	snap := batchTestObserve(t, cs)
	var got batchTestResult
	r := batchTestCall(t, cs, "computer_batch", map[string]any{
		"snapshot_id":   snap.ID,
		"steps":         []map[string]any{batchTestStroke(10)},
		"observe_after": map[string]any{"screenshot": true},
	}, &got)
	if r.IsError || got.Observation != nil || len(r.Content) != 1 || got.ObservationError == nil ||
		got.ObservationError.Code != "stale_snapshot" || !reflect.DeepEqual(got.Steps, []batchTestStep{{"completed", "sent"}}) {
		t.Fatalf("owned window follow-up: result=%+v, content=%+v", got, r.Content)
	}
}

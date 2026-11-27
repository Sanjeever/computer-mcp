package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"time"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
	"github.com/Sanjeever/computer-mcp/internal/i18n"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type transitionDesktop struct {
	*fakeDesktop
	sample func() (*image.RGBA, error)
}

func (b *transitionDesktop) SampleScreen(_ context.Context, _ desktop.Rect, _, _ int) (desktop.ScreenSample, error) {
	img, err := b.sample()
	return desktop.ScreenSample{Image: img, CapturedAt: time.Now()}, err
}

func TestClickTransitionProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, b := setup()
	b.displays = []desktop.Display{{Bounds: b.windows[0].Bounds}}
	backend := &transitionDesktop{fakeDesktop: b}
	backend.sample = func() (*image.RGBA, error) {
		img := image.NewRGBA(image.Rect(0, 0, 20, 10))
		if b.inputs > 0 {
			img.SetRGBA(4, 6, color.RGBA{R: 255, A: 255})
		}
		return img, nil
	}
	engine := newEngine(ctx, func() (desktop.Backend, error) { return backend, nil })
	defer engine.Close()
	catalog, _ := i18n.Load("en")
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := New("test", catalog, engine).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		r, err := client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || r.IsError {
			t.Fatalf("%s: %+v, %v", name, r, err)
		}
		return r
	}
	var snap Snapshot
	if err := json.Unmarshal([]byte(call("computer_observe", nil).Content[0].(*mcp.TextContent).Text), &snap); err != nil {
		t.Fatal(err)
	}
	r := call("computer_click", map[string]any{
		"snapshot_id": snap.ID, "element_id": "0",
		"capture_transition": map[string]any{"duration_ms": 300}, "observe_after": map[string]any{"screenshot": true},
	})
	var clicked mouseObservationResult
	if err := json.Unmarshal([]byte(r.Content[0].(*mcp.TextContent).Text), &clicked); err != nil {
		t.Fatal(err)
	}
	if !clicked.InputDelivered || clicked.Observation == nil || clicked.TransitionError != nil || clicked.Transition == nil || len(r.Content) != 3 {
		t.Fatalf("click result: %+v, content count=%d", clicked, len(r.Content))
	}
	frames := clicked.Transition.Frames
	if len(frames) < 2 || frames[0].TimeMS > 0 || frames[1].TimeMS < 0 || frames[1].ChangedPixels != 1 || frames[1].ChangeBounds == nil || *frames[1].ChangeBounds != (desktop.Rect{X: 4, Y: 6, Width: 1, Height: 1}) {
		t.Fatalf("unexpected transition timeline: %+v", frames)
	}
	if following := r.Content[1].(*mcp.ImageContent); string(following.Data) != "test-image" {
		t.Fatal("expected the actionable observation image first")
	}
	imageContent := r.Content[2].(*mcp.ImageContent)
	if config, err := jpeg.DecodeConfig(bytes.NewReader(imageContent.Data)); err != nil || imageContent.MIMEType != "image/jpeg" || config.Height < 30 {
		t.Fatalf("invalid contact sheet: %+v, %v", config, err)
	}
}

func TestClickTransitionFailurePreservesDelivery(t *testing.T) {
	for _, failBeforeInput := range []bool{true, false} {
		name := "after_input"
		if failBeforeInput {
			name = "before_input"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s, b := setup()
			b.displays = []desktop.Display{{Bounds: b.windows[0].Bounds}}
			s.backend = &transitionDesktop{fakeDesktop: b, sample: func() (*image.RGBA, error) {
				if failBeforeInput || b.inputs > 0 {
					return nil, desktop.Fail("native_error", "sample unavailable")
				}
				return image.NewRGBA(image.Rect(0, 0, 12, 12)), nil
			}}
			observe(ctx, s, ObserveArgs{})
			original := s.snapshot
			r := click(ctx, s, ClickArgs{SnapshotID: original.ID, ElementID: "0", CaptureTransition: &TransitionOptions{Duration: 100}})
			if failBeforeInput {
				requireCode(t, r, "native_error")
				if s.snapshot != original || b.inputs != 0 {
					t.Fatal("failed baseline consumed snapshot or dispatched input")
				}
				return
			}
			result, ok := r.value.(mouseObservationResult)
			if r.err != nil || !ok || !result.InputDelivered || result.transitionErr == nil || result.Transition == nil ||
				len(result.Transition.Frames) != 1 || s.snapshot != nil || b.inputs != 1 {
				t.Fatalf("expected delivery receipt with partial transition, got %+v, error=%v, inputs=%d", result, r.err, b.inputs)
			}
		})
	}
}

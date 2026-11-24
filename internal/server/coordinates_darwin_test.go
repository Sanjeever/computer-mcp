package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNativeImageCoordinates(t *testing.T) {
	if os.Getenv("COMPUTER_MCP_NATIVE_TEST") != "1" {
		t.Skip("set COMPUTER_MCP_NATIVE_TEST=1 in an interactive desktop session")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	b, err := desktop.New()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	status, err := b.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Accessibility || !status.ScreenCapture {
		t.Skipf("native coordinates require Accessibility and Screen Recording (accessibility=%t, screen_capture=%t)", status.Accessibility, status.ScreenCapture)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "fixture")
	compile := exec.CommandContext(ctx, "clang", "-framework", "Cocoa", "../desktop/testdata/window.m", "-o", binary)
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("fixture build: %v\n%s", err, output)
	}
	app := exec.CommandContext(ctx, binary)
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Process.Kill(); _ = app.Wait() }()
	var target desktop.Window
	for i := 0; i < 50; i++ {
		windows, err := b.Windows()
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range windows {
			if w.PID == app.Process.Pid && w.Active {
				target = w
			}
		}
		if target.ID != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if target.ID == "" {
		t.Fatal("dedicated fixture did not become active")
	}
	s := &state{backend: b}
	for _, mode := range []string{"original", "scaled", "cropped", "moved"} {
		t.Run(mode, func(t *testing.T) {
			if r := window(ctx, s, WindowArgs{Action: "activate", WindowID: target.ID}); r.err != nil {
				t.Fatal(r.err)
			}
			if mode == "moved" {
				bounds := target.Bounds
				bounds.X += 70
				bounds.Y += 40
				bounds.Width += 50
				bounds.Height += 50
				r := window(ctx, s, WindowArgs{Action: "move", WindowID: target.ID, Bounds: &bounds})
				if r.err != nil {
					t.Fatal(r.err)
				}
			}
			if r := key(ctx, s, KeyArgs{WindowID: target.ID, Keys: []string{"meta", "a"}, FocusName: "Fixture input"}); r.err != nil {
				t.Fatal(r.err)
			}
			if r := typeText(ctx, s, TypeArgs{WindowID: target.ID, Text: "reset", FocusName: "Fixture input"}); r.err != nil {
				t.Fatal(r.err)
			}
			if r := wait(ctx, s, WaitArgs{WindowID: target.ID, Text: "reset", Timeout: 2000}); r.err != nil || r.value.(map[string]any)["matched"] != true {
				t.Fatal(r.err)
			}
			args := ObserveArgs{WindowID: target.ID, Screenshot: true, MaxWidth: 320}
			if mode == "original" {
				args.MaxWidth = 600
			}
			if mode == "cropped" {
				region := target.Bounds
				region.X += 20
				region.Y += 40
				region.Width -= 20
				region.Height -= 40
				args.Region = &region
			}
			var stages []string
			report := func(stage string) { stages = append(stages, stage) }
			r := observe(desktop.WithStageReporter(ctx, report), s, args)
			if r.err != nil {
				t.Fatal(r.err)
			}
			if want := []string{"window_list", "accessibility", "capture", "capture_shareable_content", "capture_image", "capture_encode"}; !slices.Equal(stages, want) {
				t.Fatalf("native capture stages: %v", stages)
			}
			reset := false
			for _, element := range s.snapshot.Elements {
				reset = reset || (element.Name == "Fixture input" && element.Value == "reset")
			}
			if !reset {
				t.Fatal("fixture result was not cleared before clicking")
			}
			img, err := jpeg.Decode(bytes.NewReader(r.image))
			if err != nil {
				t.Fatal(err)
			}
			var marker image.Rectangle
			for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
				for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
					r, g, b, _ := img.At(x, y).RGBA()
					if r > 56000 && g < 8000 && b > 56000 {
						marker = marker.Union(image.Rect(x, y, x+1, y+1))
					}
				}
			}
			if marker.Dx() < 8 || marker.Dy() < 8 {
				t.Fatalf("click marker missing from image: %v", marker)
			}
			point := desktop.Point{X: float64(marker.Min.X+marker.Max.X) / 2, Y: float64(marker.Min.Y+marker.Max.Y) / 2}
			source := s.snapshot
			r = click(ctx, s, ClickArgs{SnapshotID: source.ID, Point: &point, ObserveAfter: &MouseObservation{Screenshot: true}})
			next := requireClickObservation(t, r)
			nextImage, err := jpeg.DecodeConfig(bytes.NewReader(r.image))
			if err != nil || next.ID == source.ID || next.Window.ID != target.ID || next.Region != source.Region ||
				nextImage.Width != img.Bounds().Dx() || nextImage.Height != img.Bounds().Dy() {
				t.Fatalf("follow-up observation: snapshot=%+v image=%+v err=%v", next, nextImage, err)
			}
			r = wait(ctx, s, WaitArgs{WindowID: target.ID, Text: "image clicked", Timeout: 2000})
			if r.err != nil || r.value.(map[string]any)["matched"] != true {
				t.Fatalf("image point did not activate marker: %+v", r)
			}
			t.Logf("verified click at image (%g,%g), image=%dx%d", point.X, point.Y, img.Bounds().Dx(), img.Bounds().Dy())
		})
		if t.Failed() {
			return
		}
	}
	t.Run("held mouse keys", func(t *testing.T) {
		for _, held := range []bool{true, false} {
			if r := observe(ctx, s, ObserveArgs{WindowID: target.ID}); r.err != nil {
				t.Fatal(r.err)
			}
			var id string
			for _, e := range s.snapshot.Elements {
				if e.Name == "Fixture path target" {
					id = e.ID
				}
			}
			if id == "" {
				t.Fatal("path target missing")
			}
			args := ClickArgs{SnapshotID: s.snapshot.ID, ElementID: id}
			text := "image clicked"
			if held {
				args.HoldKeys = []string{"shift"}
				text = "shift clicked"
			}
			if r := click(ctx, s, args); r.err != nil {
				t.Fatal(r.err)
			}
			r := wait(ctx, s, WaitArgs{WindowID: target.ID, Text: text, Timeout: 2000})
			if r.err != nil || r.value.(map[string]any)["matched"] != true {
				t.Fatalf("held=%v: %+v", held, r)
			}
		}
	})
	if t.Failed() {
		return
	}
	t.Run("continuous path and cancellation", func(t *testing.T) {
		if r := window(ctx, s, WindowArgs{Action: "activate", WindowID: target.ID}); r.err != nil {
			t.Fatal(r.err)
		}
		if r := observe(ctx, s, ObserveArgs{WindowID: target.ID, Screenshot: true}); r.err != nil {
			t.Fatal(r.err)
		}
		var bounds desktop.Rect
		for _, e := range s.snapshot.Elements {
			if e.Name == "Fixture path target" {
				bounds = e.Bounds
			}
		}
		if !bounds.Valid() {
			t.Fatal("path target missing")
		}
		raw := []desktop.Point{
			{X: bounds.X + 5, Y: bounds.Y + 5}, {X: bounds.X + 25, Y: bounds.Y + 5},
			{X: bounds.X + 25, Y: bounds.Y + 25}, {X: bounds.X + 5, Y: bounds.Y + 25},
		}
		path := make([]desktop.Point, len(raw))
		for i, p := range raw {
			path[i] = desktop.Point{X: (p.X - s.snapshot.Region.X) * float64(s.snapshot.ImageWidth) / s.snapshot.Region.Width, Y: (p.Y - s.snapshot.Region.Y) * float64(s.snapshot.ImageHeight) / s.snapshot.Region.Height}
		}
		source := s.snapshot
		r := drag(ctx, s, DragArgs{SnapshotID: source.ID, Path: path, Duration: 600, ObserveAfter: &MouseObservation{Screenshot: true}})
		next, ok := r.value.(mouseObservationResult)
		img, err := jpeg.DecodeConfig(bytes.NewReader(r.image))
		if r.err != nil || !ok || !next.InputDelivered || next.Observation == nil || next.Observation.ID == source.ID ||
			next.Observation.Region != source.Region || err != nil || img.Width != source.ImageWidth || img.Height != source.ImageHeight {
			t.Fatalf("drag follow-up: %+v, image=%+v, decode=%v", r, img, err)
		}
		r = wait(ctx, s, WaitArgs{WindowID: target.ID, Text: "path complete", Timeout: 2000})
		if r.err != nil || r.value.(map[string]any)["matched"] != true {
			t.Fatalf("continuous path failed: %+v", r)
		}
		if r := window(ctx, s, WindowArgs{Action: "activate", WindowID: target.ID}); r.err != nil {
			t.Fatal(r.err)
		}
		cancelCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		err = b.Input(cancelCtx, "drag", desktop.Input{Path: raw, Duration: 1000})
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cancellation: %v", err)
		}
		r = wait(ctx, s, WaitArgs{WindowID: target.ID, Text: "path released", Timeout: 2000})
		if r.err != nil || r.value.(map[string]any)["matched"] != true {
			t.Fatalf("canceled drag did not release button: %+v", r)
		}
	})
	if t.Failed() {
		return
	}
	t.Run("context menu and submenu", func(t *testing.T) {
		if r := observe(ctx, s, ObserveArgs{WindowID: target.ID, Screenshot: true}); r.err != nil {
			t.Fatal(r.err)
		}
		source := s.snapshot
		var canvas desktop.Element
		for _, e := range source.Elements {
			if e.Name == "Fixture context target" {
				canvas = e
				break
			}
		}
		if canvas.ID == "" {
			t.Fatal("context target missing")
		}
		p := desktop.Point{X: canvas.Bounds.X + 120, Y: canvas.Bounds.Center().Y}
		anchor := desktop.Point{
			X: (p.X - source.Region.X) * float64(source.ImageWidth) / source.Region.Width,
			Y: (p.Y - source.Region.Y) * float64(source.ImageHeight) / source.Region.Height,
		}
		if r := click(ctx, s, ClickArgs{SnapshotID: source.ID, Point: &anchor, Button: "right"}); r.err != nil {
			t.Fatal(r.err)
		}
		region := desktop.Rect{X: p.X - 20, Y: p.Y - 30, Width: 440, Height: source.Window.Bounds.Y + source.Window.Bounds.Height - (p.Y - 30)}
		submenu := fixtureMenuPoint(t, ctx, s, target.ID, &region, "Fixture submenu")
		if r := move(ctx, s, MoveArgs{SnapshotID: s.snapshot.ID, Point: submenu, Hover: 450}); r.err != nil {
			t.Fatal(r.err)
		}
		item := fixtureMenuPoint(t, ctx, s, target.ID, &region, "Fixture submenu action")
		if r := click(ctx, s, ClickArgs{SnapshotID: s.snapshot.ID, Point: &item}); r.err != nil {
			t.Fatal(r.err)
		}
		requireFixtureValue(t, ctx, s, target.ID, "Fixture submenu action")
	})
	if t.Failed() {
		return
	}
	t.Run("floating panel", func(t *testing.T) {
		if r := observe(ctx, s, ObserveArgs{WindowID: target.ID}); r.err != nil {
			t.Fatal(r.err)
		}
		var button string
		for _, element := range s.snapshot.Elements {
			if element.Name == "Palette" {
				button = element.ID
			}
		}
		if button == "" {
			t.Fatal("palette opener missing")
		}
		if r := click(ctx, s, ClickArgs{SnapshotID: s.snapshot.ID, ElementID: button}); r.err != nil {
			t.Fatal(r.err)
		}
		var palette desktop.Window
		for range 20 {
			windows, err := b.Windows()
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range windows {
				if w.PID == app.Process.Pid && w.Title == "Computer MCP Fixture Palette" {
					palette = w
				}
			}
			if palette.ID != "" {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if palette.ID == "" {
			t.Fatal("floating palette missing from window list")
		}
		if palette.Active || !palette.MouseActive {
			t.Fatalf("non-key panel state: %+v", palette)
		}
		requireCode(t, key(ctx, s, KeyArgs{WindowID: palette.ID, Keys: []string{"space"}}), "not_active")
		if r := observe(ctx, s, ObserveArgs{WindowID: palette.ID, Screenshot: true}); r.err != nil {
			t.Fatal(r.err)
		}
		button = ""
		for _, element := range s.snapshot.Elements {
			if element.Name == "Palette action" {
				button = element.ID
			}
		}
		if button == "" {
			t.Fatal("palette action missing")
		}
		if r := click(ctx, s, ClickArgs{SnapshotID: s.snapshot.ID, ElementID: button}); r.err != nil {
			t.Fatal(r.err)
		}
		r := wait(ctx, s, WaitArgs{WindowID: target.ID, Text: "palette clicked", Timeout: 2000})
		if r.err != nil || r.value.(map[string]any)["matched"] != true {
			t.Fatalf("palette click: %+v", r)
		}
		if r := window(ctx, s, WindowArgs{Action: "activate", WindowID: palette.ID}); r.err != nil {
			t.Fatal(r.err)
		}
		if r := observe(ctx, s, ObserveArgs{WindowID: palette.ID}); r.err != nil {
			t.Fatal(r.err)
		}
		var mode desktop.Element
		for _, e := range s.snapshot.Elements {
			if e.Role == "AXPopUpButton" {
				mode = e
			}
		}
		if mode.ID == "" {
			t.Fatal("color mode control missing")
		}
		if r := click(ctx, s, ClickArgs{SnapshotID: s.snapshot.ID, ElementID: mode.ID}); r.err != nil {
			t.Fatal(r.err)
		}
		item := fixtureMenuPoint(t, ctx, s, palette.ID, nil, "Grayscale sliders")
		if r := click(ctx, s, ClickArgs{SnapshotID: s.snapshot.ID, Point: &item}); r.err != nil {
			t.Fatal(r.err)
		}
		requireFixtureValue(t, ctx, s, target.ID, "Grayscale sliders")
	})
	if t.Failed() {
		return
	}
	t.Run("stdio capture", func(t *testing.T) {
		binary := filepath.Join(t.TempDir(), "computer-mcp")
		build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/computer-mcp")
		build.Dir = "../.."
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("server build: %v\n%s", err, output)
		}
		for range 2 {
			func() {
				client := mcp.NewClient(&mcp.Implementation{Name: "native-capture-test", Version: "1"}, nil)
				session, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.CommandContext(ctx, binary)}, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "computer_window", Arguments: map[string]any{"action": "list", "by_title": target.Title}})
				if err != nil || result.IsError {
					t.Fatalf("list fixture: %v, %+v", err, result)
				}
				var listed struct {
					Windows []desktop.Window `json:"windows"`
				}
				if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &listed); err != nil {
					t.Fatal(err)
				}
				var id string
				for _, w := range listed.Windows {
					if w.PID == app.Process.Pid {
						id = w.ID
					}
				}
				if id == "" {
					t.Fatal("fixture missing from stdio server")
				}
				result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "computer_observe", Arguments: map[string]any{"window_id": id, "screenshot": true, "max_width": 320}})
				if err != nil || result.IsError {
					t.Fatalf("stdio capture: %v, %+v", err, result)
				}
				found := false
				for _, content := range result.Content {
					if img, ok := content.(*mcp.ImageContent); ok {
						if _, err := jpeg.DecodeConfig(bytes.NewReader(img.Data)); err != nil {
							t.Fatal(err)
						}
						found = true
					}
				}
				if !found {
					t.Fatal("stdio observation returned no image")
				}
			}()
		}
	})
}

func fixtureMenuPoint(t *testing.T, ctx context.Context, s *state, windowID string, region *desktop.Rect, label string) desktop.Point {
	t.Helper()
	for range 3 {
		r := observe(ctx, s, ObserveArgs{WindowID: windowID, Screenshot: true, CaptureMode: "screen", OCR: true, Region: region})
		if r.err != nil {
			t.Fatal(r.err)
		}
		for _, entry := range s.snapshot.OCR {
			if strings.EqualFold(entry.Text, label) {
				return entry.Bounds.Center()
			}
		}
	}
	t.Fatalf("menu item %q missing from fixture screenshot: %+v", label, s.snapshot.OCR)
	return desktop.Point{}
}

func requireFixtureValue(t *testing.T, ctx context.Context, s *state, windowID, value string) {
	t.Helper()
	for range 20 {
		if r := observe(ctx, s, ObserveArgs{WindowID: windowID, Text: value}); r.err != nil {
			t.Fatal(r.err)
		}
		for _, e := range s.snapshot.Elements {
			if e.Name == "Fixture input" && e.Value == value {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("fixture did not report %q", value)
}

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
			r := observe(ctx, s, args)
			if r.err != nil {
				t.Fatal(r.err)
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
			if r := click(ctx, s, ClickArgs{SnapshotID: s.snapshot.ID, Point: &point}); r.err != nil {
				t.Fatal(r.err)
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

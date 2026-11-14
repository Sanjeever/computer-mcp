package desktop

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestNativeFixture(t *testing.T) {
	if os.Getenv("COMPUTER_MCP_NATIVE_TEST") != "1" {
		t.Skip("set COMPUTER_MCP_NATIVE_TEST=1 in an interactive desktop session")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	b, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	status, err := b.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Accessibility || !status.ScreenCapture {
		t.Skipf("native fixture requires Accessibility and Screen Recording (accessibility=%t, screen_capture=%t)", status.Accessibility, status.ScreenCapture)
	}
	binary := filepath.Join(t.TempDir(), "fixture")
	compile := exec.Command("clang", "-framework", "Cocoa", "testdata/window.m", "-o", binary)
	if output, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("fixture build: %v\n%s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	app := exec.CommandContext(ctx, binary)
	if err = app.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = app.Process.Kill(); _ = app.Wait() }()
	var target Window
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		windows, err := b.Windows()
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range windows {
			if w.PID == app.Process.Pid {
				target = w
				break
			}
		}
		if target.ID != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if target.ID == "" {
		t.Fatal("fixture window not found")
	}
	if err = b.Window(target, "activate", Rect{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	windows, err := b.Windows()
	if err != nil {
		t.Fatal(err)
	}
	active := false
	for _, w := range windows {
		if w.ID == target.ID && w.Active {
			active = true
			target = w
		}
	}
	if !active {
		t.Fatal("fixture is not active; refusing to send input")
	}
	if err = b.Input(ctx, "type", Input{Text: "Hello 你好 🌍"}); err != nil {
		t.Fatal(err)
	}
	waitValue := func(want string) []Element {
		t.Helper()
		var nodes []Element
		for i := 0; i < 20; i++ {
			nodes, _, err = b.Observe(ctx, target, ObserveOptions{Limit: 100, MaxDepth: 64})
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range nodes {
				if n.Value == want {
					return nodes
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("fixture value %q not observed", want)
		return nil
	}
	nodes := waitValue("Hello 你好 🌍")
	var input Element
	for _, n := range nodes {
		if n.Name == "Fixture input" {
			input = n
		}
	}
	if input.ID == "" {
		t.Fatal("fixture input is unnamed")
	}
	focused, err := b.Focus(target)
	if err != nil || !focused.Focused || focused.Name != input.Name {
		t.Fatalf("focus: %+v, %v", focused, err)
	}
	subtree, truncated, err := b.Observe(ctx, target, ObserveOptions{Limit: 20, MaxDepth: 64, RootID: input.ID})
	if err != nil || truncated || len(subtree) != 1 || subtree[0].ID != input.ID {
		t.Fatalf("subtree identity: %+v, truncated=%v, %v", subtree, truncated, err)
	}
	nodes, _, err = b.Observe(ctx, target, ObserveOptions{Limit: 100, MaxDepth: 64})
	if err != nil {
		t.Fatal(err)
	}
	capture, err := b.Capture(ctx, target.Bounds, 600)
	if err != nil {
		t.Fatal(err)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(capture.Data))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != capture.Width || config.Height != capture.Height || config.Width > 600 {
		t.Fatal("screenshot dimensions mismatch")
	}
	var button *Element
	for i := range nodes {
		if nodes[i].Name == "Fixture submit" {
			button = &nodes[i]
			break
		}
	}
	if button == nil {
		t.Fatal("fixture button missing")
	}
	if err = b.Input(ctx, "click", Input{Point: button.Bounds.Center(), Button: "left", Count: 1}); err != nil {
		t.Fatal(err)
	}
	nodes = waitValue("submitted")
	windows, err = b.Windows()
	if err != nil {
		t.Fatal(err)
	}
	stable := false
	for _, w := range windows {
		if w.ID == target.ID && w.Title == "Computer MCP Native Fixture Updated" {
			stable = true
			target = w
		}
	}
	if !stable {
		t.Fatal("window ID changed after title update")
	}
	for _, n := range nodes {
		if n.Name == "Fixture cover" {
			if err := b.CheckPoint(target, n.Bounds.Center()); err != nil {
				t.Fatal(err)
			}
			if err := b.Input(ctx, "click", Input{Point: n.Bounds.Center(), Button: "left", Count: 1}); err != nil {
				t.Fatal(err)
			}
		}
	}
	var cover Window
	for i := 0; i < 20; i++ {
		windows, err = b.Windows()
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range windows {
			if w.PID == target.PID && w.Title == "Computer MCP Fixture Cover" {
				cover = w
			}
		}
		if cover.ID != "" && cover.Active {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !cover.Active {
		t.Fatal("dedicated cover did not become active")
	}
	if err := b.CheckPoint(target, target.Bounds.Center()); err == nil {
		t.Fatal("obscured mouse target accepted")
	}
	windowCapture, err := b.CaptureWindow(ctx, target, target.Bounds, 600)
	if err != nil {
		t.Fatal(err)
	}
	windowImage, err := jpeg.Decode(bytes.NewReader(windowCapture.Data))
	if err != nil {
		t.Fatal(err)
	}
	screenCapture, err := b.Capture(ctx, target.Bounds, 600)
	if err != nil {
		t.Fatal(err)
	}
	screenImage, err := jpeg.Decode(bytes.NewReader(screenCapture.Data))
	if err != nil {
		t.Fatal(err)
	}
	red := func(img image.Image) bool {
		r, g, blue, _ := img.At(550, 250).RGBA()
		return r > 50000 && g < 10000 && blue < 10000
	}
	if red(windowImage) || !red(screenImage) {
		t.Fatal("window capture included cover or screen capture omitted it")
	}
	crop := Rect{X: target.Bounds.X + 300, Y: target.Bounds.Y + 100, Width: 200, Height: 150}
	cropped, err := b.CaptureWindow(ctx, target, crop, 600)
	if err != nil || cropped.Width != 200 || cropped.Height != 150 {
		t.Fatalf("window crop: %dx%d, %v", cropped.Width, cropped.Height, err)
	}
	t.Logf("native fixture passed: %d elements, JPEG %dx%d (%d bytes), Unicode input and click verified", len(nodes), capture.Width, capture.Height, len(capture.Data))
}

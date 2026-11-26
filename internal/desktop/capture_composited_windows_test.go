package desktop

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"image/jpeg"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	w "github.com/zzl/go-win32api/v2/win32"
)

//go:embed testdata/windows-capture-composited.ps1
var windowsCompositedFixture string

func TestNativeWindowsCompositedCapture(t *testing.T) {
	if os.Getenv("COMPUTER_MCP_NATIVE_TEST") != "1" {
		t.Skip("set COMPUTER_MCP_NATIVE_TEST=1 in an interactive desktop session")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	b, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	app := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-Command", "-")
	app.Stdin = strings.NewReader(windowsCompositedFixture + "\n\n")
	app.Stderr = os.Stderr
	stdout, err := app.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Process.Kill(); _ = app.Wait() })
	var handle uintptr
	var renderTier int
	if _, err := fmt.Fscan(bufio.NewReader(stdout), &handle, &renderTier); err != nil {
		t.Fatalf("composited fixture did not report its window: %v", err)
	}
	h := w.HWND(handle)
	t.Logf("fixture_wpf_render_tier=%d fixture_dpi=%d", renderTier, w.GetDpiForWindow(h))

	id := windowID(h)
	windows, err := b.Windows()
	if err != nil {
		t.Fatal(err)
	}
	var target Window
	for _, window := range windows {
		if window.ID == id {
			target = window
			break
		}
	}
	if target.ID == "" {
		t.Fatal("dedicated composited fixture is missing from the native window list")
	}
	var client w.RECT
	var origin w.POINT
	if ok, code := w.GetClientRect(h, &client); ok == 0 {
		t.Fatal(code)
	}
	if w.ClientToScreen(h, &origin) == 0 {
		t.Fatal("cannot locate the composited fixture client area")
	}
	region := Rect{X: float64(origin.X), Y: float64(origin.Y), Width: float64(client.Right), Height: float64(client.Bottom)}
	for _, fraction := range []float64{0.25, 0.75} {
		point := Point{X: region.X + region.Width*fraction, Y: region.Center().Y}
		if err := b.CheckPoint(target, point, true); err != nil {
			t.Fatalf("composited fixture is covered at a sample point: %v", err)
		}
	}
	// ContentRendered can precede the final desktop composition of a new window.
	// Wait for the visible reference before comparing window capture.
	deadline := time.Now().Add(2 * time.Second)
	for {
		screen, err := b.Capture(ctx, region, 320)
		if err != nil {
			t.Fatalf("screen capture: %v", err)
		}
		if err := checkCompositedCapture(screen); err == nil {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("composited fixture did not settle on screen: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	window, err := b.CaptureWindow(ctx, target, region, 320)
	if err != nil {
		t.Fatalf("window capture: %v", err)
	}
	if err := checkCompositedCapture(window); err != nil {
		t.Fatalf("window capture: %v", err)
	}
}

func checkCompositedCapture(capture Capture) error {
	img, err := jpeg.Decode(bytes.NewReader(capture.Data))
	if err != nil {
		return err
	}
	y := img.Bounds().Dy() / 2
	left := img.At(img.Bounds().Dx()/4, y)
	right := img.At(3*img.Bounds().Dx()/4, y)
	lr, lg, lb, _ := left.RGBA()
	rr, rg, rb, _ := right.RGBA()
	if lr < 0xd000 || lg > 0x3000 || lb > 0x3000 || rr > 0x3000 || rg > 0x3000 || rb < 0xd000 {
		return fmt.Errorf("missing red and blue fixture panels: left=%v right=%v", left, right)
	}
	return nil
}

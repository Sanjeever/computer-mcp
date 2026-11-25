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
	"strings"
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
	if err != nil || truncated != 0 || len(subtree) != 1 || subtree[0].ID != input.ID {
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
	rows, _, err := b.Observe(ctx, target, ObserveOptions{Limit: 10, MaxDepth: 64, Text: "File 000.txt"})
	if err != nil {
		t.Fatal(err)
	}
	foundRow := false
	for _, n := range rows {
		if n.Role == "AXRow" && strings.Contains(n.Name, "File 000.txt") {
			foundRow = true
		}
	}
	if !foundRow {
		t.Fatalf("list row text was not extracted: %+v", rows)
	}
	clickNamed := func(name string) {
		t.Helper()
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
			t.Fatal("fixture lost focus before test input")
		}
		nodes, _, err := b.Observe(ctx, target, ObserveOptions{Limit: 100, MaxDepth: 64})
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range nodes {
			if n.Name == name {
				if err := b.CheckPoint(target, n.Bounds.Center(), false); err != nil {
					t.Fatal(err)
				}
				if err := b.Input(ctx, "click", Input{Point: n.Bounds.Center(), Button: "left", Count: 1}); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatalf("fixture button %q missing", name)
	}
	t.Run("multiline editing", func(t *testing.T) {
		original := target
		defer func() {
			target = original
			if err := b.Window(target, "activate", Rect{}); err != nil {
				t.Error(err)
			}
		}()
		clickNamed("Editor")
		for i := 0; i < 40; i++ {
			windows, err := b.Windows()
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range windows {
				if w.PID == original.PID && w.Title == "Computer MCP Fixture Editor" && w.Active {
					target = w
				}
			}
			if target.ID != original.ID {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if target.ID == original.ID {
			t.Fatal("editor did not become active")
		}
		input := func(op string, in Input) {
			t.Helper()
			windows, err := b.Windows()
			if err != nil {
				t.Fatal(err)
			}
			active := false
			for _, w := range windows {
				active = active || (w.ID == target.ID && w.Active)
			}
			if !active {
				t.Fatal("editor lost focus; refusing input")
			}
			if err := b.Input(ctx, op, in); err != nil {
				t.Fatal(err)
			}
		}
		check := func(want string) {
			t.Helper()
			var got string
			for i := 0; i < 20; i++ {
				nodes, _, err := b.Observe(ctx, target, ObserveOptions{Limit: 100, MaxDepth: 64})
				if err != nil {
					t.Fatal(err)
				}
				for _, n := range nodes {
					if n.Name == "Fixture editor" {
						got = n.Value
						if got == want {
							return
						}
					}
				}
				time.Sleep(50 * time.Millisecond)
			}
			t.Fatalf("editor value\n got: %q\nwant: %q", got, want)
		}
		const document = "周末出行清单\n\n证件\n· 身份证\n· 护照\n· 驾驶证\n· 社保卡\n· 学生证\n\n衣物\n· 短袖 T 恤\n· 长裤\n· 外套\n· 内衣\n· 袜子\n· 运动鞋\n\n电子设备\n· 手机\n· 充电器\n· 充电宝\n· 移动电源\n· 笔记本电脑\n· 相机\n· 耳机"
		for i := 0; i < 3; i++ {
			if i > 0 {
				input("key", Input{Keys: []string{"meta", "a"}})
			}
			input("type", Input{Text: document})
			check(document)
			input("key", Input{Keys: []string{"meta", "up"}})
			input("type", Input{Text: "衣物\n"})
			check("衣物\n" + document)
			input("type", Input{Text: "12"})
			check("衣物\n12" + document)
			input("key", Input{Keys: []string{"shift", "right"}})
			input("type", Input{Text: "✅"})
			check("衣物\n12✅" + strings.TrimPrefix(document, "周"))
		}
		for _, value := range []string{
			"\n开头\n\n结尾\n",
			"开始\t中间\t结束",
			strings.Repeat("😀", 24) + " e\u0301 ✅",
		} {
			input("key", Input{Keys: []string{"meta", "a"}})
			input("type", Input{Text: value})
			check(value)
		}
	})
	if t.Failed() {
		return
	}
	clickNamed("Cycle")
	missing, restored := false, false
	for i := 0; i < 40; i++ {
		windows, err = b.Windows()
		if err != nil {
			t.Fatal(err)
		}
		present := false
		for _, w := range windows {
			if w.ID == target.ID {
				present = true
				target = w
			}
		}
		if !present {
			missing = true
		}
		if missing && present && target.Active {
			restored = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !restored {
		for _, w := range windows {
			if w.PID == target.PID {
				t.Logf("fixture after cycle: id=%s active=%v, expected=%s missing=%v", w.ID, w.Active, target.ID, missing)
			}
		}
		t.Fatal("window identity did not survive temporary disappearance")
	}
	clickNamed("Modal")
	blocked, unblocked := false, false
	for i := 0; i < 50; i++ {
		windows, err = b.Windows()
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range windows {
			if w.ID != target.ID {
				continue
			}
			target = w
			if w.BlockedBy != "" {
				if !blocked {
					if err := b.Window(w, "activate", Rect{}); err == nil {
						t.Fatal("activation accepted through modal sheet")
					}
				}
				blocked = true
			} else if blocked && w.Active {
				unblocked = true
			}
		}
		if unblocked {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !unblocked {
		t.Fatal("modal blocker was not detected and cleared")
	}
	nodes, _, err = b.Observe(ctx, target, ObserveOptions{Limit: 100, MaxDepth: 64})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Name == "Fixture cover" {
			if err := b.CheckPoint(target, n.Bounds.Center(), false); err != nil {
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
	if err := b.CheckPoint(target, target.Bounds.Center(), true); err == nil || !strings.Contains(err.Error(), "hit_window_id="+cover.ID) {
		t.Fatalf("obscured mouse target: %v", err)
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
	ocr, ocrTruncated, err := b.Recognize(ctx, windowCapture, 200)
	if err != nil || ocrTruncated {
		t.Fatalf("OCR failed: truncated=%v, %v", ocrTruncated, err)
	}
	foundCanvas := false
	for _, region := range ocr {
		if strings.Contains(region.Text, "Canvas text") && strings.Contains(region.Text, "识别") {
			foundCanvas = true
			if region.Bounds.Y < 200 || region.Bounds.Y > 300 {
				t.Fatalf("OCR coordinates not top-left pixels: %+v", region.Bounds)
			}
		}
	}
	if !foundCanvas {
		t.Fatalf("custom-drawn bilingual text missing from OCR: %+v", ocr)
	}
	crop := Rect{X: target.Bounds.X + 300, Y: target.Bounds.Y + 100, Width: 200, Height: 150}
	cropped, err := b.CaptureWindow(ctx, target, crop, 600)
	if err != nil || cropped.Width != 200 || cropped.Height != 150 {
		t.Fatalf("window crop: %dx%d, %v", cropped.Width, cropped.Height, err)
	}
	t.Logf("native fixture passed: %d elements, JPEG %dx%d (%d bytes), Unicode input and click verified", len(nodes), capture.Width, capture.Height, len(capture.Data))
}

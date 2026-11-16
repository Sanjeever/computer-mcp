package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	w "github.com/zzl/go-win32api/v2/win32"
)

type windowsFixture struct {
	Window Window
	Editor w.HWND
}

func TestWindowsFixtureProcess(t *testing.T) {
	if os.Getenv("COMPUTER_MCP_WINDOWS_FIXTURE") != "1" {
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	w.SetProcessDpiAwarenessContext(w.DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2)
	editorClass := "EDIT"
	if os.Getenv("COMPUTER_MCP_WINDOWS_RICHEDIT") == "1" {
		library, err := syscall.LoadDLL("Msftedit.dll")
		if err != nil {
			t.Fatal(err)
		}
		defer library.Release()
		editorClass = "RICHEDIT50W"
	}
	root, code := w.CreateWindowExW(0, w.StrToPwstr("STATIC"), w.StrToPwstr("Computer MCP Windows fixture"), w.WS_OVERLAPPEDWINDOW|w.WS_VISIBLE, 100, 100, 1155, 700, 0, 0, 0, nil)
	if root == 0 {
		t.Fatal(code)
	}
	defer w.DestroyWindow(root)
	editor, code := w.CreateWindowExW(w.WS_EX_CLIENTEDGE, w.StrToPwstr(editorClass), w.StrToPwstr(""), w.WS_CHILD|w.WS_VISIBLE|w.WS_VSCROLL|w.WINDOW_STYLE(w.ES_MULTILINE|w.ES_AUTOVSCROLL|w.ES_WANTRETURN), 12, 12, 1100, 500, root, 0, 0, nil)
	if editor == 0 {
		t.Fatal(code)
	}
	w.SetForegroundWindow(root)
	w.SetFocus(editor)
	deadline := time.Now().Add(2 * time.Second)
	var message w.MSG
	for w.GetForegroundWindow() != root && time.Now().Before(deadline) {
		for w.PeekMessageW(&message, 0, 0, 0, w.PM_REMOVE) != 0 {
			w.TranslateMessage(&message)
			w.DispatchMessageW(&message)
		}
		w.SetForegroundWindow(root)
		time.Sleep(20 * time.Millisecond)
	}
	if w.GetForegroundWindow() == root {
		w.SetFocus(editor)
		w.AllowSetForegroundWindow(uint32(os.Getppid()))
	}
	var rect w.RECT
	w.GetWindowRect(root, &rect)
	fixture := windowsFixture{Window: Window{ID: strconv.FormatUint(uint64(root), 10), PID: os.Getpid(), Title: "Computer MCP Windows fixture", Bounds: winRect(rect)}, Editor: editor}
	if err := json.NewEncoder(os.Stdout).Encode(fixture); err != nil {
		t.Fatal(err)
	}
	for {
		ok, err := w.GetMessageW(&message, 0, 0, 0)
		if ok == -1 {
			t.Fatal(err)
		}
		if ok == 0 {
			return
		}
		w.TranslateMessage(&message)
		w.DispatchMessageW(&message)
	}
}

func startWindowsFixture(t *testing.T) (Backend, windowsFixture) {
	t.Helper()
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	app := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWindowsFixtureProcess$")
	app.Env = append(os.Environ(), "COMPUTER_MCP_WINDOWS_FIXTURE=1")
	app.Stderr = os.Stderr
	stdout, err := app.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Process.Kill(); _ = app.Wait() })
	var fixture windowsFixture
	if err = json.NewDecoder(bufio.NewReader(stdout)).Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	t.Logf("fixture=%s foreground=%d", fixture.Window.ID, w.GetForegroundWindow())
	root, err := hwnd(fixture.Window.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.GetForegroundWindow() != root {
		t.Skip("Windows denied foreground access to the dedicated fixture; no input sent")
	}
	if err = b.Window(fixture.Window, "activate", Rect{}); err != nil {
		t.Fatal(err)
	}
	return b, fixture
}

func fixtureMessage(t *testing.T, editor w.HWND, message uint32, param w.WPARAM, data w.LPARAM) uintptr {
	t.Helper()
	var result uintptr
	if ok, code := w.SendMessageTimeoutW(editor, message, param, data, w.SMTO_ABORTIFHUNG, 1000, &result); ok == 0 {
		t.Fatal(code)
	}
	return result
}

func fixtureText(t *testing.T, editor w.HWND) string {
	t.Helper()
	buf := make([]uint16, 8192)
	n := fixtureMessage(t, editor, w.WM_GETTEXT, w.WPARAM(len(buf)), w.LPARAM(uintptr(unsafe.Pointer(&buf[0]))))
	return syscall.UTF16ToString(buf[:n])
}

func requireFixtureActive(t *testing.T, fixture windowsFixture) {
	t.Helper()
	root, err := hwnd(fixture.Window.ID)
	if err != nil || w.GetForegroundWindow() != root {
		t.Fatal("dedicated fixture is not active; refusing to send input")
	}
}

func TestNativeWindowsText(t *testing.T) {
	testWindowsText(t)
}

func TestNativeWindowsRichText(t *testing.T) {
	t.Setenv("COMPUTER_MCP_WINDOWS_RICHEDIT", "1")
	testWindowsText(t)
}

func testWindowsText(t *testing.T) {
	b, fixture := startWindowsFixture(t)
	ctx := context.Background()
	for _, test := range []struct{ name, text, want string }{
		{"unicode", "Hello 你好 🌍 ± § © ® ™ ✓", "Hello 你好 🌍 ± § © ® ™ ✓"},
		{"lf", "A\nB", "A\r\nB"},
		{"crlf", "A\r\nB", "A\r\nB"},
		{"cr", "A\rB", "A\r\nB"},
		{"batch_boundary", strings.Repeat("x", 63) + "\r\n🌍end", strings.Repeat("x", 63) + "\r\n🌍end"},
		{"mixed", strings.Repeat("中文 English 0123 !@#$%^&*() ± ✓\n", 6), strings.Repeat("中文 English 0123 !@#$%^&*() ± ✓\r\n", 6)},
		{"reported_mixed", "替换后的第二版内容\nREPLACED v2: abc XYZ 987\n符号混排 {[(<&$#>)]} ~ ^ | \\ / @ % *\n中文标点：你好，世界！「测试」\n保存并重开验证。\nend of test.", "替换后的第二版内容\r\nREPLACED v2: abc XYZ 987\r\n符号混排 {[(<&$#>)]} ~ ^ | \\ / @ % *\r\n中文标点：你好，世界！「测试」\r\n保存并重开验证。\r\nend of test."},
	} {
		t.Log(test.name)
		requireFixtureActive(t, fixture)
		fixtureMessage(t, fixture.Editor, w.WM_SETTEXT, 0, w.LPARAM(uintptr(unsafe.Pointer(w.StrToPwstr("")))))
		if err := b.Input(ctx, "type", Input{Text: test.text}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		var got string
		for time.Now().Before(deadline) {
			got = fixtureText(t, fixture.Editor)
			if got == test.want {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if got != test.want {
			t.Errorf("%s text mismatch: got %q, want %q", test.name, got, test.want)
		}
	}
}

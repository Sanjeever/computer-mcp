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
	Window     Window
	Editor     w.HWND
	InactiveID string
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
	if os.Getenv("COMPUTER_MCP_WINDOWS_ACTIVATION_TEST") == "1" && w.GetForegroundWindow() == root {
		other, code := w.CreateWindowExW(0, w.StrToPwstr("STATIC"), w.StrToPwstr("Computer MCP inactive fixture"), w.WS_OVERLAPPEDWINDOW, 300, 300, 400, 200, 0, 0, 0, nil)
		if other == 0 {
			t.Fatal(code)
		}
		defer w.DestroyWindow(other)
		w.ShowWindow(other, w.SW_SHOWNOACTIVATE)
		fixture.InactiveID = strconv.FormatUint(uint64(other), 10)
		if ok, code := w.LockSetForegroundWindow(w.LSFW_LOCK); ok == 0 {
			t.Fatal(code)
		}
		defer w.LockSetForegroundWindow(w.LSFW_UNLOCK)
	}
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
	t.Logf("fixture_dpi=%d", w.GetDpiForWindow(root))
	if w.GetForegroundWindow() != root {
		if ok, code := w.SetWindowPos(root, w.HWND_TOPMOST, 0, 0, 0, 0, w.SWP_NOMOVE|w.SWP_NOSIZE|w.SWP_NOACTIVATE); ok == 0 {
			t.Fatal(code)
		}
		var rect w.RECT
		if ok, code := w.GetWindowRect(fixture.Editor, &rect); ok == 0 {
			t.Fatal(code)
		}
		point := winRect(rect).Center()
		if err := b.CheckPoint(fixture.Window, point); err != nil {
			t.Fatalf("fixture setup click rejected: %v", err)
		}
		// Explicit test setup touches only the verified fixture, never another application.
		if err := b.Input(ctx, "click", Input{Point: point, Button: "left", Count: 1}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Second)
		for w.GetForegroundWindow() != root && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		requireFixtureActive(t, fixture)
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

func TestNativeWindowsActivation(t *testing.T) {
	t.Setenv("COMPUTER_MCP_WINDOWS_ACTIVATION_TEST", "1")
	b, fixture := startWindowsFixture(t)
	requireFixtureActive(t, fixture)
	if err := b.Window(fixture.Window, "activate", Rect{}); err != nil {
		t.Fatalf("already active target: %v", err)
	}
	if fixture.InactiveID == "" {
		t.Fatal("missing inactive fixture window")
	}
	err := b.Window(Window{ID: fixture.InactiveID}, "activate", Rect{})
	if err != nil {
		native, ok := err.(*Error)
		if !ok || native.Code != "activation_failed" || !strings.Contains(native.Detail, "target_window_id="+fixture.InactiveID) || !strings.Contains(native.Detail, "foreground_window_id="+fixture.Window.ID) {
			t.Fatalf("foreground denial: %v", err)
		}
		requireFixtureActive(t, fixture)
	} else {
		target, err := hwnd(fixture.InactiveID)
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Second)
		for w.GetForegroundWindow() != target && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if w.GetForegroundWindow() == target {
			t.Log("Windows permitted activation despite the fixture's foreground lock; native denial was not exercised")
		} else {
			requireFixtureActive(t, fixture)
			t.Log("Windows accepted the native activation request without transferring foreground; the server must confirm activation")
		}
	}
	if got := fixtureText(t, fixture.Editor); got != "" {
		t.Fatalf("activation sent text: %q", got)
	}
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
		empty := w.StrToPwstr("")
		fixtureMessage(t, fixture.Editor, w.WM_SETTEXT, 0, w.LPARAM(uintptr(unsafe.Pointer(empty))))
		runtime.KeepAlive(empty)
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
		if test.name == "reported_mixed" {
			wantValue := test.want
			if os.Getenv("COMPUTER_MCP_WINDOWS_RICHEDIT") == "1" {
				// RichEdit's UIA value uses CR paragraphs and includes the final paragraph mark.
				wantValue = strings.ReplaceAll(test.want, "\r\n", "\r") + "\r"
			}
			nodes, truncated, err := b.Observe(ctx, fixture.Window, ObserveOptions{Limit: 100, MaxDepth: 64})
			if err != nil || truncated {
				t.Fatalf("fixture observation: truncated=%t, %v", truncated, err)
			}
			found := false
			for _, node := range nodes {
				if node.Value == wantValue {
					found = true
				}
			}
			if !found {
				t.Fatalf("typed text missing from UIA values: %+v", nodes)
			}
			focus, err := b.Focus(fixture.Window)
			if err != nil || !focus.Focused {
				t.Fatalf("fixture focus: %+v, %v", focus, err)
			}
		}
	}
}

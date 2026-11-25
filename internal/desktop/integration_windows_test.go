package desktop

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	w "github.com/zzl/go-win32api/v2/win32"
)

const fixtureDialogTitle = "Computer MCP owned fixture dialog"
const fixtureMenuItem = "Select fixture menu item"

type windowsFixture struct {
	Window      Window
	Editor      w.HWND
	Canvas      w.HWND
	InactiveID  string
	Popup       w.HWND
	PopupCover  w.HWND
	WindowCover w.HWND
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
	dialogTest := os.Getenv("COMPUTER_MCP_WINDOWS_DIALOG_TEST") == "1"
	editorTop, editorHeight := int32(12), int32(500)
	if dialogTest {
		editorTop, editorHeight = 72, 440
	}
	editor, code := w.CreateWindowExW(w.WS_EX_CLIENTEDGE, w.StrToPwstr(editorClass), w.StrToPwstr(""), w.WS_CHILD|w.WS_VISIBLE|w.WS_VSCROLL|w.WINDOW_STYLE(w.ES_MULTILINE|w.ES_AUTOVSCROLL|w.ES_WANTRETURN), 12, editorTop, 1100, editorHeight, root, 0, 0, nil)
	if editor == 0 {
		t.Fatal(code)
	}
	canvas, code := w.CreateWindowExW(0, w.StrToPwstr("STATIC"), w.StrToPwstr(""), w.WS_CHILD|w.WS_VISIBLE|w.WINDOW_STYLE(w.SS_NOTIFY), 12, 530, 1100, 100, root, 0, 0, nil)
	if canvas == 0 {
		t.Fatal(code)
	}
	var openDialog w.HWND
	if dialogTest {
		openDialog, code = w.CreateWindowExW(0, w.StrToPwstr("BUTTON"), w.StrToPwstr("Open fixture dialog"), w.WS_CHILD|w.WS_VISIBLE|w.WS_TABSTOP, 20, 20, 160, 36, root, 0, 0, nil)
		if openDialog == 0 {
			t.Fatal(code)
		}
	}
	fixture := windowsFixture{Editor: editor, Canvas: canvas}
	targets := []w.HWND{canvas, root, editor}
	var popupButton w.HWND
	if os.Getenv("COMPUTER_MCP_WINDOWS_POPUP_TEST") == "1" {
		popupButton = createWindowsPopupFixture(t, root, &fixture)
		targets = append(targets, fixture.Popup)
	}
	var mouseEvents []string
	for _, target := range targets {
		var original uintptr
		procedure := syscall.NewCallback(func(h w.HWND, message uint32, param w.WPARAM, data w.LPARAM) w.LRESULT {
			if openDialog != 0 && h == root && message == w.WM_COMMAND && w.HWND(data) == openDialog && uint32(param)>>16 == w.BN_CLICKED {
				w.MessageBoxW(root, w.StrToPwstr("Close this fixture dialog to return to the main window."), w.StrToPwstr(fixtureDialogTitle), w.MB_OK)
				return 0
			}
			if dialogTest && h == editor && message == w.WM_CONTEXTMENU {
				menu, _ := w.CreatePopupMenu()
				if menu == 0 {
					return 0
				}
				defer w.DestroyMenu(menu)
				w.AppendMenuW(menu, w.MF_STRING, 1, w.StrToPwstr(fixtureMenuItem))
				if selected, _ := w.TrackPopupMenu(menu, w.TPM_RETURNCMD|w.TPM_NONOTIFY, int32(int16(data)), int32(int16(data>>16)), 0, root, nil); selected == 1 {
					w.SetWindowTextW(editor, w.StrToPwstr("menu clicked"))
				}
				return 0
			}
			if popupButton != 0 && h == fixture.Popup && message == w.WM_COMMAND && w.HWND(data) == popupButton && uint32(param)>>16 == w.BN_CLICKED {
				w.SetWindowTextW(editor, w.StrToPwstr("popup clicked"))
				w.DestroyWindow(h)
				return 0
			}
			if h == canvas && message == w.WM_APP {
				mouseEvents = nil
				w.SetWindowTextW(canvas, w.StrToPwstr(""))
				return 0
			}
			kind := ""
			switch message {
			case w.WM_MOUSEMOVE:
				kind = "move"
			case w.WM_LBUTTONDOWN:
				w.SetFocus(h)
				kind = "down"
			case w.WM_LBUTTONUP:
				kind = "up"
			case w.WM_MOUSEWHEEL, w.WM_MOUSEHWHEEL:
				kind = "scroll"
			}
			if kind != "" {
				mouseEvents = append(mouseEvents, fmt.Sprintf("%s:%d:%t", kind, uint32(param)&0xffff, w.GetKeyState(int32(w.VK_SPACE)) < 0))
				w.SetWindowTextW(canvas, w.StrToPwstr(strings.Join(mouseEvents, "|")))
			}
			return w.CallWindowProcW(original, h, message, param, data)
		})
		original, code = w.SetWindowLongPtrW(target, w.GWLP_WNDPROC, procedure)
		if original == 0 {
			t.Fatal(code)
		}
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
	fixture.Window = Window{ID: windowID(root), PID: os.Getpid(), Title: "Computer MCP Windows fixture", Bounds: winRect(rect)}
	if fixture.Popup != 0 {
		w.ShowWindow(fixture.Popup, w.SW_SHOWNOACTIVATE)
	}
	if os.Getenv("COMPUTER_MCP_WINDOWS_ACTIVATION_TEST") == "1" && w.GetForegroundWindow() == root {
		other, code := w.CreateWindowExW(0, w.StrToPwstr("STATIC"), w.StrToPwstr("Computer MCP inactive fixture"), w.WS_OVERLAPPEDWINDOW, 300, 300, 400, 200, 0, 0, 0, nil)
		if other == 0 {
			t.Fatal(code)
		}
		defer w.DestroyWindow(other)
		w.ShowWindow(other, w.SW_SHOWNOACTIVATE)
		fixture.InactiveID = windowID(other)
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
		if err := b.CheckPoint(fixture.Window, point, false); err != nil {
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

type windowsObservation struct {
	SnapshotID  string    `json:"snapshot_id"`
	Window      Window    `json:"window"`
	Elements    []Element `json:"elements"`
	Region      Rect      `json:"region"`
	CaptureMode string    `json:"capture_mode"`
	ImageWidth  int       `json:"image_width"`
	ImageHeight int       `json:"image_height"`
}

type windowsClickResult struct {
	InputDelivered   bool               `json:"input_delivered"`
	Observation      windowsObservation `json:"observation"`
	ObservationError json.RawMessage    `json:"observation_error"`
}

func startWindowsObservationFixture(t *testing.T) (windowsFixture, func(string, map[string]any, any)) {
	t.Helper()
	if os.Getenv("COMPUTER_MCP_NATIVE_TEST") != "1" {
		t.Skip("set COMPUTER_MCP_NATIVE_TEST=1 in an interactive desktop session")
	}
	t.Setenv("COMPUTER_MCP_WINDOWS_DIALOG_TEST", "1")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	binary := os.Getenv("COMPUTER_MCP_TEST_BINARY")
	if binary == "" {
		binary = filepath.Join(t.TempDir(), "computer-mcp.exe")
		if output, err := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/computer-mcp").CombinedOutput(); err != nil {
			t.Fatalf("build computer-mcp: %v\n%s", err, output)
		}
	}
	_, fixture := startWindowsFixture(t)
	client := mcp.NewClient(&mcp.Implementation{Name: "windows-click-fixture", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.CommandContext(ctx, binary)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	call := func(name string, args map[string]any, result any) {
		t.Helper()
		r, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		text := r.Content[0].(*mcp.TextContent).Text
		if r.IsError {
			t.Fatalf("%s: %s", name, text)
		}
		if err := json.Unmarshal([]byte(text), result); err != nil {
			t.Fatal(err)
		}
	}
	return fixture, call
}

func TestNativeWindowsClickObservation(t *testing.T) {
	fixture, call := startWindowsObservationFixture(t)
	clickAfter := func(s windowsObservation, elementID string) windowsObservation {
		t.Helper()
		var result windowsClickResult
		call("computer_click", map[string]any{"snapshot_id": s.SnapshotID, "element_id": elementID, "observe_after": map[string]any{}}, &result)
		if !result.InputDelivered || result.Observation.SnapshotID == "" || result.Observation.SnapshotID == s.SnapshotID {
			t.Fatalf("click did not return a new observation: %s", result.ObservationError)
		}
		return result.Observation
	}

	requireFixtureActive(t, fixture)
	var main windowsObservation
	call("computer_observe", map[string]any{"window_id": fixture.Window.ID}, &main)
	var openID string
	for _, el := range main.Elements {
		if el.Name == "Open fixture dialog" && el.Enabled {
			openID = el.ID
			break
		}
	}
	if openID == "" {
		t.Fatal("fixture open button missing from observation")
	}
	dialog := clickAfter(main, openID)
	if dialog.Window.Title != fixtureDialogTitle || dialog.Window.PID != fixture.Window.PID || dialog.Window.ID == main.Window.ID {
		t.Fatalf("expected owned dialog, got %+v", dialog.Window)
	}
	dialogHandle, err := hwnd(dialog.Window.ID)
	if err != nil || dialogHandle != w.GetForegroundWindow() {
		t.Fatal("fixture dialog is not in the foreground")
	}
	button, _ := w.GetDlgItem(dialogHandle, int32(w.IDOK))
	var buttonBounds w.RECT
	if ok, code := w.GetWindowRect(button, &buttonBounds); ok == 0 {
		t.Fatalf("resolve fixture dialog button: %v", code)
	}
	var closeID string
	for _, el := range dialog.Elements {
		if el.Enabled && el.Bounds == winRect(buttonBounds) {
			closeID = el.ID
			break
		}
	}
	if closeID == "" {
		t.Fatal("fixture close button missing from observation")
	}
	returned := clickAfter(dialog, closeID)
	if returned.Window.ID != main.Window.ID || !returned.Window.Active {
		t.Fatalf("expected main window after closing the dialog, got %+v", returned.Window)
	}
}

func TestNativeWindowsMenuObservation(t *testing.T) {
	fixture, call := startWindowsObservationFixture(t)
	requireFixtureActive(t, fixture)
	var image windowsObservation
	call("computer_observe", map[string]any{"window_id": fixture.Window.ID, "screenshot": true}, &image)
	var editorBounds w.RECT
	if ok, code := w.GetWindowRect(fixture.Editor, &editorBounds); ok == 0 {
		t.Fatal(code)
	}
	center := winRect(editorBounds).Center()
	point := Point{X: (center.X - image.Region.X) * float64(image.ImageWidth) / image.Region.Width, Y: (center.Y - image.Region.Y) * float64(image.ImageHeight) / image.Region.Height}
	var opened windowsClickResult
	call("computer_click", map[string]any{"snapshot_id": image.SnapshotID, "button": "right", "point": point, "observe_after": map[string]any{"screenshot": true}}, &opened)
	menu := opened.Observation
	if !opened.InputDelivered || !menu.Window.Menu || menu.Window.PID != fixture.Window.PID || menu.CaptureMode != "screen" || menu.Region != menu.Window.Bounds || menu.ImageWidth == 0 || menu.ImageHeight == 0 {
		t.Fatalf("expected a scoped menu screenshot, got %+v (%s)", menu, opened.ObservationError)
	}
	var closed windowsClickResult
	call("computer_click", map[string]any{"snapshot_id": menu.SnapshotID, "point": Point{X: float64(menu.ImageWidth) / 2, Y: float64(menu.ImageHeight) / 2}, "observe_after": map[string]any{}}, &closed)
	if !closed.InputDelivered || closed.Observation.Window.ID != fixture.Window.ID || fixtureText(t, fixture.Editor) != "menu clicked" {
		t.Fatalf("menu item selection did not return to the fixture: %+v (%s)", closed.Observation, closed.ObservationError)
	}
}

func TestNativeWindowsMouse(t *testing.T) {
	b, fixture := startWindowsFixture(t)
	ctx := context.Background()
	var rect w.RECT
	if ok, code := w.GetWindowRect(fixture.Canvas, &rect); ok == 0 {
		t.Fatal(code)
	}
	p := winRect(rect).Center()
	if err := b.CheckPoint(fixture.Window, p, false); err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		var got string
		for time.Now().Before(deadline) {
			got = fixtureText(t, fixture.Canvas)
			if strings.Contains(got, want) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("mouse event: got %q, want %q", got, want)
	}
	for _, test := range []struct {
		op   string
		in   Input
		want string
	}{
		{"move", Input{Point: p}, "move:0:false"},
		{"click", Input{Point: p, Button: "left", Count: 1, HoldKeys: []string{"ctrl", "shift"}}, "up:12:false"},
		{"scroll", Input{Point: p, DY: 1, HoldKeys: []string{"ctrl"}}, "scroll:8:false"},
		{"drag", Input{Path: []Point{p, {X: p.X + 50, Y: p.Y}}, Duration: 100, HoldKeys: []string{"space", "shift"}}, "up:4:true"},
	} {
		requireFixtureActive(t, fixture)
		fixtureMessage(t, fixture.Canvas, w.WM_APP, 0, 0)
		if err := b.Input(ctx, test.op, test.in); err != nil {
			t.Fatal(err)
		}
		check(test.want)
		for _, key := range []w.VIRTUAL_KEY{w.VK_CONTROL, w.VK_SHIFT, w.VK_SPACE, w.VK_LBUTTON} {
			if w.GetAsyncKeyState(int32(key)) < 0 {
				t.Fatalf("key %d held after %s", key, test.op)
			}
		}
	}
	requireFixtureActive(t, fixture)
	fixtureMessage(t, fixture.Canvas, w.WM_APP, 0, 0)
	cancelCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	err := b.Input(cancelCtx, "drag", Input{Path: []Point{p, {X: p.X + 100, Y: p.Y}}, Duration: 1000, HoldKeys: []string{"space", "shift"}})
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("drag cancellation: %v", err)
	}
	check("up:4:true")
	for _, key := range []w.VIRTUAL_KEY{w.VK_SHIFT, w.VK_SPACE, w.VK_LBUTTON} {
		if w.GetAsyncKeyState(int32(key)) < 0 {
			t.Fatalf("key %d held after cancellation", key)
		}
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

package desktop

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	w "github.com/zzl/go-win32api/v2/win32"
)

const fixturePopupButtonName = "Select fixture popup item"

func createWindowsPopupFixture(t *testing.T, root w.HWND, fixture *windowsFixture) w.HWND {
	t.Helper()
	create := func(ex w.WINDOW_EX_STYLE, class, caption string, style w.WINDOW_STYLE, x, y, width, height int32, parent w.HWND) w.HWND {
		t.Helper()
		handle, code := w.CreateWindowExW(ex, w.StrToPwstr(class), w.StrToPwstr(caption), style, x, y, width, height, parent, 0, 0, nil)
		if handle == 0 {
			t.Fatal(code)
		}
		return handle
	}
	// The popup overlaps the edge of the main window, which starts at x=100.
	// Its button center is outside the main window.
	fixture.Popup = create(w.WS_EX_NOACTIVATE, "STATIC", "Fixture popup", w.WS_POPUP, 16, 190, 100, 70, root)
	button := create(0, "BUTTON", fixturePopupButtonName, w.WS_CHILD|w.WS_VISIBLE, 8, 8, 80, 30, fixture.Popup)
	fixture.PopupCover = create(0, "BUTTON", "Cover inside fixture popup", w.WS_CHILD, 8, 8, 80, 30, fixture.Popup)
	fixture.WindowCover = create(w.WS_EX_NOACTIVATE, "STATIC", "Unowned fixture cover", w.WS_POPUP, 16, 190, 100, 70, 0)
	return button
}

func TestNativeWindowsOwnedPopupElement(t *testing.T) {
	t.Setenv("COMPUTER_MCP_WINDOWS_POPUP_TEST", "1")
	b, fixture := startWindowsFixture(t)
	ctx := context.Background()
	requireFixtureActive(t, fixture)
	// Win32 exposes this fixture popup as a separate UIA root. Observe it
	// directly to exercise the same native references a provider can expose
	// in its main window tree, as Paint does.
	popup := Window{ID: windowID(fixture.Popup), PID: fixture.Window.PID}
	nodes, _, err := b.Observe(ctx, popup, ObserveOptions{Limit: 20, MaxDepth: 64})
	if err != nil {
		t.Fatal(err)
	}
	var button, container Element
	for _, node := range nodes {
		switch node.Name {
		case fixturePopupButtonName:
			button = node
		case "Fixture popup":
			container = node
		}
	}
	point := button.Bounds.Center()
	if button.ID == "" || fixture.Window.Bounds.Contains(point) {
		t.Fatalf("expected a popup button outside the fixture window: %+v", button)
	}
	checkObscured := func(err error, reason string) {
		t.Helper()
		var native *Error
		if !errors.As(err, &native) || native.Code != "target_obscured" || !strings.Contains(native.Detail, "reason="+reason+";") {
			t.Fatalf("expected target_obscured with reason %s, got %v", reason, err)
		}
	}
	checkObscured(b.CheckPoint(fixture.Window, point, true), "other_window_at_point")
	if container.ID == "" || !button.Bounds.Contains(container.Bounds.Center()) {
		t.Fatalf("expected popup container centered on the button: %+v", container)
	}
	_, err = b.ElementClickPoint(ctx, fixture.Window, container)
	checkObscured(err, "popup_element_mismatch")
	const show = w.SWP_SHOWWINDOW | w.SWP_NOACTIVATE | w.SWP_NOMOVE | w.SWP_NOSIZE
	if ok, code := w.SetWindowPos(fixture.WindowCover, w.HWND_TOPMOST, 0, 0, 0, 0, show); ok == 0 {
		t.Fatal(code)
	}
	_, err = b.ElementClickPoint(ctx, fixture.Window, button)
	checkObscured(err, "other_window_at_point")
	w.ShowWindow(fixture.WindowCover, w.SW_HIDE)
	if ok, code := w.SetWindowPos(fixture.PopupCover, w.HWND_TOP, 0, 0, 0, 0, show); ok == 0 {
		t.Fatal(code)
	}
	_, err = b.ElementClickPoint(ctx, fixture.Window, button)
	checkObscured(err, "popup_element_mismatch")
	w.ShowWindow(fixture.PopupCover, w.SW_HIDE)

	validated, err := b.ElementClickPoint(ctx, fixture.Window, button)
	if err != nil || validated != point {
		t.Fatalf("popup button point = %+v, err = %v", validated, err)
	}
	requireFixtureActive(t, fixture)
	if err := b.Input(ctx, "click", Input{Point: validated, Button: "left", Count: 1}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for w.IsWindow(fixture.Popup) != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got, stillOpen := fixtureText(t, fixture.Editor), w.IsWindow(fixture.Popup) != 0; stillOpen || got != "popup clicked" {
		t.Fatalf("owned popup did not handle the click and close: open=%t, editor=%q", stillOpen, got)
	}
	var native *Error
	if _, err := b.Resolve(button.ID); !errors.As(err, &native) || native.Code != "stale_snapshot" {
		t.Fatalf("closed popup reference: %v", err)
	}
}

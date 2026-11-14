package server

import (
	"context"
	"errors"
	"testing"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
)

func TestActivationMustReachTarget(t *testing.T) {
	s, b := setup()
	b.windows[0].Active = false
	ctx := context.Background()
	requireCode(t, window(ctx, s, WindowArgs{Action: "activate", WindowID: "w1", Timeout: 1}), "activation_failed")
	b.windows[0].BlockedBy = "modal sheet"
	requireCode(t, window(ctx, s, WindowArgs{Action: "activate", WindowID: "w1"}), "window_blocked")
	b.windows[0].BlockedBy = ""
	b.activate = true
	r := window(ctx, s, WindowArgs{Action: "activate", WindowID: "w1"})
	if r.err != nil || !r.value.(desktop.Window).Active {
		t.Fatalf("activation: %+v", r)
	}
}

func TestKeyboardFocusAndSingleDelivery(t *testing.T) {
	s, b := setup()
	ctx := context.Background()
	requireCode(t, typeText(ctx, s, TypeArgs{WindowID: "w1", Text: "hello", FocusName: "Editor", Timeout: 1}), "focus_timeout")
	if b.inputs != 0 {
		t.Fatal("typed before focus")
	}
	b.windows[0].Active = false
	b.activate = true
	b.focus = desktop.Element{Name: "Editor", Focused: true}
	b.inputErr = errors.New("uncertain delivery")
	requireCode(t, typeText(ctx, s, TypeArgs{WindowID: "w1", Text: "hello", FocusName: "Editor", AutoActivate: true}), "native_error")
	if b.inputs != 1 {
		t.Fatal("input was retried")
	}
}

func TestWaitFocusConditions(t *testing.T) {
	s, b := setup()
	ctx := context.Background()
	b.focus = desktop.Element{Name: "Other", Focused: true}
	r := wait(ctx, s, WaitArgs{WindowID: "w1", FocusName: "Editor", Timeout: 1})
	if r.err != nil || r.value.(map[string]any)["matched"] != false {
		t.Fatalf("wrong focus matched: %+v", r)
	}
	b.focus.Name = "Editor"
	r = wait(ctx, s, WaitArgs{WindowID: "w1", FocusName: "Editor", Text: "Save", Timeout: 100})
	if r.err != nil || r.value.(map[string]any)["matched"] != true {
		t.Fatalf("focus and text: %+v", r)
	}
}

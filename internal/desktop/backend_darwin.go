package desktop

/*
#cgo CFLAGS: -mmacosx-version-min=14.0
#cgo LDFLAGS: -mmacosx-version-min=14.0 -framework Cocoa -framework ApplicationServices -framework ScreenCaptureKit -framework ImageIO
#include <stdlib.h>
#include "native_darwin.h"
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"unsafe"
)

type macBackend struct{}

func New() (Backend, error)  { return &macBackend{}, nil }
func (b *macBackend) Close() { C.cm_close() }
func macCall(op string, args any, out any) error {
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	co, ca := C.CString(op), C.CString(string(data))
	defer C.free(unsafe.Pointer(co))
	defer C.free(unsafe.Pointer(ca))
	result := C.cm_call(co, ca)
	if result == nil {
		return Fail("native_error", "empty native response")
	}
	defer C.free(unsafe.Pointer(result))
	raw := []byte(C.GoString(result))
	var envelope struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	if envelope.Code != "" {
		return Fail(envelope.Code, envelope.Detail)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
func (b *macBackend) Status() (Status, error) {
	var s Status
	err := macCall("status", nil, &s)
	return s, err
}
func (b *macBackend) Windows() ([]Window, error) {
	var r struct {
		Windows []Window `json:"windows"`
	}
	err := macCall("windows", nil, &r)
	return r.Windows, err
}
func (b *macBackend) Observe(ctx context.Context, w Window, options ObserveOptions) ([]Element, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	var r struct {
		Elements  []Element `json:"elements"`
		Truncated bool      `json:"truncated"`
	}
	err := macCall("observe", map[string]any{"window_id": w.ID, "options": options}, &r)
	return r.Elements, r.Truncated, err
}
func (b *macBackend) Resolve(id string) (Element, error) {
	var e Element
	err := macCall("resolve", map[string]any{"element_id": id}, &e)
	return e, err
}
func (b *macBackend) Focus(w Window) (Element, error) {
	var e Element
	err := macCall("focus", map[string]any{"window_id": w.ID}, &e)
	return e, err
}
func (b *macBackend) Window(w Window, action string, r Rect) error {
	return macCall("window", map[string]any{"window_id": w.ID, "action": action, "bounds": r}, nil)
}
func (b *macBackend) Capture(ctx context.Context, r Rect, width int) (Capture, error) {
	return b.capture(ctx, "", r, width)
}
func (b *macBackend) CaptureWindow(ctx context.Context, w Window, r Rect, width int) (Capture, error) {
	return b.capture(ctx, w.ID, r, width)
}
func (b *macBackend) CheckPoint(w Window, p Point) error {
	return macCall("check_point", map[string]any{"window_id": w.ID, "x": p.X, "y": p.Y}, nil)
}
func (b *macBackend) capture(ctx context.Context, id string, r Rect, width int) (Capture, error) {
	if err := ctx.Err(); err != nil {
		return Capture{}, err
	}
	var length C.size_t
	var ow, oh C.int
	var nativeErr *C.char
	var windowID *C.char
	if id != "" {
		windowID = C.CString(id)
		defer C.free(unsafe.Pointer(windowID))
	}
	ptr := C.cm_capture(windowID, C.double(r.X), C.double(r.Y), C.double(r.Width), C.double(r.Height), C.int(width), &length, &ow, &oh, &nativeErr)
	if nativeErr != nil {
		defer C.free(unsafe.Pointer(nativeErr))
		return Capture{}, Fail("native_error", C.GoString(nativeErr))
	}
	if ptr == nil {
		return Capture{}, Fail("permission_denied", "screen capture unavailable")
	}
	defer C.free(unsafe.Pointer(ptr))
	return Capture{Data: C.GoBytes(unsafe.Pointer(ptr), C.int(length)), Width: int(ow), Height: int(oh)}, nil
}
func (b *macBackend) Input(ctx context.Context, op string, in Input) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch op {
	case "type":
		chars := []rune(in.Text)
		for start := 0; start < len(chars); start += 20 {
			if err := ctx.Err(); err != nil {
				return err
			}
			end := min(start+20, len(chars))
			if err := macCall("type", map[string]any{"text": string(chars[start:end])}, nil); err != nil {
				return err
			}
		}
		return nil
	case "drag":
		if err := macCall("mouse", map[string]any{"x": in.Point.X, "y": in.Point.Y, "phase": "down", "button": "left", "count": 1}, nil); err != nil {
			return err
		}
		p := in.Point
		var result error
		steps := max(1, in.Duration/16)
		for i := 1; i <= steps; i++ {
			select {
			case <-ctx.Done():
				result = ctx.Err()
			case <-time.After(time.Duration(in.Duration/steps) * time.Millisecond):
			}
			if result != nil {
				break
			}
			t := float64(i) / float64(steps)
			p = Point{in.Point.X + (in.To.X-in.Point.X)*t, in.Point.Y + (in.To.Y-in.Point.Y)*t}
			result = macCall("mouse", map[string]any{"x": p.X, "y": p.Y, "phase": "drag", "button": "left", "count": 1}, nil)
			if result != nil {
				break
			}
		}
		return errors.Join(result, macCall("mouse", map[string]any{"x": p.X, "y": p.Y, "phase": "up", "button": "left", "count": 1}, nil))
	case "click":
		for i := 1; i <= in.Count; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			args := map[string]any{"x": in.Point.X, "y": in.Point.Y, "button": in.Button, "count": i, "phase": "down"}
			if err := macCall("mouse", args, nil); err != nil {
				return err
			}
			args["phase"] = "up"
			if err := macCall("mouse", args, nil); err != nil {
				return err
			}
		}
		return nil
	case "scroll":
		return macCall("scroll", map[string]any{"x": in.Point.X, "y": in.Point.Y, "dx": in.DX, "dy": in.DY}, nil)
	case "key":
		return macCall("key", map[string]any{"keys": in.Keys}, nil)
	}
	return Fail("invalid_argument", op)
}

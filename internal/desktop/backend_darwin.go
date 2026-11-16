package desktop

/*
#cgo CFLAGS: -mmacosx-version-min=14.0
#cgo LDFLAGS: -mmacosx-version-min=14.0 -framework Cocoa -framework ApplicationServices -framework ScreenCaptureKit -framework ImageIO -framework Vision
#include <stdlib.h>
#include "native_darwin.h"
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
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
func (b *macBackend) Recognize(ctx context.Context, capture Capture, limit int) ([]RecognizedText, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	data := C.CBytes(capture.Data)
	defer C.free(data)
	result := C.cm_recognize((*C.uchar)(data), C.size_t(len(capture.Data)), C.int(limit))
	if result == nil {
		return nil, false, Fail("native_error", "empty OCR response")
	}
	defer C.free(unsafe.Pointer(result))
	var response struct {
		Regions   []RecognizedText `json:"regions"`
		Truncated bool             `json:"truncated"`
		Code      string           `json:"code"`
		Detail    string           `json:"detail"`
	}
	if err := json.Unmarshal([]byte(C.GoString(result)), &response); err != nil {
		return nil, false, err
	}
	if response.Code != "" {
		return nil, false, Fail(response.Code, response.Detail)
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return response.Regions, response.Truncated, nil
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
func (b *macBackend) Input(ctx context.Context, op string, in Input) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(in.HoldKeys) > 0 {
		defer func() {
			result = errors.Join(result, macCall("hold_keys", map[string]any{"keys": in.HoldKeys, "down": false}, nil))
		}()
		if err := macCall("hold_keys", map[string]any{"keys": in.HoldKeys, "down": true}, nil); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	switch op {
	case "move":
		return macCall("mouse", map[string]any{"x": in.Point.X, "y": in.Point.Y, "phase": "move"}, nil)
	case "type":
		chars := []rune(in.Text)
		for start := 0; start < len(chars); {
			if err := ctx.Err(); err != nil {
				return err
			}
			end := start
			for end < len(chars) && end-start < 20 && chars[end] != '\n' && chars[end] != '\r' && chars[end] != '\t' {
				end++
			}
			if end == start {
				end++
			}
			if err := macCall("type", map[string]any{"text": string(chars[start:end])}, nil); err != nil {
				return err
			}
			start = end
		}
		return nil
	case "drag":
		p := in.Path[0]
		result := macCall("mouse", map[string]any{"x": p.X, "y": p.Y, "phase": "down", "button": "left", "count": 1, "hold_keys": in.HoldKeys}, nil)
		if result == nil {
			p, result = moveDrag(ctx, in, func(point Point) error {
				return macCall("mouse", map[string]any{"x": point.X, "y": point.Y, "phase": "drag", "button": "left", "count": 1, "hold_keys": in.HoldKeys}, nil)
			})
		}
		return errors.Join(result, macCall("mouse", map[string]any{"x": p.X, "y": p.Y, "phase": "up", "button": "left", "count": 1, "hold_keys": in.HoldKeys}, nil))
	case "click":
		for i := 1; i <= in.Count; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			args := map[string]any{"x": in.Point.X, "y": in.Point.Y, "button": in.Button, "count": i, "phase": "down", "hold_keys": in.HoldKeys}
			err := macCall("mouse", args, nil)
			args["phase"] = "up"
			if err := errors.Join(err, macCall("mouse", args, nil)); err != nil {
				return err
			}
		}
		return nil
	case "scroll":
		return macCall("scroll", map[string]any{"x": in.Point.X, "y": in.Point.Y, "dx": in.DX, "dy": in.DY, "hold_keys": in.HoldKeys}, nil)
	case "key":
		return macCall("key", map[string]any{"keys": in.Keys}, nil)
	}
	return Fail("invalid_argument", op)
}

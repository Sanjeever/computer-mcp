package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/kbinani/screenshot"
	w "github.com/zzl/go-win32api/v2/win32"
	"golang.org/x/image/draw"
)

type winBackend struct {
	automation *w.IUIAutomation
	walker     *w.IUIAutomationTreeWalker
	cache      *w.IUIAutomationCacheRequest
	elements   []*w.IUIAutomationElement
}

func hr(code w.HRESULT) error {
	if code < 0 {
		return Fail("native_error", fmt.Sprintf("HRESULT 0x%08x", uint32(code)))
	}
	return nil
}
func New() (Backend, error) {
	if err := hr(w.CoInitializeEx(nil, w.COINIT_MULTITHREADED)); err != nil {
		return nil, err
	}
	b := &winBackend{}
	fail := func(err error) (Backend, error) { b.Close(); return nil, err }
	w.SetProcessDpiAwarenessContext(w.DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2)
	clsid := syscall.GUID{Data1: 0xe22ad333, Data2: 0xb25f, Data3: 0x460c, Data4: [8]byte{0x83, 0xd0, 0x05, 0x81, 0x10, 0x73, 0x95, 0xc9}}
	if err := hr(w.CoCreateInstance(&clsid, nil, w.CLSCTX_INPROC_SERVER, &w.IID_IUIAutomation, unsafe.Pointer(&b.automation))); err != nil {
		return fail(err)
	}
	var a2 *w.IUIAutomation2
	if err := hr(b.automation.QueryInterface(&w.IID_IUIAutomation2, unsafe.Pointer(&a2))); err != nil {
		return fail(err)
	}
	err := errors.Join(hr(a2.Put_ConnectionTimeout(500)), hr(a2.Put_TransactionTimeout(500)))
	a2.Release()
	if err != nil {
		return fail(err)
	}
	if err = hr(b.automation.Get_ControlViewWalker(&b.walker)); err != nil {
		return fail(err)
	}
	if err = hr(b.automation.CreateCacheRequest(&b.cache)); err != nil {
		return fail(err)
	}
	for _, property := range []w.UIA_PROPERTY_ID{w.UIA_NamePropertyId, w.UIA_LocalizedControlTypePropertyId, w.UIA_BoundingRectanglePropertyId, w.UIA_IsEnabledPropertyId, w.UIA_HasKeyboardFocusPropertyId, w.UIA_IsPasswordPropertyId, w.UIA_ValueValuePropertyId} {
		if err = hr(b.cache.AddProperty(property)); err != nil {
			return fail(err)
		}
	}
	if err = hr(b.cache.Put_TreeScope(w.TreeScope_Element)); err != nil {
		return fail(err)
	}
	return b, nil
}
func (b *winBackend) clear() {
	for _, e := range b.elements {
		e.Release()
	}
	b.elements = nil
}
func (b *winBackend) Close() {
	b.clear()
	if b.cache != nil {
		b.cache.Release()
	}
	if b.walker != nil {
		b.walker.Release()
	}
	if b.automation != nil {
		b.automation.Release()
	}
	w.CoUninitialize()
}
func (b *winBackend) Status() (Status, error) {
	s := Status{Platform: "windows", Accessibility: true, ScreenCapture: true, Displays: []Display{}}
	for i := 0; i < screenshot.NumActiveDisplays(); i++ {
		r := screenshot.GetDisplayBounds(i)
		s.Displays = append(s.Displays, Display{strconv.Itoa(i), Rect{float64(r.Min.X), float64(r.Min.Y), float64(r.Dx()), float64(r.Dy())}})
	}
	return s, nil
}
func hwnd(id string) (w.HWND, error) {
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil || n == 0 {
		return 0, Fail("window_not_found", id)
	}
	h := w.HWND(n)
	if w.IsWindow(h) == 0 {
		return 0, Fail("window_not_found", id)
	}
	return h, nil
}
func winRect(r w.RECT) Rect {
	return Rect{float64(r.Left), float64(r.Top), float64(r.Right - r.Left), float64(r.Bottom - r.Top)}
}

type windowEnumeration struct {
	windows []Window
	active  w.HWND
}

var enumerationMu sync.Mutex
var activeEnumeration *windowEnumeration

var windowCallback = syscall.NewCallback(func(handle uintptr, _ uintptr) uintptr {
	result := activeEnumeration
	h := w.HWND(handle)
	if w.IsWindowVisible(h) == 0 || w.IsIconic(h) != 0 {
		return 1
	}
	var r w.RECT
	if ok, _ := w.GetWindowRect(h, &r); ok == 0 || r.Right <= r.Left || r.Bottom <= r.Top {
		return 1
	}
	buf := make([]uint16, 1024)
	n, _ := w.GetWindowTextW(h, &buf[0], int32(len(buf)))
	if n == 0 {
		return 1
	}
	var pid uint32
	w.GetWindowThreadProcessId(h, &pid)
	app := ""
	process, _ := w.OpenProcess(w.PROCESS_QUERY_LIMITED_INFORMATION, 0, pid)
	if process != 0 {
		path := make([]uint16, 32768)
		size := uint32(len(path))
		if ok, _ := w.QueryFullProcessImageNameW(process, 0, &path[0], &size); ok != 0 {
			app = filepath.Base(syscall.UTF16ToString(path[:size]))
		}
		w.CloseHandle(process)
	}
	result.windows = append(result.windows, Window{ID: strconv.FormatUint(uint64(handle), 10), PID: int(pid), App: app, Title: syscall.UTF16ToString(buf[:n]), Bounds: winRect(r), Active: h == result.active})
	if w.IsWindowEnabled(h) == 0 {
		popup := w.GetLastActivePopup(h)
		if popup != 0 && popup != h {
			result.windows[len(result.windows)-1].BlockedBy = strconv.FormatUint(uint64(popup), 10)
		}
	}
	if len(result.windows) >= 512 {
		return 0
	}
	return 1
})

func (b *winBackend) Windows() ([]Window, error) {
	enumerationMu.Lock()
	defer enumerationMu.Unlock()
	result := &windowEnumeration{windows: []Window{}, active: w.GetForegroundWindow()}
	activeEnumeration = result
	defer func() { activeEnumeration = nil }()
	ok, err := w.EnumWindows(windowCallback, 0)
	if ok == 0 && len(result.windows) < 512 {
		return nil, Fail("native_error", fmt.Sprint(err))
	}
	return result.windows, nil
}
func (b *winBackend) Window(target Window, action string, r Rect) error {
	h, err := hwnd(target.ID)
	if err != nil {
		return err
	}
	if action == "activate" {
		if w.IsIconic(h) != 0 {
			w.ShowWindow(h, w.SW_RESTORE)
		}
		if w.SetForegroundWindow(h) == 0 {
			return Fail("native_error", "Windows denied foreground activation")
		}
		return nil
	}
	ok, code := w.SetWindowPos(h, 0, int32(r.X), int32(r.Y), int32(r.Width), int32(r.Height), w.SWP_NOZORDER|w.SWP_NOACTIVATE)
	if ok == 0 {
		return Fail("native_error", fmt.Sprint(code))
	}
	return nil
}
func cachedNode(e *w.IUIAutomationElement, id, parent string) (Element, error) {
	n := Element{ID: id, Parent: parent}
	var r w.RECT
	var name, role w.BSTR
	var enabled, focused, password w.BOOL
	if err := hr(e.Get_CachedBoundingRectangle(&r)); err != nil {
		return n, err
	}
	if err := hr(e.Get_CachedName(&name)); err != nil {
		return n, err
	}
	n.Name = w.BstrToStrAndFree(name)
	if err := hr(e.Get_CachedLocalizedControlType(&role)); err != nil {
		return n, err
	}
	n.Role = w.BstrToStrAndFree(role)
	if err := hr(e.Get_CachedIsPassword(&password)); err != nil {
		return n, err
	}
	if err := hr(e.Get_CachedIsEnabled(&enabled)); err != nil {
		return n, err
	}
	if err := hr(e.Get_CachedHasKeyboardFocus(&focused)); err != nil {
		return n, err
	}
	n.Bounds = winRect(r)
	n.Enabled = enabled != 0
	n.Focused = focused != 0
	n.Password = password != 0
	if !n.Password {
		var v w.VARIANT
		if e.GetCachedPropertyValue(w.UIA_ValueValuePropertyId, &v) >= 0 {
			if v.Vt == w.VT_BSTR {
				n.Value = w.BstrToStr(*v.BstrVal())
			}
			w.VariantClear(&v)
		}
	}
	return n, nil
}
func (b *winBackend) Observe(ctx context.Context, target Window, limit int) ([]Element, bool, error) {
	b.clear()
	h, err := hwnd(target.ID)
	if err != nil {
		return nil, false, err
	}
	var root *w.IUIAutomationElement
	if err = hr(b.automation.ElementFromHandleBuildCache(h, b.cache, &root)); err != nil {
		return nil, false, err
	}
	type entry struct {
		e      *w.IUIAutomationElement
		parent string
	}
	queue := []entry{{root, ""}}
	b.elements = append(b.elements, root)
	out := []Element{}
	truncated := false
	deadline := time.Now().Add(3 * time.Second)
	for cursor := 0; cursor < len(queue); cursor++ {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		if time.Now().After(deadline) {
			truncated = true
			break
		}
		item := queue[cursor]
		id := strconv.Itoa(cursor)
		n, err := cachedNode(item.e, id, item.parent)
		if err != nil {
			truncated = true
			continue
		}
		out = append(out, n)
		var child *w.IUIAutomationElement
		if b.walker.GetFirstChildElementBuildCache(item.e, b.cache, &child) < 0 {
			truncated = true
			continue
		}
		for child != nil {
			if len(queue) >= limit || time.Now().After(deadline) || ctx.Err() != nil {
				child.Release()
				truncated = true
				break
			}
			queue = append(queue, entry{child, id})
			b.elements = append(b.elements, child)
			var next *w.IUIAutomationElement
			if b.walker.GetNextSiblingElementBuildCache(child, b.cache, &next) < 0 {
				truncated = true
				break
			}
			child = next
		}
	}
	return out, truncated, nil
}
func (b *winBackend) Resolve(id string) (Element, error) {
	i, err := strconv.Atoi(id)
	if err != nil || i < 0 || i >= len(b.elements) {
		return Element{}, Fail("stale_snapshot", id)
	}
	var updated *w.IUIAutomationElement
	if b.elements[i].BuildUpdatedCache(b.cache, &updated) < 0 {
		return Element{}, Fail("stale_snapshot", id)
	}
	defer updated.Release()
	return cachedNode(updated, id, "")
}
func (b *winBackend) Focus(target Window) (Element, error) {
	h, err := hwnd(target.ID)
	if err != nil {
		return Element{}, err
	}
	if w.GetForegroundWindow() != h {
		return Element{}, nil
	}
	var e *w.IUIAutomationElement
	if err := hr(b.automation.GetFocusedElementBuildCache(b.cache, &e)); err != nil {
		return Element{}, err
	}
	defer e.Release()
	var pid int32
	if err := hr(e.Get_CurrentProcessId(&pid)); err != nil {
		return Element{}, err
	}
	if int(pid) != target.PID {
		return Element{}, nil
	}
	return cachedNode(e, "", "")
}
func (b *winBackend) Capture(ctx context.Context, r Rect, width int) (Capture, error) {
	if err := ctx.Err(); err != nil {
		return Capture{}, err
	}
	if r.Width*r.Height > 40_000_000 {
		return Capture{}, Fail("invalid_argument", "capture region exceeds 40 million pixels; provide a smaller region")
	}
	source, err := screenshot.Capture(int(r.X), int(r.Y), int(r.Width), int(r.Height))
	if err != nil {
		return Capture{}, Fail("native_error", err.Error())
	}
	scale := math.Min(1, math.Min(float64(width)/r.Width, 2560/r.Height))
	ow, oh := max(1, int(r.Width*scale)), max(1, int(r.Height*scale))
	var img image.Image = source
	if scale < 1 {
		resized := image.NewRGBA(image.Rect(0, 0, ow, oh))
		draw.ApproxBiLinear.Scale(resized, resized.Bounds(), source, source.Bounds(), draw.Src, nil)
		img = resized
	}
	var output bytes.Buffer
	if err = jpeg.Encode(&output, img, &jpeg.Options{Quality: 80}); err != nil {
		return Capture{}, err
	}
	return Capture{output.Bytes(), ow, oh}, nil
}
func send(events []w.INPUT) error {
	if len(events) == 0 {
		return nil
	}
	sent, code := w.SendInput(uint32(len(events)), &events[0], int32(unsafe.Sizeof(w.INPUT{})))
	if int(sent) != len(events) {
		return Fail("native_error", fmt.Sprintf("SendInput sent %d/%d events (error %v); UIPI or desktop restrictions may apply", sent, len(events), code))
	}
	return nil
}
func mouse(flags w.MOUSE_EVENT_FLAGS, data uint32) w.INPUT {
	v := w.INPUT{Type_: w.INPUT_MOUSE}
	v.Mi().DwFlags = flags
	v.Mi().MouseData = data
	return v
}
func keyboard(key w.VIRTUAL_KEY, scan uint16, flags w.KEYBD_EVENT_FLAGS) w.INPUT {
	v := w.INPUT{Type_: w.INPUT_KEYBOARD}
	v.Ki().WVk = key
	v.Ki().WScan = scan
	v.Ki().DwFlags = flags
	return v
}
func move(p Point) error {
	if ok, code := w.SetCursorPos(int32(p.X), int32(p.Y)); ok == 0 {
		return Fail("native_error", fmt.Sprint(code))
	}
	return nil
}
func keyCode(k string) w.VIRTUAL_KEY {
	if len(k) == 1 {
		if k[0] >= 'a' && k[0] <= 'z' {
			return w.VIRTUAL_KEY(k[0] - 32)
		}
		return w.VIRTUAL_KEY(k[0])
	}
	if k[0] == 'f' {
		n, _ := strconv.Atoi(k[1:])
		return w.VIRTUAL_KEY(int(w.VK_F1) + n - 1)
	}
	return map[string]w.VIRTUAL_KEY{"ctrl": w.VK_CONTROL, "alt": w.VK_MENU, "shift": w.VK_SHIFT, "meta": w.VK_LWIN, "enter": w.VK_RETURN, "tab": w.VK_TAB, "escape": w.VK_ESCAPE, "space": w.VK_SPACE, "backspace": w.VK_BACK, "delete": w.VK_DELETE, "home": w.VK_HOME, "end": w.VK_END, "pageup": w.VK_PRIOR, "pagedown": w.VK_NEXT, "left": w.VK_LEFT, "right": w.VK_RIGHT, "up": w.VK_UP, "down": w.VK_DOWN}[k]
}
func (b *winBackend) Input(ctx context.Context, op string, in Input) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch op {
	case "type":
		chars := utf16.Encode([]rune(in.Text))
		for start := 0; start < len(chars); start += 64 {
			if err := ctx.Err(); err != nil {
				return err
			}
			events := []w.INPUT{}
			for _, c := range chars[start:min(start+64, len(chars))] {
				events = append(events, keyboard(0, c, w.KEYEVENTF_UNICODE), keyboard(0, c, w.KEYEVENTF_UNICODE|w.KEYEVENTF_KEYUP))
			}
			if err := send(events); err != nil {
				return err
			}
		}
		return nil
	case "key":
		downs, ups := []w.INPUT{}, []w.INPUT{}
		for _, k := range in.Keys {
			code := keyCode(k)
			flags := w.KEYBD_EVENT_FLAGS(0)
			switch k {
			case "meta", "delete", "home", "end", "pageup", "pagedown", "left", "right", "up", "down":
				flags = w.KEYEVENTF_EXTENDEDKEY
			}
			downs = append(downs, keyboard(code, 0, flags))
			ups = append([]w.INPUT{keyboard(code, 0, flags|w.KEYEVENTF_KEYUP)}, ups...)
		}
		err := send(append(downs, ups...))
		if err != nil {
			return errors.Join(err, send(ups))
		}
		return nil
	case "click":
		if err := move(in.Point); err != nil {
			return err
		}
		down, up := w.MOUSEEVENTF_LEFTDOWN, w.MOUSEEVENTF_LEFTUP
		if in.Button == "right" {
			down, up = w.MOUSEEVENTF_RIGHTDOWN, w.MOUSEEVENTF_RIGHTUP
		}
		if in.Button == "middle" {
			down, up = w.MOUSEEVENTF_MIDDLEDOWN, w.MOUSEEVENTF_MIDDLEUP
		}
		events := []w.INPUT{}
		for i := 0; i < in.Count; i++ {
			events = append(events, mouse(down, 0), mouse(up, 0))
		}
		err := send(events)
		if err != nil {
			return errors.Join(err, send([]w.INPUT{mouse(up, 0)}))
		}
		return nil
	case "scroll":
		if err := move(in.Point); err != nil {
			return err
		}
		events := []w.INPUT{}
		if in.DY != 0 {
			events = append(events, mouse(w.MOUSEEVENTF_WHEEL, uint32(int32(-in.DY*120))))
		}
		if in.DX != 0 {
			events = append(events, mouse(w.MOUSEEVENTF_HWHEEL, uint32(int32(in.DX*120))))
		}
		return send(events)
	case "drag":
		if err := move(in.Point); err != nil {
			return err
		}
		if err := send([]w.INPUT{mouse(w.MOUSEEVENTF_LEFTDOWN, 0)}); err != nil {
			return errors.Join(err, send([]w.INPUT{mouse(w.MOUSEEVENTF_LEFTUP, 0)}))
		}
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
			result = move(Point{in.Point.X + (in.To.X-in.Point.X)*t, in.Point.Y + (in.To.Y-in.Point.Y)*t})
			if result != nil {
				break
			}
		}
		return errors.Join(result, send([]w.INPUT{mouse(w.MOUSEEVENTF_LEFTUP, 0)}))
	}
	return Fail("invalid_argument", op)
}

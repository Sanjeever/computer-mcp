package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	automation        *w.IUIAutomation
	walker            *w.IUIAutomationTreeWalker
	cache             *w.IUIAutomationCacheRequest
	elements          map[string]*w.IUIAutomationElement
	logUIADiagnostics bool
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
	b := &winBackend{logUIADiagnostics: os.Getenv("COMPUTER_MCP_UIA_DIAGNOSTICS") == "1"}
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
	handle, _, _ := strings.Cut(id, ":")
	n, err := strconv.ParseUint(handle, 10, 64)
	if err != nil || n == 0 || windowID(w.HWND(n)) != id {
		return 0, Fail("window_not_found", id)
	}
	return w.HWND(n), nil
}

func identifyWindow(h w.HWND) (id string, pid uint32, process w.HANDLE) {
	thread := w.GetWindowThreadProcessId(h, &pid)
	if thread == 0 {
		return "", 0, 0
	}
	process, _ = w.OpenProcess(w.PROCESS_QUERY_LIMITED_INFORMATION, 0, pid)
	if process == 0 {
		return "", 0, 0
	}
	var born, exit, kernel, user w.FILETIME
	ok, _ := w.GetProcessTimes(process, &born, &exit, &kernel, &user)
	var currentPID uint32
	if ok == 0 || w.GetWindowThreadProcessId(h, &currentPID) != thread || currentPID != pid {
		w.CloseHandle(process)
		return "", 0, 0
	}
	// A handle alone can be reused after a window closes. Include its GUI thread
	// and process lifetime to avoid resolving an old reference in a new process.
	return fmt.Sprintf("%d:%d:%d:%d", h, pid, thread, uint64(born.DwHighDateTime)<<32|uint64(born.DwLowDateTime)), pid, process
}

func windowID(h w.HWND) string {
	id, _, process := identifyWindow(h)
	if process != 0 {
		w.CloseHandle(process)
	}
	return id
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
	id, pid, process := identifyWindow(h)
	if id == "" {
		return 1
	}
	defer w.CloseHandle(process)
	app := ""
	path := make([]uint16, 32768)
	size := uint32(len(path))
	if ok, _ := w.QueryFullProcessImageNameW(process, 0, &path[0], &size); ok != 0 {
		app = filepath.Base(syscall.UTF16ToString(path[:size]))
	}
	window := Window{ID: id, PID: int(pid), App: app, Title: syscall.UTF16ToString(buf[:n]), Bounds: winRect(r), Active: h == result.active}
	if owner, _ := w.GetWindow(h, w.GW_OWNER); owner != 0 {
		var ownerPID uint32
		if w.GetWindowThreadProcessId(owner, &ownerPID) != 0 && ownerPID == pid {
			window.OwnerID = windowID(owner)
		}
	}
	if w.IsWindowEnabled(h) == 0 {
		popup := w.GetLastActivePopup(h)
		if popup != 0 && popup != h {
			window.BlockedBy = windowID(popup)
		}
	}
	result.windows = append(result.windows, window)
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
		if w.GetForegroundWindow() == h {
			return nil
		}
		if w.IsIconic(h) != 0 {
			w.ShowWindow(h, w.SW_RESTORE)
		}
		if w.SetForegroundWindow(h) == 0 && w.GetForegroundWindow() != h {
			return Fail("activation_failed", fmt.Sprintf("Windows denied foreground activation; target_window_id=%s; foreground_window_id=%s", target.ID, windowID(w.GetForegroundWindow())))
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
func runtimeID(e *w.IUIAutomationElement) (string, error) {
	var array *w.SAFEARRAY
	if err := hr(e.GetRuntimeId(&array)); err != nil {
		return "", err
	}
	if array == nil {
		return "", Fail("native_error", "UIA runtime ID is unavailable")
	}
	defer w.SafeArrayDestroy(array)
	var lower, upper int32
	if err := hr(w.SafeArrayGetLBound(array, 1, &lower)); err != nil {
		return "", err
	}
	if err := hr(w.SafeArrayGetUBound(array, 1, &upper)); err != nil {
		return "", err
	}
	if upper < lower || upper-lower > 128 {
		return "", Fail("native_error", "invalid UIA runtime ID")
	}
	var data *int32
	if err := hr(w.SafeArrayAccessData(array, unsafe.Pointer(&data))); err != nil {
		return "", err
	}
	id := fmt.Sprint(unsafe.Slice(data, int(upper-lower+1)))
	w.SafeArrayUnaccessData(array)
	return id, nil
}
func (b *winBackend) Observe(ctx context.Context, target Window, options ObserveOptions) ([]Element, Truncation, error) {
	h, err := hwnd(target.ID)
	if err != nil {
		return nil, 0, err
	}
	var root *w.IUIAutomationElement
	if options.RootID != "" {
		previous := b.elements[options.RootID]
		if previous == nil {
			return nil, 0, Fail("stale_snapshot", "subtree root")
		}
		err = hr(previous.BuildUpdatedCache(b.cache, &root))
	} else {
		err = hr(b.automation.ElementFromHandleBuildCache(h, b.cache, &root))
	}
	if err != nil {
		return nil, 0, err
	}
	if root == nil {
		return nil, 0, Fail("native_error", "UIA reported no root element")
	}
	defer root.Release()
	b.clear()
	b.elements = map[string]*w.IUIAutomationElement{}
	out := []Element{}
	var truncated Truncation
	providerError := func(operation string, err error) {
		if b.logUIADiagnostics && truncated&TruncatedProviderError == 0 {
			fmt.Fprintf(os.Stderr, "computer-mcp uia_traversal operation=%s error=%q\n", operation, err.Error())
		}
		truncated |= TruncatedProviderError
	}
	deadline := time.Now().Add(3 * time.Second)
	visited := map[string]bool{}
	scanned := 0
	full := func() bool {
		switch {
		case ctx.Err() != nil:
		case len(out) >= options.Limit:
			truncated |= TruncatedElementLimit
		case scanned >= 5000:
			truncated |= TruncatedNodeLimit
		case time.Now().After(deadline):
			truncated |= TruncatedTimeLimit
		default:
			return false
		}
		return true
	}
	var visit func(*w.IUIAutomationElement, string, int)
	visit = func(e *w.IUIAutomationElement, parent string, depth int) {
		scanned++
		id, err := runtimeID(e)
		if err != nil {
			providerError("runtime_id", err)
			return
		}
		if visited[id] {
			return
		}
		visited[id] = true
		n, err := cachedNode(e, id, parent)
		if err != nil {
			providerError("cached_properties", err)
		} else if options.Includes(n) {
			e.AddRef()
			b.elements[id] = e
			out = append(out, n)
			parent = id
		}
		var child *w.IUIAutomationElement
		if err := hr(b.walker.GetFirstChildElementBuildCache(e, b.cache, &child)); err != nil {
			providerError("first_child", err)
			return
		}
		for child != nil {
			if full() {
				child.Release()
				return
			}
			if depth >= options.MaxDepth {
				child.Release()
				truncated |= TruncatedDepthLimit
				return
			}
			visit(child, parent, depth+1)
			var next *w.IUIAutomationElement
			code := b.walker.GetNextSiblingElementBuildCache(child, b.cache, &next)
			child.Release()
			if err := hr(code); err != nil {
				providerError("next_sibling", err)
				return
			}
			child = next
		}
	}
	if !full() {
		visit(root, "", 0)
	}
	if err := ctx.Err(); err != nil {
		return nil, truncated, err
	}
	return out, truncated, nil
}
func (b *winBackend) Resolve(id string) (Element, error) {
	e := b.elements[id]
	if e == nil {
		return Element{}, Fail("stale_snapshot", id)
	}
	var updated *w.IUIAutomationElement
	if e.BuildUpdatedCache(b.cache, &updated) < 0 {
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
	if e == nil {
		return Element{}, nil
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
	ReportStage(ctx, "capture_image")
	source, err := screenshot.Capture(int(r.X), int(r.Y), int(r.Width), int(r.Height))
	if err != nil {
		return Capture{}, Fail("native_error", err.Error())
	}
	ReportStage(ctx, "capture_encode")
	return encodeCapture(source, width)
}
func encodeCapture(source image.Image, width int) (Capture, error) {
	r := Rect{Width: float64(source.Bounds().Dx()), Height: float64(source.Bounds().Dy())}
	scale := math.Min(1, math.Min(float64(width)/r.Width, 2560/r.Height))
	ow, oh := max(1, int(r.Width*scale)), max(1, int(r.Height*scale))
	var img image.Image = source
	if scale < 1 {
		resized := image.NewRGBA(image.Rect(0, 0, ow, oh))
		draw.ApproxBiLinear.Scale(resized, resized.Bounds(), source, source.Bounds(), draw.Src, nil)
		img = resized
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, img, &jpeg.Options{Quality: 80}); err != nil {
		return Capture{}, err
	}
	return Capture{output.Bytes(), ow, oh}, nil
}
func (b *winBackend) CheckPoint(target Window, p Point, _ bool) error {
	_, err := checkWindowsPoint(&target, w.POINT{X: int32(p.X), Y: int32(p.Y)})
	return err
}

func checkWindowsPoint(target *Window, point w.POINT) (w.HWND, error) {
	var h w.HWND
	if target != nil {
		var err error
		h, err = hwnd(target.ID)
		if err != nil {
			return 0, err
		}
	}
	hit := w.WindowFromPoint(point)
	root := w.GetAncestor(hit, w.GA_ROOT)
	if target == nil || root == h {
		return root, nil
	}
	return 0, windowsPointFailure(*target, point, hit, root, "other_window_at_point", nil)
}

func (b *winBackend) ClickWindowHit(target *Window, p Point) (ClickHit, error) {
	point := w.POINT{X: int32(p.X), Y: int32(p.Y)}
	root, err := checkWindowsPoint(target, point)
	if err != nil {
		return ClickHit{}, err
	}
	result := ClickHit{DesktopPoint: Point{X: float64(point.X), Y: float64(point.Y)}}
	if target != nil {
		result.WindowID = target.ID
	} else if root != 0 {
		result.WindowID = windowID(root)
	}
	return result, nil
}

func (b *winBackend) ClickHit(ctx context.Context, p Point) ClickHit {
	point := w.POINT{X: int32(p.X), Y: int32(p.Y)}
	hit := w.WindowFromPoint(point)
	result := ClickHit{
		DesktopPoint: Point{X: float64(point.X), Y: float64(point.Y)},
		WindowID:     windowID(w.GetAncestor(hit, w.GA_ROOT)),
	}
	if hit == 0 {
		result.ControlError = "no window at point"
		return result
	}
	var element *w.IUIAutomationElement
	if err := hr(b.automation.ElementFromPointBuildCache(point, b.cache, &element)); err != nil {
		result.ControlError = err.Error()
		return result
	}
	if element == nil {
		result.ControlError = "UIA reported no element at point"
		return result
	}
	defer element.Release()
	if ctx.Err() != nil {
		return result
	}
	node, err := cachedNode(element, "", "")
	if err != nil {
		result.ControlError = err.Error()
		return result
	}
	if w.WindowFromPoint(point) != hit || !node.Bounds.Contains(result.DesktopPoint) {
		result.ControlError = "UIA element bounds or window hit could not be verified"
		return result
	}
	result.Control = &ControlSummary{Role: node.Role, Bounds: node.Bounds}
	if !node.Password {
		result.Control.Name = node.Name
	}
	return result
}

func windowsPointFailure(target Window, point w.POINT, hit, root w.HWND, reason string, cause error) error {
	detail := fmt.Sprintf("target_window_id=%s; desktop_point=(%d,%d)", target.ID, point.X, point.Y)
	if hit == 0 || root == 0 {
		return Fail("target_obscured", "reason=no_window_at_point; "+detail)
	}
	owner, _ := w.GetWindow(root, w.GW_OWNER)
	detail = fmt.Sprintf("reason=%s; %s; hit_window_id=%s; hit_root_window_id=%s; hit_owner_window_id=%s", reason, detail, windowID(hit), windowID(root), windowID(owner))
	if cause != nil {
		detail += "; " + cause.Error()
	}
	return Fail("target_obscured", detail)
}

func ownedByWindow(popup, target w.HWND, targetPID uint32) bool {
	for h, depth := popup, 0; h != 0 && depth < 64; depth++ {
		var pid uint32
		if w.GetWindowThreadProcessId(h, &pid) == 0 || pid != targetPID {
			return false
		}
		if h == target {
			return true
		}
		h, _ = w.GetWindow(h, w.GW_OWNER)
	}
	return false
}

func (b *winBackend) ElementClickPoint(ctx context.Context, target Window, e Element) (Point, error) {
	p := e.Bounds.Center()
	h, err := hwnd(target.ID)
	if err != nil {
		return Point{}, err
	}
	point := w.POINT{X: int32(p.X), Y: int32(p.Y)}
	hit := w.WindowFromPoint(point)
	root := w.GetAncestor(hit, w.GA_ROOT)
	if root == h {
		if !target.Bounds.Contains(p) {
			return Point{}, Fail("stale_snapshot", "element is outside window")
		}
		return p, nil
	}
	if !ownedByWindow(root, h, uint32(target.PID)) {
		return Point{}, windowsPointFailure(target, point, hit, root, "other_window_at_point", nil)
	}
	var rect w.RECT
	if ok, code := w.GetWindowRect(root, &rect); ok == 0 {
		return Point{}, windowsPointFailure(target, point, hit, root, "popup_bounds_unverified", code)
	}
	if e.Bounds.X < float64(rect.Left) || e.Bounds.Y < float64(rect.Top) || e.Bounds.X+e.Bounds.Width > float64(rect.Right) || e.Bounds.Y+e.Bounds.Height > float64(rect.Bottom) {
		return Point{}, windowsPointFailure(target, point, hit, root, "element_outside_popup", nil)
	}
	matched, err := b.popupElementAtPoint(ctx, point, b.elements[e.ID])
	if contextErr := ctx.Err(); contextErr != nil {
		return Point{}, contextErr
	}
	if err != nil {
		return Point{}, windowsPointFailure(target, point, hit, root, "popup_element_unverified", err)
	}
	if !matched {
		return Point{}, windowsPointFailure(target, point, hit, root, "popup_element_mismatch", nil)
	}
	// UIA calls can block while the popup moves, closes, or loses foreground.
	current := w.WindowFromPoint(point)
	currentRoot := w.GetAncestor(current, w.GA_ROOT)
	if current != hit || currentRoot != root || !ownedByWindow(root, h, uint32(target.PID)) {
		return Point{}, windowsPointFailure(target, point, current, currentRoot, "popup_hit_changed", nil)
	}
	if foreground := w.GetForegroundWindow(); foreground != h {
		return Point{}, Fail("not_active", fmt.Sprintf("target_window_id=%s; foreground_window_id=%s", target.ID, windowID(foreground)))
	}
	return p, nil
}

func (b *winBackend) popupElementAtPoint(ctx context.Context, point w.POINT, expected *w.IUIAutomationElement) (bool, error) {
	var hit *w.IUIAutomationElement
	if err := hr(b.automation.ElementFromPoint(point, &hit)); err != nil {
		return false, err
	}
	if hit == nil {
		return false, errors.New("UIA reported no element at the popup point")
	}
	defer hit.Release()
	// Hit testing can return a decorative child omitted by the observed control view.
	var control *w.IUIAutomationElement
	if err := hr(b.walker.NormalizeElement(hit, &control)); err != nil {
		return false, err
	}
	if control == nil {
		return false, errors.New("UIA reported no control at the popup point")
	}
	for depth := 0; control != nil; depth++ {
		defer control.Release()
		if err := ctx.Err(); err != nil {
			return false, err
		}
		var same w.BOOL
		if err := hr(b.automation.CompareElements(expected, control, &same)); err != nil || same != 0 {
			return same != 0, err
		}
		// A button can expose its icon or text as a separate UIA element.
		// Stop at the first control that could handle the action itself.
		var kind w.UIA_CONTROLTYPE_ID
		if err := hr(control.Get_CurrentControlType(&kind)); err != nil {
			return false, err
		}
		if (kind != w.UIA_ImageControlTypeId && kind != w.UIA_TextControlTypeId) || depth == 63 {
			break
		}
		var parent *w.IUIAutomationElement
		if err := hr(b.walker.GetParentElement(control, &parent)); err != nil {
			return false, err
		}
		control = parent
	}
	return false, nil
}
func (b *winBackend) Recognize(context.Context, Capture, int) ([]RecognizedText, bool, error) {
	return nil, false, Fail("unsupported_operation", "OCR is available on macOS; this Windows distribution has no MSIX package identity")
}
func (b *winBackend) CaptureWindow(ctx context.Context, target Window, region Rect, width int) (Capture, error) {
	if err := ctx.Err(); err != nil {
		return Capture{}, err
	}
	h, err := hwnd(target.ID)
	if err != nil {
		return Capture{}, err
	}
	var rect w.RECT
	if ok, _ := w.GetWindowRect(h, &rect); ok == 0 {
		return Capture{}, Fail("native_error", "GetWindowRect failed")
	}
	bounds := winRect(rect)
	if bounds != target.Bounds {
		return Capture{}, Fail("stale_snapshot", "window geometry changed")
	}
	wid, height := int(rect.Right-rect.Left), int(rect.Bottom-rect.Top)
	if wid <= 0 || height <= 0 || int64(wid)*int64(height) > 40_000_000 {
		return Capture{}, Fail("invalid_argument", "window exceeds 40 million source pixels")
	}
	dc := w.CreateCompatibleDC(0)
	if dc == 0 {
		return Capture{}, Fail("native_error", "CreateCompatibleDC failed")
	}
	defer w.DeleteDC(dc)
	info := w.BITMAPINFO{BmiHeader: w.BITMAPINFOHEADER{BiSize: uint32(unsafe.Sizeof(w.BITMAPINFOHEADER{})), BiWidth: int32(wid), BiHeight: -int32(height), BiPlanes: 1, BiBitCount: 32, BiCompression: w.BI_RGB}}
	var bits unsafe.Pointer
	bitmap, code := w.CreateDIBSection(dc, &info, w.DIB_RGB_COLORS, unsafe.Pointer(&bits), 0, 0)
	if bitmap == 0 {
		return Capture{}, Fail("native_error", fmt.Sprint(code))
	}
	defer w.DeleteObject(w.HGDIOBJ(bitmap))
	previous := w.SelectObject(dc, w.HGDIOBJ(bitmap))
	defer w.SelectObject(dc, previous)
	ReportStage(ctx, "capture_image")
	if w.PrintWindow(h, dc, w.PRINT_WINDOW_FLAGS(w.PW_RENDERFULLCONTENT)) == 0 {
		return Capture{}, Fail("native_error", "PrintWindow failed; no screen fallback was used")
	}
	if err := ctx.Err(); err != nil {
		return Capture{}, err
	}
	crop := image.Rect(int(region.X-bounds.X), int(region.Y-bounds.Y), int(region.X-bounds.X+region.Width), int(region.Y-bounds.Y+region.Height))
	if crop.Empty() || !crop.In(image.Rect(0, 0, wid, height)) {
		return Capture{}, Fail("invalid_argument", "capture region is outside window")
	}
	pixels := unsafe.Slice((*byte)(bits), wid*height*4)
	source := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	for y := 0; y < crop.Dy(); y++ {
		for x := 0; x < crop.Dx(); x++ {
			i, j := ((y+crop.Min.Y)*wid+x+crop.Min.X)*4, y*source.Stride+x*4
			source.Pix[j], source.Pix[j+1], source.Pix[j+2], source.Pix[j+3] = pixels[i+2], pixels[i+1], pixels[i], 255
		}
	}
	ReportStage(ctx, "capture_encode")
	return encodeCapture(source, width)
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
func (b *winBackend) Input(ctx context.Context, op string, in Input) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(in.HoldKeys) > 0 {
		downs, ups := []w.INPUT{}, []w.INPUT{}
		for _, k := range in.HoldKeys {
			flags := w.KEYBD_EVENT_FLAGS(0)
			if k == "meta" {
				flags = w.KEYEVENTF_EXTENDEDKEY
			}
			downs = append(downs, keyboard(keyCode(k), 0, flags))
			ups = append([]w.INPUT{keyboard(keyCode(k), 0, flags|w.KEYEVENTF_KEYUP)}, ups...)
		}
		defer func() { result = errors.Join(result, send(ups)) }()
		if err := send(downs); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	switch op {
	case "move":
		return move(in.Point)
	case "type":
		chars := utf16.Encode([]rune(in.Text))
		for start := 0; start < len(chars); {
			if err := ctx.Err(); err != nil {
				return err
			}
			events := make([]w.INPUT, 0, 128)
			for start < len(chars) && len(events) < 128 {
				c := chars[start]
				start++
				if c == '\r' || c == '\n' {
					if c == '\r' && start < len(chars) && chars[start] == '\n' {
						start++
					}
					// VK_PACKET control characters are not Enter key events in all editors.
					events = append(events, keyboard(w.VK_RETURN, 0, 0), keyboard(w.VK_RETURN, 0, w.KEYEVENTF_KEYUP))
				} else {
					events = append(events, keyboard(0, c, w.KEYEVENTF_UNICODE), keyboard(0, c, w.KEYEVENTF_UNICODE|w.KEYEVENTF_KEYUP))
				}
			}
			if err := send(events); err != nil {
				ups := make([]w.INPUT, 0, len(events)/2)
				for i := len(events) - 1; i >= 0; i -= 2 {
					ups = append(ups, events[i])
				}
				return errors.Join(err, send(ups))
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
		if err := move(in.Path[0]); err != nil {
			return err
		}
		if err := send([]w.INPUT{mouse(w.MOUSEEVENTF_LEFTDOWN, 0)}); err != nil {
			return errors.Join(err, send([]w.INPUT{mouse(w.MOUSEEVENTF_LEFTUP, 0)}))
		}
		_, result := moveDrag(ctx, in, move)
		return errors.Join(result, send([]w.INPUT{mouse(w.MOUSEEVENTF_LEFTUP, 0)}))
	}
	return Fail("invalid_argument", op)
}

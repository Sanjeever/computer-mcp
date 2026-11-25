package desktop

import (
	"syscall"
	"unsafe"

	w "github.com/zzl/go-win32api/v2/win32"
)

func (b *winBackend) ActiveMenu(owner Window) (Window, *Rect) {
	h, err := hwnd(owner.ID)
	if err != nil {
		return Window{}, nil
	}
	var pid, foregroundPID uint32
	w.GetWindowThreadProcessId(h, &pid)
	foreground := w.GetForegroundWindow()
	thread := w.GetWindowThreadProcessId(foreground, &foregroundPID)
	if thread == 0 || pid != foregroundPID {
		return Window{}, nil
	}
	menuClass := w.StrToPwstr("#32768")
	var class [32]uint16
	w.GetClassNameW(foreground, &class[0], int32(len(class)))
	style, _ := w.GetWindowLongPtrW(foreground, w.GWL_STYLE)
	titleLength, _ := w.GetWindowTextLengthW(foreground)
	if foreground != h && style&uintptr(w.WS_POPUP) != 0 && titleLength == 0 && syscall.UTF16ToString(class[:]) != "#32768" {
		// Ribbon and other application-rendered menus can take foreground as
		// titleless owned popups without entering the Win32 menu loop.
		return menuWindow(foreground, owner, ownedByWindow(foreground, h, pid))
	}
	var info w.GUITHREADINFO
	info.CbSize = uint32(unsafe.Sizeof(info))
	if ok, _ := w.GetGUIThreadInfo(thread, &info); ok == 0 || info.Flags&(w.GUI_INMENUMODE|w.GUI_POPUPMENUMODE|w.GUI_SYSTEMMENUMODE) == 0 {
		return Window{}, nil
	}
	verified := ownedByWindow(w.GetAncestor(info.HwndMenuOwner, w.GA_ROOT), h, pid)
	// Find the frontmost visible native menu on this GUI thread. Win32 menu
	// windows may not have a GW_OWNER even when the thread has a menu owner.
	var candidate w.HWND
	for range 64 {
		candidate, _ = w.FindWindowExW(0, candidate, menuClass, nil)
		if candidate == 0 {
			break
		}
		if w.GetWindowThreadProcessId(candidate, nil) == thread && w.IsWindowVisible(candidate) != 0 {
			return menuWindow(candidate, owner, verified)
		}
	}
	return Window{}, nil
}

func menuWindow(h w.HWND, owner Window, verified bool) (Window, *Rect) {
	var rect w.RECT
	if ok, _ := w.GetWindowRect(h, &rect); ok == 0 || w.IsWindowVisible(h) == 0 {
		return Window{}, nil
	}
	bounds := winRect(rect)
	if !bounds.Valid() {
		return Window{}, nil
	}
	if verified {
		if id := windowID(h); id != "" {
			return Window{ID: id, PID: owner.PID, App: owner.App, Bounds: bounds, MouseActive: true, Menu: true, OwnerID: owner.ID}, nil
		}
	}
	return Window{}, &bounds
}

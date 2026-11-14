//go:build darwin || windows

package desktop

import (
	"runtime"
	"testing"
)

func TestNativeStatus(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	backend, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	status, err := backend.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Platform == "" {
		t.Fatal("missing native platform")
	}
	t.Logf("platform=%s accessibility=%t screen_capture=%t displays=%d", status.Platform, status.Accessibility, status.ScreenCapture, len(status.Displays))
}

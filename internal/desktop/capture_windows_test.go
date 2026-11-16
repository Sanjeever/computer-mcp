package desktop

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"runtime"
	"testing"
	"unsafe"

	w "github.com/zzl/go-win32api/v2/win32"
)

func TestWindowsCaptureDimensions(t *testing.T) {
	for _, test := range []struct {
		name                    string
		source                  image.Rectangle
		maxWidth, width, height int
	}{
		{"crop_without_upscaling", image.Rect(0, 0, 1100, 250), 1440, 1100, 250},
		{"scaled", image.Rect(0, 0, 1100, 250), 550, 550, 125},
		{"height_limit", image.Rect(0, 0, 300, 3000), 1440, 256, 2560},
		{"nonzero_origin", image.Rect(-50, 20, 1050, 270), 1440, 1100, 250},
	} {
		t.Run(test.name, func(t *testing.T) {
			capture, err := encodeCapture(image.NewGray(test.source), test.maxWidth)
			if err != nil {
				t.Fatal(err)
			}
			config, err := jpeg.DecodeConfig(bytes.NewReader(capture.Data))
			if err != nil {
				t.Fatal(err)
			}
			if capture.Width != test.width || capture.Height != test.height || config.Width != test.width || config.Height != test.height {
				t.Fatalf("capture metadata %dx%d, JPEG %dx%d; want %dx%d", capture.Width, capture.Height, config.Width, config.Height, test.width, test.height)
			}
		})
	}
}

func TestNativeWindowsCapture(t *testing.T) {
	b, fixture := startWindowsFixture(t)
	requireFixtureActive(t, fixture)
	text := w.StrToPwstr("Computer MCP capture 0123456789")
	fixtureMessage(t, fixture.Editor, w.WM_SETTEXT, 0, w.LPARAM(uintptr(unsafe.Pointer(text))))
	runtime.KeepAlive(text)
	var rect w.RECT
	if ok, code := w.GetWindowRect(fixture.Editor, &rect); ok == 0 {
		t.Fatal(code)
	}
	region := winRect(rect)
	region.Height = 250
	for _, mode := range []string{"window", "screen"} {
		for _, width := range []int{1440, 550} {
			t.Logf("%s_%d", mode, width)
			var capture Capture
			var err error
			if mode == "window" {
				capture, err = b.CaptureWindow(context.Background(), fixture.Window, region, width)
			} else {
				capture, err = b.Capture(context.Background(), region, width)
			}
			if err != nil {
				t.Fatal(err)
			}
			img, err := jpeg.Decode(bytes.NewReader(capture.Data))
			if err != nil {
				t.Fatal(err)
			}
			wantWidth := min(width, int(region.Width))
			wantHeight := int(region.Height) * wantWidth / int(region.Width)
			if capture.Width != wantWidth || capture.Height != wantHeight || img.Bounds().Dx() != wantWidth || img.Bounds().Dy() != wantHeight {
				t.Fatalf("capture dimensions: %dx%d, JPEG %v", capture.Width, capture.Height, img.Bounds())
			}
			dark := 0
			for y := 3; y < min(40, img.Bounds().Dy()); y++ {
				for x := 5; x < min(300, img.Bounds().Dx()); x++ {
					r, g, b, _ := img.At(x, y).RGBA()
					if r+g+b < 3*32768 {
						dark++
					}
				}
			}
			if dark < 20 {
				t.Fatal("fixture text is missing from captured pixels")
			}
		}
	}
}

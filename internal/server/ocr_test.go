package server

import (
	"context"
	"testing"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
)

func TestOCRCoordinatesAndNativeNames(t *testing.T) {
	s, b := setup()
	b.ocrSupported = true
	b.nodes = []desktop.Element{
		{ID: "row", Role: "AXRow", Bounds: desktop.Rect{X: 100, Y: 120, Width: 200, Height: 80}, Enabled: true},
		{ID: "named", Role: "AXButton", Name: "Native", Bounds: desktop.Rect{X: 100, Y: 220, Width: 200, Height: 80}},
		{ID: "secret", Role: "AXTextField", Password: true, Bounds: desktop.Rect{X: 100, Y: 320, Width: 200, Height: 80}},
	}
	b.ocr = []desktop.RecognizedText{
		{Text: "Visual label", Bounds: desktop.Rect{X: 110, Y: 15, Width: 50, Height: 15}, Confidence: .9},
		{Text: "Different", Bounds: desktop.Rect{X: 110, Y: 65, Width: 50, Height: 15}},
		{Text: "Secret", Bounds: desktop.Rect{X: 110, Y: 115, Width: 50, Height: 15}},
	}
	r := observe(context.Background(), s, ObserveArgs{Screenshot: true, OCR: true})
	if r.err != nil {
		t.Fatal(r.err)
	}
	if s.snapshot.Elements[0].OCRText != "Visual label" || s.snapshot.Elements[0].Name != "" {
		t.Fatalf("OCR did not augment row: %+v", s.snapshot.Elements[0])
	}
	if s.snapshot.Elements[1].Name != "Native" || s.snapshot.Elements[1].OCRText != "" || len(s.snapshot.OCR) != 2 {
		t.Fatal("native name replaced or password leaked")
	}
	if r := click(context.Background(), s, ClickArgs{SnapshotID: s.snapshot.ID, ElementID: "row"}); r.err != nil {
		t.Fatalf("OCR corrupted native identity validation: %v", r.err)
	}
}

func TestOCRRequiresCapabilityAndImage(t *testing.T) {
	s, _ := setup()
	ctx := context.Background()
	requireCode(t, observe(ctx, s, ObserveArgs{OCR: true}), "invalid_argument")
	requireCode(t, observe(ctx, s, ObserveArgs{Screenshot: true, OCR: true}), "unsupported_operation")
}

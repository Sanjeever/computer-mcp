package server

import (
	"context"
	"strings"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
)

func recognize(ctx context.Context, b desktop.Backend, snap *Snapshot, capture desktop.Capture, limit int) error {
	regions, truncated, err := b.Recognize(ctx, capture, limit)
	if err != nil {
		return err
	}
	snap.OCRRequested, snap.OCRTruncated = true, truncated
	for _, region := range regions {
		region.Text = clip(region.Text, 1024)
		bounds := desktop.Rect{
			X:      snap.Region.X + region.Bounds.X*snap.Region.Width/float64(capture.Width),
			Y:      snap.Region.Y + region.Bounds.Y*snap.Region.Height/float64(capture.Height),
			Width:  region.Bounds.Width * snap.Region.Width / float64(capture.Width),
			Height: region.Bounds.Height * snap.Region.Height / float64(capture.Height),
		}
		password, best := false, -1
		for i, e := range snap.Elements {
			if e.Password && e.Bounds.Intersects(bounds) {
				password = true
				break
			}
			if e.Password || e.Name != "" || e.Value != "" || !e.Bounds.Contains(bounds.Center()) {
				continue
			}
			switch strings.ToLower(e.Role) {
			case "axwindow", "axgroup", "axscrollarea", "axlayoutarea", "window", "group", "pane":
				continue
			}
			if best == -1 || e.Bounds.Width*e.Bounds.Height < snap.Elements[best].Bounds.Width*snap.Elements[best].Bounds.Height {
				best = i
			}
		}
		if password {
			continue
		}
		snap.OCR = append(snap.OCR, region)
		if best != -1 && snap.CaptureMode == "window" {
			snap.Elements[best].OCRText = clip(strings.TrimSpace(snap.Elements[best].OCRText+" "+region.Text), 512)
		}
	}
	return ctx.Err()
}

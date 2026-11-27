package server

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"time"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	transitionInterval = time.Second / 30
	transitionWidth    = 480
	transitionHeight   = 360
)

type TransitionOptions struct {
	Region   *desktop.Rect `json:"region,omitempty"`
	Duration int           `json:"duration_ms,omitempty"`
}

type transitionCapture struct {
	sampler  desktop.ScreenSampler
	region   desktop.Rect
	duration time.Duration
	baseline desktop.ScreenSample
}

type transitionFrame struct {
	TimeMS        float64       `json:"time_ms"`
	ChangedPixels int           `json:"changed_pixels"`
	ChangeBounds  *desktop.Rect `json:"change_bounds,omitempty"`
}

type transitionResult struct {
	Region     desktop.Rect      `json:"region"`
	TileWidth  int               `json:"tile_width"`
	TileHeight int               `json:"tile_height"`
	TargetFPS  int               `json:"target_fps"`
	DurationMS int64             `json:"requested_duration_ms"`
	Frames     []transitionFrame `json:"frames"`
	KeyFrames  []int             `json:"key_frames"`
}

func beginTransition(ctx context.Context, b desktop.Backend, source *Snapshot, options *TransitionOptions) (*transitionCapture, error) {
	sampler, ok := b.(desktop.ScreenSampler)
	if !ok {
		return nil, desktop.Fail("unsupported_operation", "capture_transition is currently available on Windows")
	}
	duration := options.Duration
	if duration == 0 {
		duration = 800
	}
	if duration < 100 || duration > 1500 {
		return nil, desktop.Fail("invalid_argument", "capture_transition.duration_ms: 100..1500")
	}
	region := source.Region
	if options.Region != nil {
		region = *options.Region
	}
	if !region.Valid() || math.Trunc(region.X) != region.X || math.Trunc(region.Y) != region.Y ||
		math.Trunc(region.Width) != region.Width || math.Trunc(region.Height) != region.Height || region.Width*region.Height > 2_073_600 {
		return nil, desktop.Fail("invalid_argument", "capture_transition.region requires whole desktop pixels and at most 2073600 pixels; provide a smaller region")
	}
	desktop.ReportStage(ctx, "transition_baseline")
	status, err := b.Status()
	if err != nil {
		return nil, err
	}
	for _, display := range status.Displays {
		if rectWithin(region, display.Bounds) {
			baseline, err := sampler.SampleScreen(ctx, region, transitionWidth, transitionHeight)
			if err != nil {
				return nil, err
			}
			return &transitionCapture{sampler, region, time.Duration(duration) * time.Millisecond, baseline}, nil
		}
	}
	return nil, desktop.Fail("invalid_argument", "capture_transition.region must fit on one display")
}

func (c *transitionCapture) collect(ctx context.Context, dispatchedAt time.Time) (*transitionResult, []byte, error) {
	desktop.ReportStage(ctx, "transition_capture")
	deadline := dispatchedAt.Add(c.duration)
	samples := []desktop.ScreenSample{c.baseline}
	var captureErr error
	for next := dispatchedAt; ; {
		if deadline.Before(next) {
			next = deadline
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
		if captureErr = ctx.Err(); captureErr != nil || !time.Now().Before(deadline) {
			break
		}
		sample, err := c.sampler.SampleScreen(ctx, c.region, transitionWidth, transitionHeight)
		if err != nil {
			captureErr = err
			break
		}
		if sample.CapturedAt.After(deadline) {
			break
		}
		if sample.Image.Bounds() != c.baseline.Image.Bounds() {
			captureErr = desktop.Fail("native_error", "screen sample dimensions changed during transition")
			break
		}
		samples = append(samples, sample)
		// Skip all scheduled times that elapsed during a slow sample.
		next = dispatchedAt.Add((time.Since(dispatchedAt)/transitionInterval + 1) * transitionInterval)
	}
	desktop.ReportStage(ctx, "transition_encode")
	result, image, err := c.render(samples, dispatchedAt)
	if err != nil {
		return result, nil, err
	}
	return result, image, captureErr
}

func (c *transitionCapture) afterInput(ctx context.Context, s *state, result mouseObservationResult, dispatchedAt time.Time) mouseObservationResult {
	result.Transition, result.transitionImage, result.transitionErr = c.collect(ctx, dispatchedAt)
	result.transitionPending = false
	return s.publishMouseResult(result)
}

func (c *transitionCapture) render(samples []desktop.ScreenSample, dispatchedAt time.Time) (*transitionResult, []byte, error) {
	bounds := c.baseline.Image.Bounds()
	result := &transitionResult{
		Region: c.region, TileWidth: bounds.Dx(), TileHeight: bounds.Dy(), TargetFPS: 30,
		DurationMS: c.duration.Milliseconds(), Frames: make([]transitionFrame, len(samples)),
	}
	for i, sample := range samples {
		frame := &result.Frames[i]
		frame.TimeMS = float64(sample.CapturedAt.Sub(dispatchedAt).Microseconds()) / 1000
		if i > 0 {
			frame.ChangedPixels, frame.ChangeBounds = transitionDifference(samples[i-1].Image, sample.Image)
		}
	}
	result.KeyFrames = selectTransitionFrames(result.Frames)
	width, height := result.TileWidth, result.TileHeight
	const labelHeight = 20
	const gap = 4
	width = max(92, width)
	columns := min(3, len(result.KeyFrames))
	rows := (len(result.KeyFrames) + columns - 1) / columns
	sheet := image.NewRGBA(image.Rect(0, 0, columns*(width+gap)-gap, rows*(height+labelHeight+gap)-gap))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.RGBA{25, 25, 25, 255}), image.Point{}, draw.Src)
	label := font.Drawer{Dst: sheet, Src: image.White, Face: basicfont.Face7x13}
	for tile, frame := range result.KeyFrames {
		x, y := (tile%columns)*(width+gap), (tile/columns)*(height+labelHeight+gap)
		label.Dot = fixed.P(x+3, y+14)
		label.DrawString(fmt.Sprintf("%+.1f ms", result.Frames[frame].TimeMS))
		origin := image.Pt(x, y+labelHeight)
		draw.Draw(sheet, image.Rectangle{Min: origin, Max: origin.Add(bounds.Size())}, samples[frame].Image, bounds.Min, draw.Src)
	}
	var jpegData bytes.Buffer
	err := jpeg.Encode(&jpegData, sheet, &jpeg.Options{Quality: 85})
	return result, jpegData.Bytes(), err
}

func transitionDifference(before, after *image.RGBA) (int, *desktop.Rect) {
	bounds := after.Bounds()
	minX, minY, maxX, maxY := bounds.Dx(), bounds.Dy(), -1, -1
	var changed int
	for y := range bounds.Dy() {
		for x := range bounds.Dx() {
			a, b := y*before.Stride+x*4, y*after.Stride+x*4
			r := int(before.Pix[a]) - int(after.Pix[b])
			g := int(before.Pix[a+1]) - int(after.Pix[b+1])
			blue := int(before.Pix[a+2]) - int(after.Pix[b+2])
			if max(r, g, blue) < 16 && min(r, g, blue) > -16 {
				continue
			}
			changed++
			minX, minY = min(minX, x), min(minY, y)
			maxX, maxY = max(maxX, x), max(maxY, y)
		}
	}
	if changed == 0 {
		return 0, nil
	}
	return changed, &desktop.Rect{X: float64(minX), Y: float64(minY), Width: float64(maxX - minX + 1), Height: float64(maxY - minY + 1)}
}

func selectTransitionFrames(frames []transitionFrame) []int {
	last := len(frames) - 1
	selected := []int{0}
	buckets := min(4, last-1)
	for bucket := range buckets {
		start := 1 + bucket*(last-1)/buckets
		end := 1 + (bucket+1)*(last-1)/buckets
		best := start
		for i := start + 1; i < end; i++ {
			if frames[i].ChangedPixels > frames[best].ChangedPixels {
				best = i
			}
		}
		selected = append(selected, best)
	}
	if last > 0 {
		selected = append(selected, last)
	}
	return selected
}

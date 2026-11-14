package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/Sanjeever/computer-mcp/internal/desktop"
	"github.com/gofrs/flock"
)

type Snapshot struct {
	ID          string            `json:"snapshot_id"`
	Window      *desktop.Window   `json:"window,omitempty"`
	Displays    []desktop.Display `json:"displays,omitempty"`
	Elements    []desktop.Element `json:"elements"`
	Truncated   bool              `json:"truncated"`
	Region      desktop.Rect      `json:"region"`
	ImageWidth  int               `json:"image_width,omitempty"`
	ImageHeight int               `json:"image_height,omitempty"`
	Created     time.Time         `json:"created_at"`
}
type response struct {
	value any
	image []byte
	err   error
}
type job struct {
	ctx    context.Context
	run    func(context.Context, *state) response
	result chan response
}
type Engine struct {
	jobs chan job
	done chan struct{}
	stop context.CancelFunc
	ctx  context.Context
}
type state struct {
	backend  desktop.Backend
	snapshot *Snapshot
}

func NewEngine(parent context.Context) *Engine {
	ctx, cancel := context.WithCancel(parent)
	e := &Engine{jobs: make(chan job, 16), done: make(chan struct{}), stop: cancel, ctx: ctx}
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(e.done)
		var b desktop.Backend
		defer func() {
			if b != nil {
				b.Close()
			}
		}()
		cache, cacheErr := os.UserCacheDir()
		lock := flock.New(filepath.Join(cache, "computer-mcp", "desktop.lock"))
		s := &state{}
		for {
			select {
			case <-ctx.Done():
				return
			case j := <-e.jobs:
				if err := j.ctx.Err(); err != nil {
					j.result <- response{err: err}
					continue
				}
				if cacheErr != nil {
					j.result <- response{err: cacheErr}
					continue
				}
				if err := os.MkdirAll(filepath.Dir(lock.Path()), 0700); err != nil {
					j.result <- response{err: err}
					continue
				}
				acquired, err := lock.TryLock()
				if err != nil {
					j.result <- response{err: err}
					continue
				}
				if !acquired {
					j.result <- response{err: desktop.Fail("busy", "")}
					continue
				}
				if b == nil {
					b, err = desktop.New()
					s.backend = b
				}
				var r response
				if err != nil {
					r.err = err
				} else {
					r = j.run(j.ctx, s)
				}
				if err = lock.Unlock(); err != nil && r.err == nil {
					r.err = err
				}
				j.result <- r
			}
		}
	}()
	return e
}
func (e *Engine) Close() {
	e.stop()
	select {
	case <-e.done:
	case <-time.After(2 * time.Second):
	}
}
func (e *Engine) call(ctx context.Context, f func(context.Context, *state) response) response {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	stop := context.AfterFunc(e.ctx, cancel)
	defer stop()
	j := job{ctx, f, make(chan response, 1)}
	select {
	case <-e.done:
		return response{err: context.Canceled}
	case <-ctx.Done():
		return response{err: ctx.Err()}
	case e.jobs <- j:
	default:
		return response{err: desktop.Fail("busy", "")}
	}
	select {
	case r := <-j.result:
		return r
	case <-ctx.Done():
		return response{err: ctx.Err()}
	case <-e.done:
		return response{err: context.Canceled}
	}
}
func findWindow(b desktop.Backend, id string, active bool) (desktop.Window, error) {
	windows, err := b.Windows()
	if err != nil {
		return desktop.Window{}, err
	}
	for _, w := range windows {
		if (id == "" && w.Active) || w.ID == id {
			if active && !w.Active {
				return w, desktop.Fail("not_active", id)
			}
			return w, nil
		}
	}
	return desktop.Window{}, desktop.Fail("window_not_found", id)
}
func (s *state) current(id string) (*Snapshot, error) {
	snap := s.snapshot
	if snap == nil || snap.ID != id || time.Since(snap.Created) > 60*time.Second {
		return nil, desktop.Fail("stale_snapshot", "")
	}
	if snap.Window == nil {
		status, err := s.backend.Status()
		if err != nil {
			return nil, err
		}
		if !slices.Equal(status.Displays, snap.Displays) {
			return nil, desktop.Fail("stale_snapshot", "display layout changed")
		}
		return snap, nil
	}
	w, err := findWindow(s.backend, snap.Window.ID, true)
	if err != nil {
		return nil, err
	}
	if w.PID != snap.Window.PID || w.Bounds != snap.Window.Bounds {
		return nil, desktop.Fail("stale_snapshot", "window geometry changed")
	}
	return snap, nil
}
func imagePoint(s *Snapshot, p desktop.Point) (desktop.Point, error) {
	if s.ImageWidth == 0 || s.ImageHeight == 0 || math.IsNaN(p.X) || math.IsNaN(p.Y) || p.X < 0 || p.Y < 0 || p.X >= float64(s.ImageWidth) || p.Y >= float64(s.ImageHeight) {
		return desktop.Point{}, desktop.Fail("invalid_argument", "point must lie inside the snapshot image")
	}
	return desktop.Point{X: s.Region.X + p.X*s.Region.Width/float64(s.ImageWidth), Y: s.Region.Y + p.Y*s.Region.Height/float64(s.ImageHeight)}, nil
}

type StatusArgs struct{}
type WindowArgs struct {
	Action   string        `json:"action"`
	WindowID string        `json:"window_id,omitempty"`
	Bounds   *desktop.Rect `json:"bounds,omitempty"`
}
type ObserveArgs struct {
	Desktop     bool          `json:"desktop,omitempty"`
	WindowID    string        `json:"window_id,omitempty"`
	Screenshot  bool          `json:"screenshot,omitempty"`
	MaxElements int           `json:"max_elements,omitempty"`
	MaxWidth    int           `json:"max_width,omitempty"`
	Region      *desktop.Rect `json:"region,omitempty"`
}
type ClickArgs struct {
	SnapshotID string         `json:"snapshot_id"`
	ElementID  string         `json:"element_id,omitempty"`
	Point      *desktop.Point `json:"point,omitempty"`
	Button     string         `json:"button,omitempty"`
	Count      int            `json:"count,omitempty"`
}
type TypeArgs struct {
	WindowID string `json:"window_id"`
	Text     string `json:"text"`
}
type KeyArgs struct {
	WindowID string   `json:"window_id"`
	Keys     []string `json:"keys"`
}
type ScrollArgs struct {
	SnapshotID string        `json:"snapshot_id"`
	Point      desktop.Point `json:"point"`
	DX         int           `json:"delta_x,omitempty"`
	DY         int           `json:"delta_y,omitempty"`
}
type DragArgs struct {
	SnapshotID string        `json:"snapshot_id"`
	From       desktop.Point `json:"from"`
	To         desktop.Point `json:"to"`
	Duration   int           `json:"duration_ms,omitempty"`
}
type WaitArgs struct {
	WindowID string `json:"window_id"`
	Text     string `json:"text,omitempty"`
	Timeout  int    `json:"timeout_ms,omitempty"`
}

func invalid(detail string) response { return response{err: desktop.Fail("invalid_argument", detail)} }
func delivered(err error) response {
	return response{value: map[string]any{"input_delivered": err == nil}, err: err}
}

func status(ctx context.Context, s *state, _ StatusArgs) response {
	v, err := s.backend.Status()
	return response{value: v, err: err}
}
func window(ctx context.Context, s *state, a WindowArgs) response {
	if a.Action == "list" {
		if a.WindowID != "" || a.Bounds != nil {
			return invalid("list does not accept window_id or bounds")
		}
		v, err := s.backend.Windows()
		return response{value: map[string]any{"windows": v}, err: err}
	}
	if a.WindowID == "" || (a.Action != "activate" && a.Action != "move") {
		return invalid("action must be list, activate or move; window_id is required for mutations")
	}
	if a.Action == "move" && (a.Bounds == nil || !a.Bounds.Valid()) {
		return invalid("valid bounds required")
	}
	if a.Action == "activate" && a.Bounds != nil {
		return invalid("activate does not accept bounds")
	}
	w, err := findWindow(s.backend, a.WindowID, false)
	if err != nil {
		return response{err: err}
	}
	r := desktop.Rect{}
	if a.Bounds != nil {
		r = *a.Bounds
	}
	s.snapshot = nil
	if err = s.backend.Window(w, a.Action, r); err != nil {
		return response{err: err}
	}
	w, err = findWindow(s.backend, a.WindowID, false)
	return response{value: w, err: err}
}
func observe(ctx context.Context, s *state, a ObserveArgs) response {
	if a.MaxElements == 0 {
		a.MaxElements = 200
	}
	if a.MaxWidth == 0 {
		a.MaxWidth = 1440
	}
	if a.MaxElements < 1 || a.MaxElements > 1000 || a.MaxWidth < 320 || a.MaxWidth > 2560 {
		return invalid("max_elements: 1..1000; max_width: 320..2560")
	}
	if a.Desktop {
		if a.WindowID != "" {
			return invalid("desktop mode does not accept window_id")
		}
		s.snapshot = nil
		status, err := s.backend.Status()
		if err != nil {
			return response{err: err}
		}
		if len(status.Displays) == 0 {
			return invalid("no active displays")
		}
		region := status.Displays[0].Bounds
		if a.Region != nil {
			region = *a.Region
		}
		if !region.Valid() {
			return invalid("invalid capture region")
		}
		intersects := false
		for _, d := range status.Displays {
			r := d.Bounds
			if region.X < r.X+r.Width && region.X+region.Width > r.X && region.Y < r.Y+r.Height && region.Y+region.Height > r.Y {
				intersects = true
				break
			}
		}
		if !intersects {
			return invalid("region is outside active displays")
		}
		capture, err := s.backend.Capture(ctx, region, a.MaxWidth)
		if err != nil {
			return response{err: err}
		}
		if err = ctx.Err(); err != nil {
			return response{err: err}
		}
		token := make([]byte, 16)
		if _, err = rand.Read(token); err != nil {
			return response{err: err}
		}
		snap := &Snapshot{ID: hex.EncodeToString(token), Displays: slices.Clone(status.Displays), Elements: []desktop.Element{}, Region: region, ImageWidth: capture.Width, ImageHeight: capture.Height, Created: time.Now().UTC()}
		s.snapshot = snap
		return response{value: snap, image: capture.Data}
	}
	if !a.Screenshot && (a.Region != nil || a.MaxWidth != 1440) {
		return invalid("region and max_width require screenshot=true")
	}
	s.snapshot = nil
	w, err := findWindow(s.backend, a.WindowID, false)
	if err != nil {
		return response{err: err}
	}
	if !w.Bounds.Valid() {
		return invalid("window has no visible bounds")
	}
	region := w.Bounds
	if a.Region != nil {
		region = *a.Region
		if !region.Valid() || region.X < w.Bounds.X || region.Y < w.Bounds.Y || region.X+region.Width > w.Bounds.X+w.Bounds.Width || region.Y+region.Height > w.Bounds.Y+w.Bounds.Height {
			return invalid("region must be inside window bounds")
		}
	}
	elements, truncated, err := s.backend.Observe(ctx, w, a.MaxElements)
	if err != nil {
		return response{err: err}
	}
	if elements == nil {
		elements = []desktop.Element{}
	}
	elements = slices.Clone(elements)
	for i := range elements {
		elements[i].Name = clip(elements[i].Name, 512)
		elements[i].Value = clip(elements[i].Value, 1024)
		if elements[i].Password {
			elements[i].Value = ""
		}
	}
	token := make([]byte, 16)
	if _, err = rand.Read(token); err != nil {
		return response{err: err}
	}
	snap := &Snapshot{ID: hex.EncodeToString(token), Window: &w, Elements: elements, Truncated: truncated, Region: region, Created: time.Now().UTC()}
	var data []byte
	if a.Screenshot {
		capture, err := s.backend.Capture(ctx, region, a.MaxWidth)
		if err != nil {
			return response{err: err}
		}
		data = capture.Data
		snap.ImageWidth = capture.Width
		snap.ImageHeight = capture.Height
	}
	if err = ctx.Err(); err != nil {
		return response{err: err}
	}
	s.snapshot = snap
	return response{value: snap, image: data}
}
func clip(v string, n int) string {
	r := []rune(v)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return v
}
func click(ctx context.Context, s *state, a ClickArgs) response {
	if (a.ElementID == "") == (a.Point == nil) {
		return invalid("provide exactly one of element_id or point")
	}
	if a.Button == "" {
		a.Button = "left"
	}
	if a.Count == 0 {
		a.Count = 1
	}
	if (a.Button != "left" && a.Button != "right" && a.Button != "middle") || a.Count < 1 || a.Count > 2 {
		return invalid("button: left/right/middle; count: 1/2")
	}
	snap, err := s.current(a.SnapshotID)
	if err != nil {
		return response{err: err}
	}
	var p desktop.Point
	if a.Point != nil {
		p, err = imagePoint(snap, *a.Point)
	} else {
		var old *desktop.Element
		for i := range snap.Elements {
			if snap.Elements[i].ID == a.ElementID {
				old = &snap.Elements[i]
				break
			}
		}
		if old == nil {
			return response{err: desktop.Fail("stale_snapshot", a.ElementID)}
		}
		var now desktop.Element
		now, err = s.backend.Resolve(a.ElementID)
		if err == nil && (now.Role != old.Role || clip(now.Name, 512) != old.Name || now.Bounds != old.Bounds || !now.Enabled || !now.Bounds.Valid()) {
			err = desktop.Fail("stale_snapshot", a.ElementID)
		}
		p = now.Bounds.Center()
		if err == nil && !snap.Window.Bounds.Contains(p) {
			err = desktop.Fail("stale_snapshot", "element is outside window")
		}
	}
	if err != nil {
		return response{err: err}
	}
	s.snapshot = nil
	return delivered(s.backend.Input(ctx, "click", desktop.Input{Point: p, Button: a.Button, Count: a.Count}))
}
func typeText(ctx context.Context, s *state, a TypeArgs) response {
	if a.WindowID == "" || len(a.Text) == 0 || len(a.Text) > 65536 || strings.ContainsRune(a.Text, 0) {
		return invalid("window_id and 1..65536 bytes of non-NUL text required")
	}
	if _, err := findWindow(s.backend, a.WindowID, true); err != nil {
		return response{err: err}
	}
	s.snapshot = nil
	return delivered(s.backend.Input(ctx, "type", desktop.Input{Text: a.Text}))
}
func key(ctx context.Context, s *state, a KeyArgs) response {
	if a.WindowID == "" || len(a.Keys) < 1 || len(a.Keys) > 5 {
		return invalid("window_id and 1..5 keys required")
	}
	seen := map[string]bool{}
	normal := 0
	for _, k := range a.Keys {
		if seen[k] || !desktop.ValidKey(k) {
			return invalid("unknown or repeated key: " + k)
		}
		seen[k] = true
		if k != "ctrl" && k != "alt" && k != "shift" && k != "meta" {
			normal++
		}
	}
	if normal != 1 {
		return invalid("a chord requires exactly one non-modifier key")
	}
	if a.Keys[len(a.Keys)-1] == "ctrl" || a.Keys[len(a.Keys)-1] == "alt" || a.Keys[len(a.Keys)-1] == "shift" || a.Keys[len(a.Keys)-1] == "meta" {
		return invalid("put modifiers before the final key")
	}
	if _, err := findWindow(s.backend, a.WindowID, true); err != nil {
		return response{err: err}
	}
	s.snapshot = nil
	return delivered(s.backend.Input(ctx, "key", desktop.Input{Keys: a.Keys}))
}
func scroll(ctx context.Context, s *state, a ScrollArgs) response {
	if a.DX < -100 || a.DX > 100 || a.DY < -100 || a.DY > 100 || (a.DX == 0 && a.DY == 0) {
		return invalid("nonzero wheel delta in -100..100 required")
	}
	snap, err := s.current(a.SnapshotID)
	if err != nil {
		return response{err: err}
	}
	p, err := imagePoint(snap, a.Point)
	if err != nil {
		return response{err: err}
	}
	s.snapshot = nil
	return delivered(s.backend.Input(ctx, "scroll", desktop.Input{Point: p, DX: a.DX, DY: a.DY}))
}
func drag(ctx context.Context, s *state, a DragArgs) response {
	if a.Duration == 0 {
		a.Duration = 500
	}
	if a.Duration < 100 || a.Duration > 3000 {
		return invalid("duration_ms: 100..3000")
	}
	snap, err := s.current(a.SnapshotID)
	if err != nil {
		return response{err: err}
	}
	p, err := imagePoint(snap, a.From)
	if err != nil {
		return response{err: err}
	}
	to, err := imagePoint(snap, a.To)
	if err != nil {
		return response{err: err}
	}
	s.snapshot = nil
	return delivered(s.backend.Input(ctx, "drag", desktop.Input{Point: p, To: to, Duration: a.Duration}))
}
func wait(ctx context.Context, s *state, a WaitArgs) response {
	if a.WindowID == "" {
		return invalid("window_id required")
	}
	if a.Timeout == 0 {
		a.Timeout = 5000
	}
	if a.Timeout < 1 || a.Timeout > 10000 || len(a.Text) > 1024 {
		return invalid("timeout_ms: 1..10000; text <=1024 bytes")
	}
	s.snapshot = nil
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, time.Duration(a.Timeout)*time.Millisecond)
	defer cancel()
	finished := func() response {
		if err := parent.Err(); err != nil {
			return response{err: err}
		}
		return response{value: map[string]any{"matched": false}}
	}
	for {
		if ctx.Err() != nil {
			return finished()
		}
		w, err := findWindow(s.backend, a.WindowID, false)
		if err == nil {
			if a.Text == "" {
				return response{value: map[string]any{"matched": true, "window": w}}
			}
			var nodes []desktop.Element
			nodes, _, err = s.backend.Observe(ctx, w, 500)
			if err == nil {
				for _, n := range nodes {
					if strings.Contains(n.Name, a.Text) || (!n.Password && strings.Contains(n.Value, a.Text)) {
						return response{value: map[string]any{"matched": true, "window": w}}
					}
				}
			}
		}
		var de *desktop.Error
		if ctx.Err() != nil {
			return finished()
		}
		if err != nil && (!errors.As(err, &de) || de.Code != "window_not_found") {
			return response{err: err}
		}
		select {
		case <-ctx.Done():
			return finished()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
func errorCode(err error) (string, string) {
	var de *desktop.Error
	if errors.As(err, &de) {
		return de.Code, de.Detail
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout", ""
	}
	if errors.Is(err, context.Canceled) {
		return "canceled", ""
	}
	return "native_error", fmt.Sprint(err)
}

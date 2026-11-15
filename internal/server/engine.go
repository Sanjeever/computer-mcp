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
	ID           string                   `json:"snapshot_id"`
	Window       *desktop.Window          `json:"window,omitempty"`
	Displays     []desktop.Display        `json:"displays,omitempty"`
	Elements     []desktop.Element        `json:"elements"`
	Truncated    bool                     `json:"truncated"`
	Region       desktop.Rect             `json:"region"`
	ImageWidth   int                      `json:"image_width,omitempty"`
	ImageHeight  int                      `json:"image_height,omitempty"`
	Created      time.Time                `json:"created_at"`
	Scope        desktop.ObserveOptions   `json:"-"`
	CaptureMode  string                   `json:"capture_mode,omitempty"`
	OCR          []desktop.RecognizedText `json:"ocr,omitempty"`
	OCRTruncated bool                     `json:"ocr_truncated,omitempty"`
	OCRRequested bool                     `json:"-"`
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
	previous *Snapshot
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
			if active && w.BlockedBy != "" {
				return w, desktop.Fail("window_blocked", w.BlockedBy)
			}
			if active && !w.Active {
				detail := "target=" + id
				for _, current := range windows {
					if current.Active {
						detail += fmt.Sprintf("; active=%s (%s, %s)", current.ID, current.App, current.Title)
					}
				}
				return w, desktop.Fail("not_active", detail)
			}
			return w, nil
		}
	}
	return desktop.Window{}, desktop.Fail("window_not_found", id)
}
func (s *state) current(id string) (*Snapshot, error) {
	snap := s.snapshot
	if snap == nil {
		return nil, desktop.Fail("stale_snapshot", "no current snapshot; input, wait, window mutations and failed observations invalidate snapshots")
	}
	if snap.ID != id {
		target := "desktop"
		if snap.Window != nil {
			target = "window_id=" + snap.Window.ID
		}
		return nil, desktop.Fail("stale_snapshot", "latest observation is "+target+"; earlier snapshots are invalid")
	}
	if time.Since(snap.Created) > 60*time.Second {
		return nil, desktop.Fail("stale_snapshot", "snapshot exceeded its 60-second lifetime")
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
	var de *desktop.Error
	if err != nil && !(errors.As(err, &de) && de.Code == "not_active" && w.MouseActive) {
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
	ByApp    string        `json:"by_app,omitempty"`
	ByTitle  string        `json:"by_title,omitempty"`
	Timeout  int           `json:"timeout_ms,omitempty"`
	Bounds   *desktop.Rect `json:"bounds,omitempty"`
}
type ObserveArgs struct {
	Desktop         bool          `json:"desktop,omitempty"`
	WindowID        string        `json:"window_id,omitempty"`
	Screenshot      bool          `json:"screenshot,omitempty"`
	MaxElements     int           `json:"max_elements,omitempty"`
	MaxWidth        int           `json:"max_width,omitempty"`
	Region          *desktop.Rect `json:"region,omitempty"`
	RootID          string        `json:"root_id,omitempty"`
	SnapshotID      string        `json:"snapshot_id,omitempty"`
	Text            string        `json:"text,omitempty"`
	Role            string        `json:"role,omitempty"`
	Compact         bool          `json:"compact,omitempty"`
	MaxDepth        int           `json:"max_depth,omitempty"`
	SinceSnapshotID string        `json:"since_snapshot_id,omitempty"`
	CaptureMode     string        `json:"capture_mode,omitempty"`
	OCR             bool          `json:"ocr,omitempty"`
}
type ClickArgs struct {
	SnapshotID string         `json:"snapshot_id"`
	ElementID  string         `json:"element_id,omitempty"`
	Point      *desktop.Point `json:"point,omitempty"`
	Button     string         `json:"button,omitempty"`
	Count      int            `json:"count,omitempty"`
}
type TypeArgs struct {
	WindowID     string `json:"window_id"`
	Text         string `json:"text"`
	AutoActivate bool   `json:"auto_activate,omitempty"`
	FocusName    string `json:"focus_name,omitempty"`
	Timeout      int    `json:"timeout_ms,omitempty"`
}
type KeyArgs struct {
	WindowID     string   `json:"window_id"`
	Keys         []string `json:"keys"`
	AutoActivate bool     `json:"auto_activate,omitempty"`
	FocusName    string   `json:"focus_name,omitempty"`
	Timeout      int      `json:"timeout_ms,omitempty"`
}
type ScrollArgs struct {
	SnapshotID string        `json:"snapshot_id"`
	Point      desktop.Point `json:"point"`
	DX         int           `json:"delta_x,omitempty"`
	DY         int           `json:"delta_y,omitempty"`
}
type DragArgs struct {
	SnapshotID string          `json:"snapshot_id"`
	From       *desktop.Point  `json:"from,omitempty"`
	To         *desktop.Point  `json:"to,omitempty"`
	Path       []desktop.Point `json:"path,omitempty"`
	Duration   int             `json:"duration_ms,omitempty"`
}
type WaitArgs struct {
	WindowID  string `json:"window_id"`
	Text      string `json:"text,omitempty"`
	Timeout   int    `json:"timeout_ms,omitempty"`
	Active    bool   `json:"active,omitempty"`
	Focused   bool   `json:"focused,omitempty"`
	FocusName string `json:"focus_name,omitempty"`
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
	if a.Timeout < 0 || a.Timeout > 10000 || (a.Timeout != 0 && a.Action != "activate") {
		return invalid("timeout_ms: 1..10000, for activate only")
	}
	if a.WindowID != "" && (a.ByApp != "" || a.ByTitle != "") {
		return invalid("window_id cannot be combined with by_app or by_title")
	}
	if a.Action == "list" {
		if a.WindowID != "" || a.Bounds != nil {
			return invalid("list does not accept window_id or bounds")
		}
		v, err := s.backend.Windows()
		v = selectWindows(v, a.ByApp, a.ByTitle)
		return response{value: map[string]any{"windows": v}, err: err}
	}
	if (a.WindowID == "" && a.ByApp == "" && a.ByTitle == "") || (a.Action != "activate" && a.Action != "move") {
		return invalid("action must be list, activate or move; a window target is required for mutations")
	}
	if a.Action == "move" && (a.Bounds == nil || !a.Bounds.Valid()) {
		return invalid("valid bounds required")
	}
	if a.Action == "activate" && a.Bounds != nil {
		return invalid("activate does not accept bounds")
	}
	if a.WindowID == "" {
		all, err := s.backend.Windows()
		if err != nil {
			return response{err: err}
		}
		matches := selectWindows(all, a.ByApp, a.ByTitle)
		if len(matches) == 0 {
			return response{err: desktop.Fail("window_not_found", "selector matched no windows")}
		}
		if len(matches) != 1 {
			return response{err: desktop.Fail("ambiguous_window", fmt.Sprintf("%d windows match; use list with the same filters and select window_id", len(matches)))}
		}
		a.WindowID = matches[0].ID
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
	if a.Action == "activate" {
		w, err = activateWindow(ctx, s.backend, w, a.Timeout)
		return response{value: w, err: err}
	}
	if err = s.backend.Window(w, a.Action, r); err != nil {
		return response{err: err}
	}
	w, err = findWindow(s.backend, a.WindowID, false)
	return response{value: w, err: err}
}
func selectWindows(windows []desktop.Window, app, title string) []desktop.Window {
	result := make([]desktop.Window, 0, len(windows))
	for _, w := range windows {
		if app != "" && !strings.EqualFold(w.App, app) && !strings.EqualFold(w.AppID, app) {
			continue
		}
		if title != "" && !strings.Contains(strings.ToLower(w.Title), strings.ToLower(title)) {
			continue
		}
		result = append(result, w)
	}
	return result
}
func observe(ctx context.Context, s *state, a ObserveArgs) response {
	if a.OCR && !a.Screenshot && !a.Desktop {
		return invalid("ocr requires screenshot=true")
	}
	if a.OCR {
		capabilities, err := s.backend.Status()
		if err != nil {
			return response{err: err}
		}
		if !capabilities.OCR {
			return response{err: desktop.Fail("unsupported_operation", "OCR is unavailable on this platform")}
		}
	}
	if a.CaptureMode != "" && a.CaptureMode != "window" && a.CaptureMode != "screen" {
		return invalid("capture_mode: window or screen")
	}
	if a.Desktop && a.CaptureMode == "window" {
		return invalid("desktop mode requires screen capture")
	}
	if !a.Desktop && !a.Screenshot && a.CaptureMode != "" {
		return invalid("capture_mode requires screenshot=true")
	}
	if a.MaxDepth == 0 {
		a.MaxDepth = 64
	}
	if a.MaxDepth < 1 || a.MaxDepth > 64 || len(a.Text) > 1024 || len(a.Role) > 128 {
		return invalid("max_depth: 1..64; text <=1024 bytes; role <=128 bytes")
	}
	if (a.RootID == "") != (a.SnapshotID == "") {
		return invalid("root_id requires snapshot_id and vice versa")
	}
	if a.RootID != "" {
		if s.snapshot == nil || s.snapshot.ID != a.SnapshotID || time.Since(s.snapshot.Created) > time.Minute || s.snapshot.Window == nil {
			return response{err: desktop.Fail("stale_snapshot", "subtree root")}
		}
		if a.WindowID != "" && a.WindowID != s.snapshot.Window.ID {
			return invalid("subtree belongs to another window")
		}
		a.WindowID = s.snapshot.Window.ID
		found := false
		for _, e := range s.snapshot.Elements {
			if e.ID == a.RootID {
				found = true
				break
			}
		}
		if !found {
			return response{err: desktop.Fail("stale_snapshot", "subtree root")}
		}
	}
	if a.SinceSnapshotID != "" && (s.previous == nil || s.previous.ID != a.SinceSnapshotID) {
		return response{err: desktop.Fail("stale_snapshot", "diff base is not the latest observation")}
	}
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
		if a.RootID != "" || a.Text != "" || a.Role != "" || a.Compact || a.SinceSnapshotID != "" {
			return invalid("desktop mode does not accept semantic filters or diffs")
		}
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
		snap := &Snapshot{ID: hex.EncodeToString(token), Displays: slices.Clone(status.Displays), Elements: []desktop.Element{}, Region: region, ImageWidth: capture.Width, ImageHeight: capture.Height, Created: time.Now().UTC(), CaptureMode: "screen"}
		if a.OCR {
			if err := recognize(ctx, s.backend, snap, capture, a.MaxElements); err != nil {
				return response{err: err}
			}
		}
		s.snapshot = snap
		s.previous = snap
		return response{value: snap, image: capture.Data}
	}
	if !a.Screenshot && a.MaxWidth != 1440 {
		return invalid("max_width requires screenshot=true")
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
	options := desktop.ObserveOptions{Limit: a.MaxElements, RootID: a.RootID, Region: a.Region, Text: a.Text, Role: a.Role, Compact: a.Compact, MaxDepth: a.MaxDepth}
	if a.SinceSnapshotID != "" && (s.previous.Window == nil || s.previous.Window.ID != w.ID || !sameScope(s.previous.Scope, options) || s.previous.OCRRequested != a.OCR) {
		return invalid("diff requires the same window and observation filters")
	}
	elements, truncated, err := s.backend.Observe(ctx, w, options)
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
	snap := &Snapshot{ID: hex.EncodeToString(token), Window: &w, Elements: elements, Truncated: truncated, Region: region, Created: time.Now().UTC(), Scope: options}
	var data []byte
	if a.Screenshot {
		var capture desktop.Capture
		if a.CaptureMode == "" {
			a.CaptureMode = "window"
		}
		if a.CaptureMode == "window" {
			capture, err = s.backend.CaptureWindow(ctx, w, region, a.MaxWidth)
		} else {
			capture, err = s.backend.Capture(ctx, region, a.MaxWidth)
		}
		if err != nil {
			return response{err: err}
		}
		data = capture.Data
		snap.ImageWidth = capture.Width
		snap.ImageHeight = capture.Height
		snap.CaptureMode = a.CaptureMode
		if a.OCR {
			if err := recognize(ctx, s.backend, snap, capture, a.MaxElements); err != nil {
				return response{err: err}
			}
		}
	}
	if err = ctx.Err(); err != nil {
		return response{err: err}
	}
	s.snapshot = snap
	var value any = snap
	if a.SinceSnapshotID != "" {
		value = diffSnapshots(s.previous, snap)
	}
	s.previous = snap
	return response{value: value, image: data}
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
	if snap.Window != nil {
		if err := s.backend.CheckPoint(*snap.Window, p); err != nil {
			return response{err: err}
		}
	}
	s.snapshot = nil
	return delivered(s.backend.Input(ctx, "click", desktop.Input{Point: p, Button: a.Button, Count: a.Count}))
}
func typeText(ctx context.Context, s *state, a TypeArgs) response {
	if a.WindowID == "" || len(a.Text) == 0 || len(a.Text) > 65536 || strings.ContainsRune(a.Text, 0) {
		return invalid("window_id and 1..65536 bytes of non-NUL text required")
	}
	if err := prepareKeyboard(ctx, s, a.WindowID, a.AutoActivate, a.FocusName, a.Timeout); err != nil {
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
	if err := prepareKeyboard(ctx, s, a.WindowID, a.AutoActivate, a.FocusName, a.Timeout); err != nil {
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
	if snap.Window != nil {
		if err := s.backend.CheckPoint(*snap.Window, p); err != nil {
			return response{err: err}
		}
	}
	s.snapshot = nil
	return delivered(s.backend.Input(ctx, "scroll", desktop.Input{Point: p, DX: a.DX, DY: a.DY}))
}
func drag(ctx context.Context, s *state, a DragArgs) response {
	path := a.Path
	if path != nil {
		if a.From != nil || a.To != nil || len(path) < 2 || len(path) > 128 {
			return invalid("provide either from and to, or path with 2..128 image points")
		}
	} else {
		if a.From == nil || a.To == nil {
			return invalid("provide either from and to, or path with 2..128 image points")
		}
		path = []desktop.Point{*a.From, *a.To}
	}
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
	points := make([]desktop.Point, len(path))
	for i, point := range path {
		if err := ctx.Err(); err != nil {
			return response{err: err}
		}
		points[i], err = imagePoint(snap, point)
		if err != nil {
			return response{err: err}
		}
	}
	if snap.Window != nil {
		for _, point := range points {
			if err := ctx.Err(); err != nil {
				return response{err: err}
			}
			if err := s.backend.CheckPoint(*snap.Window, point); err != nil {
				return response{err: err}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return response{err: err}
	}
	s.snapshot = nil
	return delivered(s.backend.Input(ctx, "drag", desktop.Input{Path: points, Duration: a.Duration}))
}
func wait(ctx context.Context, s *state, a WaitArgs) response {
	if a.WindowID == "" {
		return invalid("window_id required")
	}
	if a.Timeout == 0 {
		a.Timeout = 5000
	}
	if a.Timeout < 1 || a.Timeout > 10000 || len(a.Text) > 1024 || len(a.FocusName) > 1024 {
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
		w, err := findWindow(s.backend, a.WindowID, a.Active || a.Focused || a.FocusName != "")
		ready := err == nil
		if ready && (a.Focused || a.FocusName != "") {
			var focused desktop.Element
			focused, err = s.backend.Focus(w)
			ready = err == nil && focused.Focused && (a.FocusName == "" || focused.Name == a.FocusName)
		}
		if ready {
			if a.Text == "" {
				return response{value: map[string]any{"matched": true, "window": w}}
			}
			var nodes []desktop.Element
			nodes, _, err = s.backend.Observe(ctx, w, desktop.ObserveOptions{Limit: 500, MaxDepth: 64, Text: a.Text})
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
		if err != nil && (!errors.As(err, &de) || (de.Code != "window_not_found" && de.Code != "not_active")) {
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

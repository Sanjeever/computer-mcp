package desktop

import (
	"context"
	"fmt"
	"math"
	"strings"
)

type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func (r Rect) Valid() bool {
	return finite(r.X) && finite(r.Y) && finite(r.Width) && finite(r.Height) && r.Width > 0 && r.Height > 0 && r.Width <= 32768 && r.Height <= 32768
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func (r Rect) Contains(p Point) bool {
	return p.X >= r.X && p.Y >= r.Y && p.X < r.X+r.Width && p.Y < r.Y+r.Height
}
func (r Rect) Center() Point { return Point{r.X + r.Width/2, r.Y + r.Height/2} }
func (r Rect) Intersects(other Rect) bool {
	return r.X < other.X+other.Width && r.X+r.Width > other.X && r.Y < other.Y+other.Height && r.Y+r.Height > other.Y
}

type ObserveOptions struct {
	Limit    int    `json:"limit"`
	RootID   string `json:"root_id,omitempty"`
	Region   *Rect  `json:"region,omitempty"`
	Text     string `json:"text,omitempty"`
	Role     string `json:"role,omitempty"`
	Compact  bool   `json:"compact,omitempty"`
	MaxDepth int    `json:"max_depth"`
}

func (o ObserveOptions) Includes(e Element) bool {
	if o.Region != nil && !o.Region.Intersects(e.Bounds) {
		return false
	}
	if o.Role != "" && !strings.EqualFold(e.Role, o.Role) {
		return false
	}
	if o.Text != "" && !strings.Contains(e.Name, o.Text) && (e.Password || !strings.Contains(e.Value, o.Text)) {
		return false
	}
	if o.Compact && e.Name == "" && e.Value == "" && !e.Focused {
		switch strings.ToLower(e.Role) {
		case "group", "pane", "axgroup", "axlayoutarea", "axscrollarea":
			return false
		}
	}
	return true
}

type Window struct {
	ID          string `json:"window_id"`
	PID         int    `json:"pid"`
	App         string `json:"app"`
	AppID       string `json:"app_id,omitempty"`
	Title       string `json:"title"`
	Bounds      Rect   `json:"bounds"`
	Active      bool   `json:"active"`
	MouseActive bool   `json:"mouse_active,omitempty"`
	Menu        bool   `json:"menu,omitempty"`
	BlockedBy   string `json:"blocked_by,omitempty"`
	OwnerID     string `json:"-"`
}
type Element struct {
	ID       string `json:"element_id"`
	Parent   string `json:"parent_id,omitempty"`
	Role     string `json:"role"`
	Name     string `json:"name,omitempty"`
	Value    string `json:"value,omitempty"`
	Bounds   Rect   `json:"bounds"`
	Enabled  bool   `json:"enabled"`
	Focused  bool   `json:"focused"`
	Password bool   `json:"password,omitempty"`
	OCRText  string `json:"ocr_text,omitempty"`
}
type Display struct {
	ID     string `json:"display_id"`
	Bounds Rect   `json:"bounds"`
}
type Status struct {
	Platform      string    `json:"platform"`
	Accessibility bool      `json:"accessibility"`
	ScreenCapture bool      `json:"screen_capture"`
	OCR           bool      `json:"ocr"`
	Displays      []Display `json:"displays"`
}
type Capture struct {
	Data   []byte
	Width  int
	Height int
}
type RecognizedText struct {
	Text       string  `json:"text"`
	Bounds     Rect    `json:"bounds"`
	Confidence float64 `json:"confidence"`
}
type Input struct {
	Point    Point
	Path     []Point
	Button   string
	Count    int
	Text     string
	Keys     []string
	HoldKeys []string
	DX       int
	DY       int
	Duration int
}
type Backend interface {
	Status() (Status, error)
	Windows() ([]Window, error)
	Observe(context.Context, Window, ObserveOptions) ([]Element, bool, error)
	Resolve(string) (Element, error)
	Focus(Window) (Element, error)
	Window(Window, string, Rect) error
	Input(context.Context, string, Input) error
	Capture(context.Context, Rect, int) (Capture, error)
	CaptureWindow(context.Context, Window, Rect, int) (Capture, error)
	CheckPoint(window Window, point Point, allowMenu bool) error
	ElementClickPoint(context.Context, Window, Element) (Point, error)
	Recognize(context.Context, Capture, int) ([]RecognizedText, bool, error)
	Close()
}

// MenuObserver finds a visible menu for an exact owner. A candidate without a
// verified identity can provide bounds for a caller-initiated desktop crop.
type MenuObserver interface {
	ActiveMenu(owner Window) (verified Window, suggestedRegion *Rect)
}

type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string       { return fmt.Sprintf("%s: %s", e.Code, e.Detail) }
func Fail(code, detail string) error { return &Error{code, detail} }

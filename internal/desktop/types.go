package desktop

import (
	"context"
	"fmt"
	"math"
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

type Window struct {
	ID     string `json:"window_id"`
	PID    int    `json:"pid"`
	App    string `json:"app"`
	Title  string `json:"title"`
	Bounds Rect   `json:"bounds"`
	Active bool   `json:"active"`
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
}
type Display struct {
	ID     string `json:"display_id"`
	Bounds Rect   `json:"bounds"`
}
type Status struct {
	Platform      string    `json:"platform"`
	Accessibility bool      `json:"accessibility"`
	ScreenCapture bool      `json:"screen_capture"`
	Displays      []Display `json:"displays"`
}
type Capture struct {
	Data   []byte
	Width  int
	Height int
}
type Input struct {
	Point    Point
	To       Point
	Button   string
	Count    int
	Text     string
	Keys     []string
	DX       int
	DY       int
	Duration int
}
type Backend interface {
	Status() (Status, error)
	Windows() ([]Window, error)
	Observe(context.Context, Window, int) ([]Element, bool, error)
	Resolve(string) (Element, error)
	Window(Window, string, Rect) error
	Input(context.Context, string, Input) error
	Capture(context.Context, Rect, int) (Capture, error)
	Close()
}
type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string       { return fmt.Sprintf("%s: %s", e.Code, e.Detail) }
func Fail(code, detail string) error { return &Error{code, detail} }

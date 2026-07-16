package platform

import (
	"context"
	"fmt"
	"runtime"

	"mouseshare/internal/domain"
)

type Event struct {
	Kind      string  `json:"kind"`
	X         float64 `json:"x,omitempty"`
	Y         float64 `json:"y,omitempty"`
	DeltaX    float64 `json:"deltaX,omitempty"`
	DeltaY    float64 `json:"deltaY,omitempty"`
	Button    int     `json:"button,omitempty"`
	KeyCode   uint16  `json:"keyCode,omitempty"`
	Modifiers uint64  `json:"modifiers,omitempty"`
}

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Rect struct {
	MinX   float64 `json:"minX"`
	MinY   float64 `json:"minY"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func (r Rect) MaxX() float64 { return r.MinX + r.Width }
func (r Rect) MaxY() float64 { return r.MinY + r.Height }
func (r Rect) Empty() bool   { return r.Width <= 0 || r.Height <= 0 }

func (r Rect) Clamp(p Point) Point {
	if r.Empty() {
		return p
	}
	if p.X < r.MinX {
		p.X = r.MinX
	}
	if p.X > r.MaxX()-1 {
		p.X = r.MaxX() - 1
	}
	if p.Y < r.MinY {
		p.Y = r.MinY
	}
	if p.Y > r.MaxY()-1 {
		p.Y = r.MaxY() - 1
	}
	return p
}

func (r Rect) String() string {
	return fmt.Sprintf("{x=%.0f y=%.0f w=%.0f h=%.0f}", r.MinX, r.MinY, r.Width, r.Height)
}

type Bridge interface {
	Name() string
	Permissions(context.Context) domain.PermissionState
	Bounds(context.Context) (Rect, error)
	CursorPosition(context.Context) (Point, error)
	StartCapture(context.Context, chan<- Event) error
	Inject(context.Context, Event) error
	StopCapture() error
}

func Current() Bridge {
	switch runtime.GOOS {
	case "darwin":
		return newDarwinBridge()
	case "windows":
		return newWindowsBridge()
	default:
		return newStubBridge(runtime.GOOS)
	}
}

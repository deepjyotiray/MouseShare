package platform

import (
	"context"
	"fmt"

	"mouseshare/internal/domain"
)

type stubBridge struct {
	name string
}

func newStubBridge(name string) Bridge {
	return &stubBridge{name: name}
}

func (b *stubBridge) Name() string { return b.name }

func (b *stubBridge) Permissions(context.Context) domain.PermissionState {
	return domain.PermissionState{
		Warnings: []string{
			fmt.Sprintf("%s input bridge is not implemented in this build", b.name),
		},
	}
}

func (b *stubBridge) Bounds(context.Context) (Rect, error) {
	return Rect{}, fmt.Errorf("%s screen bounds unavailable", b.name)
}

func (b *stubBridge) CursorPosition(context.Context) (Point, error) {
	return Point{}, fmt.Errorf("%s cursor position unavailable", b.name)
}

func (b *stubBridge) StartCapture(context.Context, chan<- Event) error { return nil }
func (b *stubBridge) Inject(context.Context, Event) error              { return nil }
func (b *stubBridge) StopCapture() error                               { return nil }

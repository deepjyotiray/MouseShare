package platform

import (
	"context"
	"runtime"

	"mouseshare/internal/domain"
)

type Event struct {
	Kind string         `json:"kind"`
	Data map[string]any `json:"data,omitempty"`
}

type Bridge interface {
	Name() string
	Permissions(context.Context) domain.PermissionState
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

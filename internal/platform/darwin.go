//go:build darwin

package platform

/*
#cgo darwin LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>
#include <stdbool.h>

extern void darwinEmitEvent(int kind, double x, double y, double dx, double dy, int button, unsigned short keyCode, unsigned long long flags);

enum {
	MSEventMouseMove = 1,
	MSEventLeftDown = 2,
	MSEventLeftUp = 3,
	MSEventRightDown = 4,
	MSEventRightUp = 5,
	MSEventOtherDown = 6,
	MSEventOtherUp = 7,
	MSEventScroll = 8,
	MSEventKeyDown = 9,
	MSEventKeyUp = 10,
	MSEventFlagsChanged = 11
};

static CFMachPortRef msTap = NULL;
static CFRunLoopSourceRef msSource = NULL;
static CFRunLoopRef msRunLoop = NULL;
static bool msSuppressLocalInput = false;

static CGEventRef msEventCallback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *refcon) {
	if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
		if (msTap != NULL) {
			CGEventTapEnable(msTap, true);
		}
		return event;
	}

	CGPoint loc = CGEventGetLocation(event);
	uint64_t flags = CGEventGetFlags(event);
	bool suppress = msSuppressLocalInput;
	switch (type) {
		case kCGEventMouseMoved:
		case kCGEventLeftMouseDragged:
		case kCGEventRightMouseDragged:
		case kCGEventOtherMouseDragged:
			darwinEmitEvent(
				MSEventMouseMove,
				loc.x,
				loc.y,
				(double)CGEventGetIntegerValueField(event, kCGMouseEventDeltaX),
				(double)CGEventGetIntegerValueField(event, kCGMouseEventDeltaY),
				0,
				0,
				flags
			);
			break;
		case kCGEventLeftMouseDown:
			darwinEmitEvent(MSEventLeftDown, loc.x, loc.y, 0, 0, 0, 0, flags);
			break;
		case kCGEventLeftMouseUp:
			darwinEmitEvent(MSEventLeftUp, loc.x, loc.y, 0, 0, 0, 0, flags);
			break;
		case kCGEventRightMouseDown:
			darwinEmitEvent(MSEventRightDown, loc.x, loc.y, 0, 0, 1, 0, flags);
			break;
		case kCGEventRightMouseUp:
			darwinEmitEvent(MSEventRightUp, loc.x, loc.y, 0, 0, 1, 0, flags);
			break;
		case kCGEventOtherMouseDown:
			darwinEmitEvent(MSEventOtherDown, loc.x, loc.y, 0, 0, (int)CGEventGetIntegerValueField(event, kCGMouseEventButtonNumber), 0, flags);
			break;
		case kCGEventOtherMouseUp:
			darwinEmitEvent(MSEventOtherUp, loc.x, loc.y, 0, 0, (int)CGEventGetIntegerValueField(event, kCGMouseEventButtonNumber), 0, flags);
			break;
		case kCGEventScrollWheel:
			darwinEmitEvent(MSEventScroll, loc.x, loc.y,
				(double)CGEventGetIntegerValueField(event, kCGScrollWheelEventPointDeltaAxis2),
				(double)CGEventGetIntegerValueField(event, kCGScrollWheelEventPointDeltaAxis1),
				0, 0, flags);
			break;
		case kCGEventKeyDown:
			darwinEmitEvent(MSEventKeyDown, 0, 0, 0, 0, 0, (unsigned short)CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode), flags);
			break;
		case kCGEventKeyUp:
			darwinEmitEvent(MSEventKeyUp, 0, 0, 0, 0, 0, (unsigned short)CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode), flags);
			break;
		case kCGEventFlagsChanged:
			darwinEmitEvent(MSEventFlagsChanged, 0, 0, 0, 0, 0, (unsigned short)CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode), flags);
			break;
		default:
			break;
	}
	if (suppress) {
		return NULL;
	}
	return event;
}

static bool msIsTrusted(void) {
	return AXIsProcessTrusted();
}

static CGRect msMainDisplayBounds(void) {
	return CGDisplayBounds(CGMainDisplayID());
}

static CGPoint msCursorPosition(void) {
	CGEventRef event = CGEventCreate(NULL);
	if (event == NULL) {
		return CGPointMake(0, 0);
	}
	CGPoint point = CGEventGetLocation(event);
	CFRelease(event);
	return point;
}

static bool msStartEventTap(void) {
	if (msTap != NULL) {
		return true;
	}
	msRunLoop = CFRunLoopGetCurrent();
	CGEventMask mask =
		CGEventMaskBit(kCGEventMouseMoved) |
		CGEventMaskBit(kCGEventLeftMouseDown) |
		CGEventMaskBit(kCGEventLeftMouseUp) |
		CGEventMaskBit(kCGEventRightMouseDown) |
		CGEventMaskBit(kCGEventRightMouseUp) |
		CGEventMaskBit(kCGEventOtherMouseDown) |
		CGEventMaskBit(kCGEventOtherMouseUp) |
		CGEventMaskBit(kCGEventLeftMouseDragged) |
		CGEventMaskBit(kCGEventRightMouseDragged) |
		CGEventMaskBit(kCGEventOtherMouseDragged) |
		CGEventMaskBit(kCGEventScrollWheel) |
		CGEventMaskBit(kCGEventKeyDown) |
		CGEventMaskBit(kCGEventKeyUp) |
		CGEventMaskBit(kCGEventFlagsChanged);
	msTap = CGEventTapCreate(
		kCGHIDEventTap,
		kCGHeadInsertEventTap,
		kCGEventTapOptionDefault,
		mask,
		msEventCallback,
		NULL
	);
	if (msTap == NULL) {
		return false;
	}
	msSource = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, msTap, 0);
	if (msSource == NULL) {
		CFRelease(msTap);
		msTap = NULL;
		return false;
	}
	CFRunLoopAddSource(msRunLoop, msSource, kCFRunLoopCommonModes);
	CGEventTapEnable(msTap, true);
	return true;
}

static void msRunEventTap(void) {
	CFRunLoopRun();
	if (msSource != NULL && msRunLoop != NULL) {
		CFRunLoopRemoveSource(msRunLoop, msSource, kCFRunLoopCommonModes);
		CFRelease(msSource);
		msSource = NULL;
	}
	if (msTap != NULL) {
		CFMachPortInvalidate(msTap);
		CFRelease(msTap);
		msTap = NULL;
	}
	msRunLoop = NULL;
}

static void msStopEventTap(void) {
	if (msRunLoop != NULL) {
		CFRunLoopStop(msRunLoop);
	}
}

static void msPostMouse(int eventType, int button, double x, double y, uint64_t flags) {
	CGEventRef event = CGEventCreateMouseEvent(NULL, eventType, CGPointMake(x, y), (CGMouseButton)button);
	if (event == NULL) {
		return;
	}
	CGEventSetFlags(event, flags);
	CGEventPost(kCGHIDEventTap, event);
	CFRelease(event);
}

static void msPostScroll(double dx, double dy) {
	CGEventRef event = CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitPixel, 2, (int32_t)dy, (int32_t)dx);
	if (event == NULL) {
		return;
	}
	CGEventPost(kCGHIDEventTap, event);
	CFRelease(event);
}

static void msPostKey(unsigned short keyCode, bool down, uint64_t flags) {
	CGEventRef event = CGEventCreateKeyboardEvent(NULL, (CGKeyCode)keyCode, down);
	if (event == NULL) {
		return;
	}
	CGEventSetFlags(event, flags);
	CGEventPost(kCGHIDEventTap, event);
	CFRelease(event);
}

static void msHideAndDetachCursor(double x, double y) {
	CGAssociateMouseAndMouseCursorPosition(false);
	CGWarpMouseCursorPosition(CGPointMake(x, y));
	CGDisplayHideCursor(kCGNullDirectDisplay);
}

static void msShowAndAttachCursor(double x, double y) {
	CGAssociateMouseAndMouseCursorPosition(true);
	CGWarpMouseCursorPosition(CGPointMake(x, y));
	CGDisplayShowCursor(kCGNullDirectDisplay);
}

static void msSetSuppressLocalInput(bool suppress) {
	msSuppressLocalInput = suppress;
}
*/
import "C"

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"mouseshare/internal/domain"
)

const (
	darwinMouseMove = iota + 1
	darwinLeftDown
	darwinLeftUp
	darwinRightDown
	darwinRightUp
	darwinOtherDown
	darwinOtherUp
	darwinScroll
	darwinKeyDown
	darwinKeyUp
	darwinFlagsChanged
)

var (
	darwinCaptureMu sync.RWMutex
	darwinCaptureCh chan<- Event
)

type darwinBridge struct {
	mu      sync.Mutex
	running bool
	anchor  Point
	locked  bool
}

func newDarwinBridge() Bridge {
	return &darwinBridge{}
}

func (b *darwinBridge) Name() string { return "macos" }

func (b *darwinBridge) Permissions(context.Context) domain.PermissionState {
	trusted := bool(C.msIsTrusted())
	state := domain.PermissionState{
		Accessibility: trusted,
		InputCapture:  trusted,
		ScreenAccess:  true,
	}
	if !trusted {
		state.Warnings = append(state.Warnings, "Grant Accessibility access in System Settings > Privacy & Security > Accessibility.")
	}
	return state
}

func (b *darwinBridge) Bounds(ctx context.Context) (Rect, error) {
	bounds := C.msMainDisplayBounds()
	return Rect{
		MinX:   float64(bounds.origin.x),
		MinY:   float64(bounds.origin.y),
		Width:  float64(bounds.size.width),
		Height: float64(bounds.size.height),
	}, nil
}

func (b *darwinBridge) CursorPosition(ctx context.Context) (Point, error) {
	point := C.msCursorPosition()
	return Point{X: float64(point.x), Y: float64(point.y)}, nil
}

func (b *darwinBridge) EnterControl(ctx context.Context, anchor Point) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	C.msSetSuppressLocalInput(C.bool(true))
	C.msHideAndDetachCursor(C.double(anchor.X), C.double(anchor.Y))
	b.anchor = anchor
	b.locked = true
	return nil
}

func (b *darwinBridge) ExitControl(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.locked {
		return nil
	}
	C.msSetSuppressLocalInput(C.bool(false))
	C.msShowAndAttachCursor(C.double(b.anchor.X), C.double(b.anchor.Y))
	b.locked = false
	return nil
}

func (b *darwinBridge) StartCapture(ctx context.Context, events chan<- Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running {
		return nil
	}
	if !bool(C.msIsTrusted()) {
		return fmt.Errorf("accessibility permission is required before capture can start")
	}

	ready := make(chan error, 1)
	darwinCaptureMu.Lock()
	darwinCaptureCh = events
	darwinCaptureMu.Unlock()

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if ok := bool(C.msStartEventTap()); !ok {
			ready <- fmt.Errorf("failed to create Quartz event tap; check Accessibility permission")
			return
		}
		ready <- nil
		C.msRunEventTap()
	}()

	select {
	case err := <-ready:
		if err != nil {
			darwinCaptureMu.Lock()
			darwinCaptureCh = nil
			darwinCaptureMu.Unlock()
			return err
		}
		b.running = true
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *darwinBridge) Inject(ctx context.Context, event Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	switch event.Kind {
	case "mouse_move":
		C.msPostMouse(C.int(C.kCGEventMouseMoved), C.int(0), C.double(event.X), C.double(event.Y), C.uint64_t(event.Modifiers))
	case "left_down":
		C.msPostMouse(C.int(C.kCGEventLeftMouseDown), C.int(0), C.double(event.X), C.double(event.Y), C.uint64_t(event.Modifiers))
	case "left_up":
		C.msPostMouse(C.int(C.kCGEventLeftMouseUp), C.int(0), C.double(event.X), C.double(event.Y), C.uint64_t(event.Modifiers))
	case "right_down":
		C.msPostMouse(C.int(C.kCGEventRightMouseDown), C.int(1), C.double(event.X), C.double(event.Y), C.uint64_t(event.Modifiers))
	case "right_up":
		C.msPostMouse(C.int(C.kCGEventRightMouseUp), C.int(1), C.double(event.X), C.double(event.Y), C.uint64_t(event.Modifiers))
	case "other_down":
		C.msPostMouse(C.int(C.kCGEventOtherMouseDown), C.int(event.Button), C.double(event.X), C.double(event.Y), C.uint64_t(event.Modifiers))
	case "other_up":
		C.msPostMouse(C.int(C.kCGEventOtherMouseUp), C.int(event.Button), C.double(event.X), C.double(event.Y), C.uint64_t(event.Modifiers))
	case "scroll":
		C.msPostScroll(C.double(event.DeltaX), C.double(event.DeltaY))
	case "key_down":
		C.msPostKey(C.ushort(event.KeyCode), C.bool(true), C.uint64_t(event.Modifiers))
	case "key_up", "flags_changed":
		C.msPostKey(C.ushort(event.KeyCode), C.bool(false), C.uint64_t(event.Modifiers))
	}
	return nil
}

func (b *darwinBridge) StopCapture() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.running {
		return nil
	}
	C.msStopEventTap()
	darwinCaptureMu.Lock()
	darwinCaptureCh = nil
	darwinCaptureMu.Unlock()
	b.running = false
	return nil
}

//export darwinEmitEvent
func darwinEmitEvent(kind C.int, x C.double, y C.double, dx C.double, dy C.double, button C.int, keyCode C.ushort, flags C.ulonglong) {
	event := Event{
		X:         float64(x),
		Y:         float64(y),
		DeltaX:    float64(dx),
		DeltaY:    float64(dy),
		Button:    int(button),
		KeyCode:   uint16(keyCode),
		Modifiers: uint64(flags),
	}
	switch int(kind) {
	case darwinMouseMove:
		event.Kind = "mouse_move"
	case darwinLeftDown:
		event.Kind = "left_down"
	case darwinLeftUp:
		event.Kind = "left_up"
	case darwinRightDown:
		event.Kind = "right_down"
	case darwinRightUp:
		event.Kind = "right_up"
	case darwinOtherDown:
		event.Kind = "other_down"
	case darwinOtherUp:
		event.Kind = "other_up"
	case darwinScroll:
		event.Kind = "scroll"
	case darwinKeyDown:
		event.Kind = "key_down"
	case darwinKeyUp:
		event.Kind = "key_up"
	case darwinFlagsChanged:
		event.Kind = "flags_changed"
	default:
		return
	}

	darwinCaptureMu.RLock()
	ch := darwinCaptureCh
	darwinCaptureMu.RUnlock()
	if ch == nil {
		return
	}
	select {
	case ch <- event:
	default:
	}
}

//go:build windows

package platform

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"mouseshare/internal/domain"
)

const (
	whKeyboardLL = 13
	whMouseLL    = 14

	wmQuit        = 0x0012
	wmKeyDown     = 0x0100
	wmKeyUp       = 0x0101
	wmSysKeyDown  = 0x0104
	wmSysKeyUp    = 0x0105
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmRButtonDown = 0x0204
	wmRButtonUp   = 0x0205
	wmMouseWheel  = 0x020A
	wmXButtonDown = 0x020B
	wmXButtonUp   = 0x020C
	wmMouseHWheel = 0x020E

	hcAction = 0

	inputMouse    = 0
	inputKeyboard = 1

	mouseeventfMove        = 0x0001
	mouseeventfAbsolute    = 0x8000
	mouseeventfVirtualdesk = 0x4000
	mouseeventfLeftDown    = 0x0002
	mouseeventfLeftUp      = 0x0004
	mouseeventfRightDown   = 0x0008
	mouseeventfRightUp     = 0x0010
	mouseeventfMiddleDown  = 0x0020
	mouseeventfMiddleUp    = 0x0040
	mouseeventfWheel       = 0x0800
	mouseeventfHWheel      = 0x1000
	mouseeventfXDown       = 0x0080
	mouseeventfXUp         = 0x0100

	keyeventfKeyUp = 0x0002

	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79

	xbutton1 = 0x0001
	xbutton2 = 0x0002

	wheelDelta = 120
)

type point struct {
	X int32
	Y int32
}

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type msllhookstruct struct {
	Pt          point
	MouseData   uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type kbdllhookstruct struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type mouseinput struct {
	Dx          int32
	Dy          int32
	MouseData   uint32
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type keybdinput struct {
	WVk         uint16
	WScan       uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type hardwareinput struct {
	UMsg    uint32
	WParamL uint16
	WParamH uint16
}

type inputUnion struct {
	mi mouseinput
}

type input struct {
	Type uint32
	_    uint32
	U    inputUnion
}

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procSetWindowsHookEx    = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetMessage          = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessage     = user32.NewProc("DispatchMessageW")
	procPostThreadMessage   = user32.NewProc("PostThreadMessageW")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procSendInput           = user32.NewProc("SendInput")
	procGetCurrentThreadID  = kernel32.NewProc("GetCurrentThreadId")

	windowsCaptureMu sync.RWMutex
	windowsCaptureCh chan<- Event
	lastMousePoint   point
	lastMouseValid   bool
	mouseHook        uintptr
	keyboardHook     uintptr
	mouseCB          = syscall.NewCallback(windowsMouseProc)
	keyboardCB       = syscall.NewCallback(windowsKeyboardProc)
)

type windowsBridge struct {
	mu       sync.Mutex
	running  bool
	threadID uint32
}

func newWindowsBridge() Bridge {
	return &windowsBridge{}
}

func (b *windowsBridge) Name() string { return "windows" }

func (b *windowsBridge) Permissions(context.Context) domain.PermissionState {
	return domain.PermissionState{
		Accessibility: true,
		InputCapture:  true,
		ScreenAccess:  true,
		Warnings: []string{
			"Allow Windows Firewall access on first launch so peers on the same Wi-Fi can connect.",
		},
	}
}

func (b *windowsBridge) Bounds(context.Context) (Rect, error) {
	return Rect{
		MinX:   float64(getSystemMetrics(smXVirtualScreen)),
		MinY:   float64(getSystemMetrics(smYVirtualScreen)),
		Width:  float64(getSystemMetrics(smCXVirtualScreen)),
		Height: float64(getSystemMetrics(smCYVirtualScreen)),
	}, nil
}

func (b *windowsBridge) CursorPosition(context.Context) (Point, error) {
	var pt point
	r1, _, err := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	if r1 == 0 {
		return Point{}, err
	}
	return Point{X: float64(pt.X), Y: float64(pt.Y)}, nil
}

func (b *windowsBridge) StartCapture(ctx context.Context, events chan<- Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running {
		return nil
	}
	windowsCaptureMu.Lock()
	windowsCaptureCh = events
	lastMouseValid = false
	windowsCaptureMu.Unlock()

	ready := make(chan error, 1)
	threadID := make(chan uint32, 1)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		tid := uint32(mustProc(procGetCurrentThreadID.Call()))
		threadID <- tid

		mh, _, err := procSetWindowsHookEx.Call(uintptr(whMouseLL), mouseCB, 0, 0)
		if mh == 0 {
			ready <- fmt.Errorf("install mouse hook failed: %v", err)
			return
		}
		kh, _, err := procSetWindowsHookEx.Call(uintptr(whKeyboardLL), keyboardCB, 0, 0)
		if kh == 0 {
			_, _, _ = procUnhookWindowsHookEx.Call(mh)
			ready <- fmt.Errorf("install keyboard hook failed: %v", err)
			return
		}

		windowsCaptureMu.Lock()
		mouseHook = mh
		keyboardHook = kh
		windowsCaptureMu.Unlock()
		ready <- nil

		var m msg
		for {
			ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			switch int32(ret) {
			case -1:
				return
			case 0:
				return
			default:
				procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
				procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
			}
		}
	}()

	select {
	case tid := <-threadID:
		b.threadID = tid
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-ready:
		if err != nil {
			windowsCaptureMu.Lock()
			windowsCaptureCh = nil
			windowsCaptureMu.Unlock()
			return err
		}
		b.running = true
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *windowsBridge) Inject(ctx context.Context, event Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	switch event.Kind {
	case "mouse_move":
		bounds, err := b.Bounds(ctx)
		if err != nil {
			return err
		}
		x, y := absoluteCoords(bounds, event.X, event.Y)
		return sendInput([]input{{
			Type: inputMouse,
			U: inputUnion{mi: mouseinput{
				Dx:      x,
				Dy:      y,
				DwFlags: mouseeventfMove | mouseeventfAbsolute | mouseeventfVirtualdesk,
			}},
		}})
	case "left_down":
		return sendMouseButton(event, mouseeventfLeftDown, 0)
	case "left_up":
		return sendMouseButton(event, mouseeventfLeftUp, 0)
	case "right_down":
		return sendMouseButton(event, mouseeventfRightDown, 0)
	case "right_up":
		return sendMouseButton(event, mouseeventfRightUp, 0)
	case "other_down":
		return sendMouseButton(event, buttonFlag(true, event.Button), buttonData(event.Button))
	case "other_up":
		return sendMouseButton(event, buttonFlag(false, event.Button), buttonData(event.Button))
	case "scroll":
		var inputs []input
		if event.DeltaY != 0 {
			inputs = append(inputs, input{
				Type: inputMouse,
				U: inputUnion{mi: mouseinput{
					MouseData: uint32(int32(event.DeltaY * wheelDelta)),
					DwFlags:   mouseeventfWheel,
				}},
			})
		}
		if event.DeltaX != 0 {
			inputs = append(inputs, input{
				Type: inputMouse,
				U: inputUnion{mi: mouseinput{
					MouseData: uint32(int32(event.DeltaX * wheelDelta)),
					DwFlags:   mouseeventfHWheel,
				}},
			})
		}
		return sendInput(inputs)
	case "key_down":
		return sendKey(event.KeyCode, 0)
	case "key_up", "flags_changed":
		return sendKey(event.KeyCode, keyeventfKeyUp)
	default:
		return nil
	}
}

func (b *windowsBridge) StopCapture() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.running {
		return nil
	}
	if b.threadID != 0 {
		_, _, _ = procPostThreadMessage.Call(uintptr(b.threadID), uintptr(wmQuit), 0, 0)
	}
	windowsCaptureMu.Lock()
	if mouseHook != 0 {
		_, _, _ = procUnhookWindowsHookEx.Call(mouseHook)
		mouseHook = 0
	}
	if keyboardHook != 0 {
		_, _, _ = procUnhookWindowsHookEx.Call(keyboardHook)
		keyboardHook = 0
	}
	windowsCaptureCh = nil
	lastMouseValid = false
	windowsCaptureMu.Unlock()
	b.running = false
	return nil
}

func windowsMouseProc(code int, wParam uintptr, lParam uintptr) uintptr {
	if code == hcAction {
		info := (*msllhookstruct)(unsafe.Pointer(lParam))
		event := Event{
			X: float64(info.Pt.X),
			Y: float64(info.Pt.Y),
		}
		switch uint32(wParam) {
		case wmMouseMove:
			event.Kind = "mouse_move"
			windowsCaptureMu.Lock()
			if lastMouseValid {
				event.DeltaX = float64(info.Pt.X - lastMousePoint.X)
				event.DeltaY = float64(info.Pt.Y - lastMousePoint.Y)
			}
			lastMousePoint = info.Pt
			lastMouseValid = true
			ch := windowsCaptureCh
			windowsCaptureMu.Unlock()
			emitWindowsEvent(ch, event)
		case wmLButtonDown:
			event.Kind = "left_down"
			emitCurrentWindowsEvent(event)
		case wmLButtonUp:
			event.Kind = "left_up"
			emitCurrentWindowsEvent(event)
		case wmRButtonDown:
			event.Kind = "right_down"
			emitCurrentWindowsEvent(event)
		case wmRButtonUp:
			event.Kind = "right_up"
			emitCurrentWindowsEvent(event)
		case wmXButtonDown:
			event.Kind = "other_down"
			event.Button = decodeXButton(info.MouseData)
			emitCurrentWindowsEvent(event)
		case wmXButtonUp:
			event.Kind = "other_up"
			event.Button = decodeXButton(info.MouseData)
			emitCurrentWindowsEvent(event)
		case wmMouseWheel:
			event.Kind = "scroll"
			event.DeltaY = float64(int16(info.MouseData>>16)) / wheelDelta
			emitCurrentWindowsEvent(event)
		case wmMouseHWheel:
			event.Kind = "scroll"
			event.DeltaX = float64(int16(info.MouseData>>16)) / wheelDelta
			emitCurrentWindowsEvent(event)
		}
	}
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(code), wParam, lParam)
	return ret
}

func windowsKeyboardProc(code int, wParam uintptr, lParam uintptr) uintptr {
	if code == hcAction {
		info := (*kbdllhookstruct)(unsafe.Pointer(lParam))
		event := Event{KeyCode: uint16(info.VkCode)}
		switch uint32(wParam) {
		case wmKeyDown, wmSysKeyDown:
			event.Kind = "key_down"
			emitCurrentWindowsEvent(event)
		case wmKeyUp, wmSysKeyUp:
			event.Kind = "key_up"
			emitCurrentWindowsEvent(event)
		}
	}
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(code), wParam, lParam)
	return ret
}

func emitCurrentWindowsEvent(event Event) {
	windowsCaptureMu.RLock()
	ch := windowsCaptureCh
	windowsCaptureMu.RUnlock()
	emitWindowsEvent(ch, event)
}

func emitWindowsEvent(ch chan<- Event, event Event) {
	if ch == nil {
		return
	}
	select {
	case ch <- event:
	default:
	}
}

func getSystemMetrics(idx int32) int32 {
	r1, _, _ := procGetSystemMetrics.Call(uintptr(idx))
	return int32(r1)
}

func absoluteCoords(bounds Rect, x, y float64) (int32, int32) {
	if bounds.Width <= 1 || bounds.Height <= 1 {
		return 0, 0
	}
	nx := ((x - bounds.MinX) * 65535.0) / (bounds.Width - 1)
	ny := ((y - bounds.MinY) * 65535.0) / (bounds.Height - 1)
	return int32(nx), int32(ny)
}

func sendMouseButton(event Event, flags uint32, data uint32) error {
	bounds := Rect{
		MinX:   float64(getSystemMetrics(smXVirtualScreen)),
		MinY:   float64(getSystemMetrics(smYVirtualScreen)),
		Width:  float64(getSystemMetrics(smCXVirtualScreen)),
		Height: float64(getSystemMetrics(smCYVirtualScreen)),
	}
	x, y := absoluteCoords(bounds, event.X, event.Y)
	return sendInput([]input{
		{
			Type: inputMouse,
			U: inputUnion{mi: mouseinput{
				Dx:      x,
				Dy:      y,
				DwFlags: mouseeventfMove | mouseeventfAbsolute | mouseeventfVirtualdesk,
			}},
		},
		{
			Type: inputMouse,
			U: inputUnion{mi: mouseinput{
				MouseData: data,
				DwFlags:   flags,
			}},
		},
	})
}

func sendKey(keyCode uint16, flags uint32) error {
	var u inputUnion
	*(*keybdinput)(unsafe.Pointer(&u)) = keybdinput{
		WVk:     keyCode,
		DwFlags: flags,
	}
	return sendInput([]input{{Type: inputKeyboard, U: u}})
}

func sendInput(inputs []input) error {
	if len(inputs) == 0 {
		return nil
	}
	r1, _, err := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(inputs[0]),
	)
	if r1 == 0 {
		return err
	}
	return nil
}

func buttonFlag(down bool, button int) uint32 {
	switch button {
	case 1:
		if down {
			return mouseeventfXDown
		}
		return mouseeventfXUp
	case 2:
		if down {
			return mouseeventfMiddleDown
		}
		return mouseeventfMiddleUp
	default:
		if down {
			return mouseeventfXDown
		}
		return mouseeventfXUp
	}
}

func buttonData(button int) uint32 {
	switch button {
	case 1:
		return xbutton1
	case 2:
		return 0
	default:
		return xbutton2
	}
}

func decodeXButton(mouseData uint32) int {
	switch mouseData >> 16 {
	case xbutton1:
		return 1
	case xbutton2:
		return 3
	default:
		return 1
	}
}

func mustProc(r1 uintptr, _ uintptr, _ error) uintptr {
	return r1
}

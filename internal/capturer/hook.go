package capturer

import (
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	WH_KEYBOARD_LL = 13
	WH_MOUSE_LL    = 14
	WM_KEYDOWN     = 0x0100
	WM_KEYUP       = 0x0101
	WM_SYSKEYDOWN  = 0x0104
	WM_SYSKEYUP    = 0x0105
	WM_MOUSEMOVE   = 0x0200
	WM_LBUTTONDOWN = 0x0201
	WM_LBUTTONUP   = 0x0202
	WM_RBUTTONDOWN = 0x0204
	WM_RBUTTONUP   = 0x0205
	WM_MBUTTONDOWN = 0x0207
	WM_MBUTTONUP   = 0x0208
	WM_MOUSEWHEEL  = 0x020A
	PM_REMOVE      = 0x0001
	HC_ACTION      = 0
)

type KBDLLHOOKSTRUCT struct {
	VKCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type MSLLHOOKSTRUCT struct {
	PtX       int32
	PtY       int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type LowLevelKeyboardProc func(nCode int, wParam uintptr, lParam uintptr) uintptr
type LowLevelMouseProc func(nCode int, wParam uintptr, lParam uintptr) uintptr

type InputEvent struct {
	Type      string
	Timestamp int64
	Data      map[string]interface{}
}

type Capturer struct {
	kbHook        uintptr
	mouseHook     uintptr
	Events        chan InputEvent
	running       bool
	msgLock       sync.Mutex
	lastMouseMove time.Time
}

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procPeekMessageW        = user32.NewProc("PeekMessageW")
)

var (
	currentVal   atomic.Value
	kbCallback    uintptr
	mouseCallback uintptr
)

func loadCurrent() *Capturer {
	c, _ := currentVal.Load().(*Capturer)
	return c
}

func storeCurrent(c *Capturer) {
	currentVal.Store(c)
}

func init() {
	kbCallback = syscall.NewCallback(keyboardHookProc)
	mouseCallback = syscall.NewCallback(mouseHookProc)
}

func keyboardHookProc(nCode int, wParam, lParam uintptr) uintptr {
	c := loadCurrent()
	if c == nil {
		ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
		return ret
	}
	if nCode >= 0 {
		kbd := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))
		now := time.Now().UnixMicro()
		switch wParam {
		case WM_KEYDOWN, WM_SYSKEYDOWN:
			select {
			case c.Events <- InputEvent{
				Type:      "keystroke",
				Timestamp: now,
				Data: map[string]interface{}{
					"action":   "down",
					"vkCode":   kbd.VKCode,
					"scanCode": kbd.ScanCode,
					"flags":    kbd.Flags,
				},
			}:
			default:
			}
		case WM_KEYUP, WM_SYSKEYUP:
			select {
			case c.Events <- InputEvent{
				Type:      "keystroke",
				Timestamp: now,
				Data: map[string]interface{}{
					"action":   "up",
					"vkCode":   kbd.VKCode,
					"scanCode": kbd.ScanCode,
					"flags":    kbd.Flags,
				},
			}:
			default:
			}
		}
	}
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

func mouseHookProc(nCode int, wParam, lParam uintptr) uintptr {
	c := loadCurrent()
	if c == nil {
		ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
		return ret
	}
	if nCode >= 0 {
		ms := (*MSLLHOOKSTRUCT)(unsafe.Pointer(lParam))
		now := time.Now().UnixMicro()
		switch wParam {
		case WM_MOUSEMOVE:
			if time.Since(c.lastMouseMove) < 30*time.Millisecond {
				break
			}
			c.lastMouseMove = time.Now()
			select {
			case c.Events <- InputEvent{
				Type:      "mouse",
				Timestamp: now,
				Data: map[string]interface{}{
					"x": ms.PtX,
					"y": ms.PtY,
				},
			}:
			default:
			}
		case WM_LBUTTONDOWN:
			select {
			case c.Events <- InputEvent{
				Type:      "click",
				Timestamp: now,
				Data: map[string]interface{}{
					"button": "left",
					"action": "down",
					"x":      ms.PtX,
					"y":      ms.PtY,
				},
			}:
			default:
			}
		case WM_LBUTTONUP:
			select {
			case c.Events <- InputEvent{
				Type:      "click",
				Timestamp: now,
				Data: map[string]interface{}{
					"button": "left",
					"action": "up",
					"x":      ms.PtX,
					"y":      ms.PtY,
				},
			}:
			default:
			}
		case WM_RBUTTONDOWN:
			select {
			case c.Events <- InputEvent{
				Type:      "click",
				Timestamp: now,
				Data: map[string]interface{}{
					"button": "right",
					"action": "down",
					"x":      ms.PtX,
					"y":      ms.PtY,
				},
			}:
			default:
			}
		case WM_RBUTTONUP:
			select {
			case c.Events <- InputEvent{
				Type:      "click",
				Timestamp: now,
				Data: map[string]interface{}{
					"button": "right",
					"action": "up",
					"x":      ms.PtX,
					"y":      ms.PtY,
				},
			}:
			default:
			}
		case WM_MBUTTONDOWN:
			select {
			case c.Events <- InputEvent{
				Type:      "click",
				Timestamp: now,
				Data: map[string]interface{}{
					"button": "middle",
					"action": "down",
					"x":      ms.PtX,
					"y":      ms.PtY,
				},
			}:
			default:
			}
		case WM_MBUTTONUP:
			select {
			case c.Events <- InputEvent{
				Type:      "click",
				Timestamp: now,
				Data: map[string]interface{}{
					"button": "middle",
					"action": "up",
					"x":      ms.PtX,
					"y":      ms.PtY,
				},
			}:
			default:
			}
		case WM_MOUSEWHEEL:
			delta := int32(ms.MouseData >> 16)
			select {
			case c.Events <- InputEvent{
				Type:      "scroll",
				Timestamp: now,
				Data: map[string]interface{}{
					"delta": delta,
					"x":     ms.PtX,
					"y":     ms.PtY,
				},
			}:
			default:
			}
		}
	}
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

func New() *Capturer {
	return &Capturer{
		Events: make(chan InputEvent, 1000),
	}
}

func (c *Capturer) Start() error {
	c.msgLock.Lock()
	defer c.msgLock.Unlock()
	if c.running {
		return nil
	}
	storeCurrent(c)
	hook, _, err := procSetWindowsHookExW.Call(
		WH_KEYBOARD_LL, kbCallback, 0, 0)
	if hook == 0 {
		storeCurrent(nil)
		return err
	}
	c.kbHook = hook
	hook, _, err = procSetWindowsHookExW.Call(
		WH_MOUSE_LL, mouseCallback, 0, 0)
	if hook == 0 {
		procUnhookWindowsHookEx.Call(c.kbHook)
		c.kbHook = 0
		storeCurrent(nil)
		return err
	}
	c.mouseHook = hook
	c.running = true
	go c.msgLoop()
	return nil
}

func (c *Capturer) Stop() error {
	c.msgLock.Lock()
	defer c.msgLock.Unlock()
	if !c.running {
		return nil
	}
	if c.kbHook != 0 {
		procUnhookWindowsHookEx.Call(c.kbHook)
		c.kbHook = 0
	}
	if c.mouseHook != 0 {
		procUnhookWindowsHookEx.Call(c.mouseHook)
		c.mouseHook = 0
	}
	c.running = false
	storeCurrent(nil)
	return nil
}

func (c *Capturer) msgLoop() {
	msg := make([]byte, 48)
	for c.running {
		procPeekMessageW.Call(
			uintptr(unsafe.Pointer(&msg[0])),
			0, 0, 0, PM_REMOVE)
		time.Sleep(5 * time.Millisecond)
	}
}

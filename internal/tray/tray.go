package tray

import (
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	gdi32                   = syscall.NewLazyDLL("gdi32.dll")
	shell32                 = syscall.NewLazyDLL("shell32.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")

	procShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	procCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	procTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	procAppendMenuW            = user32.NewProc("AppendMenuW")
	procDestroyMenu            = user32.NewProc("DestroyMenu")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDefWindowProcW         = user32.NewProc("DefWindowProcW")
	procRegisterClassExW       = user32.NewProc("RegisterClassExW")
	procUnregisterClassW       = user32.NewProc("UnregisterClassW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procGetMessageW            = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessageW       = user32.NewProc("DispatchMessageW")
	procCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	procCreateBitmap           = gdi32.NewProc("CreateBitmap")
	procCreateIconIndirect     = user32.NewProc("CreateIconIndirect")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDestroyIcon            = user32.NewProc("DestroyIcon")
	procLoadCursorW            = user32.NewProc("LoadCursorW")
	procLoadIconW              = user32.NewProc("LoadIconW")
	procGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	procShellExecuteW          = shell32.NewProc("ShellExecuteW")
)

const (
	NIM_ADD        = 0
	NIM_MODIFY     = 1
	NIM_DELETE     = 2
	NIF_MESSAGE    = 1
	NIF_ICON       = 2
	NIF_TIP        = 4
	WM_USER        = 0x0400
	WM_TRAY_CALLBACK = WM_USER + 1
	WM_COMMAND     = 0x0111
	WM_DESTROY     = 0x0002
	WM_QUIT        = 0x0012
	WM_LBUTTONUP   = 0x0202
	WM_RBUTTONUP   = 0x0205
	WM_LBUTTONDBLCLK = 0x0203
	TPM_RIGHTALIGN = 0x0008
	TPM_BOTTOMALIGN = 0x0020
	TPM_NONOTIFY   = 0x0080
	TPM_RETURNCMD  = 0x0100
	MF_STRING      = 0
	MF_SEPARATOR   = 0x0800
	MF_DISABLED    = 0x0002
	MF_CHECKED     = 0x0008
	MF_UNCHECKED   = 0
	IDI_APPLICATION = 32512
	IDC_ARROW      = 32512
	COLOR_WINDOW   = 5
	SW_SHOW        = 5
	SW_HIDE        = 0
)

type MenuItem struct {
	ID       int
	Label    string
	Clicked  func()
	Disabled bool
	Checked  bool
}

type NOTIFYICONDATAW struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     uintptr
}

type WNDCLASSEXW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type ICONINFO struct {
	fIcon    uint32
	xHotspot uint32
	yHotspot uint32
	hbmMask  uintptr
	hbmColor uintptr
}

type POINT struct {
	X int32
	Y int32
}

type MSG struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      POINT
}

type Tray struct {
	mu        sync.Mutex
	hwnd      uintptr
	hicon     uintptr
	nid       NOTIFYICONDATAW
	menuItems []MenuItem
	running   bool
	onExit    func()
	className string
	classAtom uint16
}

func New(title string, onExit func()) *Tray {
	t := &Tray{
		className: "BehaviorDNATrayWindow",
		onExit:    onExit,
	}
	hi, _, _ := procLoadIconW.Call(0, IDI_APPLICATION)
	t.hicon = hi
	return t
}

func (t *Tray) SetIcon(color string) {
	hi := createColorIcon(color)
	if hi != 0 {
		if t.hicon != 0 {
			procDestroyIcon.Call(t.hicon)
		}
		t.hicon = hi
	}
}

func (t *Tray) SetTooltip(text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	copy(t.nid.szTip[:], syscall.StringToUTF16(text))
}

func (t *Tray) SetMenu(items []MenuItem) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.menuItems = items
}

func (t *Tray) Show() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return nil
	}

	hInst, _, _ := procGetModuleHandleW.Call(0)

	classNamePtr, _ := syscall.UTF16PtrFromString(t.className)

	wc := WNDCLASSEXW{
		cbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		lpfnWndProc:   syscall.NewCallback(t.windowProc),
		hInstance:     hInst,
		hCursor:       0,
		hbrBackground: COLOR_WINDOW + 1,
		lpszClassName: classNamePtr,
	}
	wc.hCursor, _, _ = procLoadCursorW.Call(0, IDC_ARROW)

	atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return err
	}
	t.classAtom = uint16(atom)

	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(classNamePtr)),
		uintptr(unsafe.Pointer(classNamePtr)),
		0,
		0, 0, 0, 0,
		0,
		0,
		hInst,
		0,
	)
	if hwnd == 0 {
		procUnregisterClassW.Call(uintptr(unsafe.Pointer(classNamePtr)), hInst)
		return err
	}
	t.hwnd = hwnd

	t.nid = NOTIFYICONDATAW{}
	t.nid.cbSize = uint32(unsafe.Offsetof(t.nid.guidItem))
	t.nid.hWnd = hwnd
	t.nid.uID = 1
	t.nid.uFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	t.nid.uCallbackMessage = WM_TRAY_CALLBACK
	t.nid.hIcon = t.hicon
	copy(t.nid.szTip[:], syscall.StringToUTF16("BehaviorDNA Agent"))

	ret, _, err := procShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&t.nid)))
	if ret == 0 {
		procDestroyWindow.Call(hwnd)
		procUnregisterClassW.Call(uintptr(unsafe.Pointer(classNamePtr)), hInst)
		return err
	}

	t.running = true
	go t.msgLoop()
	return nil
}

func (t *Tray) Hide() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running {
		return nil
	}
	t.running = false

	procShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&t.nid)))

	procPostQuitMessage.Call(0)

	if t.hwnd != 0 {
		procDestroyWindow.Call(t.hwnd)
		t.hwnd = 0
	}

	hInst, _, _ := procGetModuleHandleW.Call(0)
	classNamePtr, _ := syscall.UTF16PtrFromString(t.className)
	procUnregisterClassW.Call(uintptr(unsafe.Pointer(classNamePtr)), hInst)

	if t.hicon != 0 {
		procDestroyIcon.Call(t.hicon)
		t.hicon = 0
	}
	return nil
}

var procDestroyWindow = user32.NewProc("DestroyWindow")

func (t *Tray) msgLoop() {
	var msg MSG
	for t.running {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (t *Tray) windowProc(hwnd uintptr, msg uint32, wParam uintptr, lParam uintptr) uintptr {
	switch msg {
	case WM_TRAY_CALLBACK:
		switch lParam {
		case WM_LBUTTONUP, WM_LBUTTONDBLCLK:
			procShellExecuteW.Call(0, 0,
				uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("http://localhost:9090"))),
				0, 0, SW_SHOW)
		case WM_RBUTTONUP:
			t.showContextMenu()
		}
	case WM_COMMAND:
		id := int(wParam & 0xFFFF)
		t.mu.Lock()
		for _, item := range t.menuItems {
			if item.ID == id {
				clicked := item.Clicked
				t.mu.Unlock()
				if clicked != nil {
					clicked()
				}
				return 0
			}
		}
		t.mu.Unlock()
	case WM_DESTROY:
		if t.onExit != nil {
			t.onExit()
		}
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func (t *Tray) showContextMenu() {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}

	t.mu.Lock()
	items := make([]MenuItem, len(t.menuItems))
	copy(items, t.menuItems)
	t.mu.Unlock()

	for _, item := range items {
		if item.ID == -1 {
			procAppendMenuW.Call(hMenu, MF_SEPARATOR, 0, 0)
			continue
		}
		flags := uintptr(MF_STRING)
		if item.Disabled {
			flags |= MF_DISABLED
		}
		if item.Checked {
			flags |= MF_CHECKED
		}
		labelPtr, _ := syscall.UTF16PtrFromString(item.Label)
		procAppendMenuW.Call(hMenu, flags, uintptr(item.ID), uintptr(unsafe.Pointer(labelPtr)))
	}

	var cursor POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))

	cmd, _, _ := procTrackPopupMenu.Call(hMenu,
		TPM_RIGHTALIGN|TPM_BOTTOMALIGN|TPM_NONOTIFY|TPM_RETURNCMD,
		uintptr(cursor.X), uintptr(cursor.Y), 0, t.hwnd, 0)

	if cmd != 0 {
		t.mu.Lock()
		for _, item := range t.menuItems {
			if item.ID == int(cmd) {
				clicked := item.Clicked
				t.mu.Unlock()
				if clicked != nil {
					clicked()
				}
				procDestroyMenu.Call(hMenu)
				return
			}
		}
		t.mu.Unlock()
	}
	procDestroyMenu.Call(hMenu)
}

var procGetCursorPos = user32.NewProc("GetCursorPos")

func createColorIcon(color string) uintptr {
	var rgb uint32
	switch color {
	case "green":
		rgb = 0x00FF00
	case "yellow":
		rgb = 0xFFFF00
	case "red":
		rgb = 0xFF0000
	default:
		rgb = 0x0000FF
	}

	width := 16
	height := 16

	colorData := make([]byte, width*height*3)
	b := byte(rgb & 0xFF)
	g := byte((rgb >> 8) & 0xFF)
	r := byte((rgb >> 16) & 0xFF)
	for i := 0; i < width*height; i++ {
		colorData[i*3] = b
		colorData[i*3+1] = g
		colorData[i*3+2] = r
	}

	maskStride := (width + 7) / 8
	maskData := make([]byte, maskStride*height)

	hbmColor, _, _ := procCreateBitmap.Call(uintptr(width), uintptr(height), 1, 24, uintptr(unsafe.Pointer(&colorData[0])))
	if hbmColor == 0 {
		return 0
	}
	hbmMask, _, _ := procCreateBitmap.Call(uintptr(width), uintptr(height), 1, 1, uintptr(unsafe.Pointer(&maskData[0])))
	if hbmMask == 0 {
		procDeleteObject.Call(hbmColor)
		return 0
	}

	ii := ICONINFO{
		fIcon:    1,
		hbmMask:  hbmMask,
		hbmColor: hbmColor,
	}
	hicon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	procDeleteObject.Call(hbmColor)
	procDeleteObject.Call(hbmMask)
	return hicon
}

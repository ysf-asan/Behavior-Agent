package capturer

import (
	"syscall"
	"unsafe"
)

type WindowInfo struct {
	Title   string
	Process string
	PID     uint32
}

var (
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")

	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess         = kernel32.NewProc("OpenProcess")
	procCloseHandle         = kernel32.NewProc("CloseHandle")

	psapi                   = syscall.NewLazyDLL("psapi.dll")
	procGetModuleBaseNameW  = psapi.NewProc("GetModuleBaseNameW")
)

func GetActiveWindowTitle() string {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return ""
	}
	buf := make([]uint16, 512)
	ret, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if ret == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:ret])
}

func GetActiveWindowInfo() *WindowInfo {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return nil
	}
	buf := make([]uint16, 512)
	ret, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	title := ""
	if ret > 0 {
		title = syscall.UTF16ToString(buf[:ret])
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	processName := ""
	if pid > 0 {
		const PROCESS_QUERY_INFORMATION = 0x0400
		const PROCESS_VM_READ = 0x0010
		hProcess, _, _ := procOpenProcess.Call(
			PROCESS_QUERY_INFORMATION|PROCESS_VM_READ,
			0,
			uintptr(pid))
		if hProcess != 0 {
			defer procCloseHandle.Call(hProcess)
			nameBuf := make([]uint16, 260)
			ret, _, _ := procGetModuleBaseNameW.Call(
				hProcess, 0, uintptr(unsafe.Pointer(&nameBuf[0])), uintptr(len(nameBuf)))
			if ret > 0 {
				processName = syscall.UTF16ToString(nameBuf[:ret])
			}
		}
	}
	return &WindowInfo{
		Title:   title,
		Process: processName,
		PID:     pid,
	}
}

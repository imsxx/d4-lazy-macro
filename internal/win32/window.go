//go:build windows

package win32

import (
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
)

// WindowRect 是窗口在屏幕上的绝对物理矩形。
type WindowRect struct {
	X, Y, Width, Height int32
}

// FindWindowByProcess 按进程名查找可见的顶层窗口，返回面积最大的那个（游戏主窗口）。
// 与前台无关：游戏被其他窗口挡住、或玩家切到别处干活时，依然能定位到它。
func FindWindowByProcess(process string) (windows.Handle, bool) {
	var best windows.Handle
	bestArea := 0
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		if vis, _, _ := procIsWindowVisible.Call(hwnd); vis == 0 {
			return 1 // 继续枚举
		}
		var pid uint32
		_, _, _ = procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == 0 {
			return 1
		}
		name, err := processName(pid)
		if err != nil || !MatchProcess(name, process) {
			return 1
		}
		var rc RECT
		if ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc))); ret == 0 {
			return 1
		}
		if area := int(rc.Right-rc.Left) * int(rc.Bottom-rc.Top); area > bestArea {
			bestArea = area
			best = windows.Handle(hwnd)
		}
		return 1
	})
	_, _, _ = procEnumWindows.Call(cb, 0)
	return best, best != 0
}

// GetWindowRectOf 返回窗口矩形在屏幕上的绝对物理坐标。
func GetWindowRectOf(hwnd windows.Handle) (WindowRect, bool) {
	var rc RECT
	if ret, _, _ := procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc))); ret == 0 {
		return WindowRect{}, false
	}
	return WindowRect{X: rc.Left, Y: rc.Top, Width: rc.Right - rc.Left, Height: rc.Bottom - rc.Top}, true
}

// IsForeground 返回该窗口当前是否位于前台。
func IsForeground(hwnd windows.Handle) bool {
	cur, _, _ := procGetForegroundWindow.Call()
	return cur != 0 && cur == uintptr(hwnd)
}

// ForegroundWindow 描述当前前台窗口及其进程。
type ForegroundWindow struct {
	Handle      windows.Handle
	Title       string
	ClassName   string
	ProcessID   uint32
	ProcessName string
	Elevated    bool
}

// GetForegroundWindow 返回当前前台窗口信息，用于「只在游戏前台时发送」的保护与真实窗口信息采集。
func GetForegroundWindow() (ForegroundWindow, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return ForegroundWindow{}, fmt.Errorf("当前没有前台窗口")
	}
	fg := ForegroundWindow{Handle: windows.Handle(hwnd)}

	title := make([]uint16, 512)
	if n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&title[0])), uintptr(len(title))); n > 0 {
		fg.Title = windows.UTF16ToString(title[:n])
	}
	class := make([]uint16, 256)
	if n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class))); n > 0 {
		fg.ClassName = windows.UTF16ToString(class[:n])
	}
	_, _, _ = procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&fg.ProcessID)))
	if fg.ProcessID != 0 {
		if name, err := processName(fg.ProcessID); err == nil {
			fg.ProcessName = name
		}
		if elevated, err := processElevated(fg.ProcessID); err == nil {
			fg.Elevated = elevated
		}
	}
	return fg, nil
}

func processName(pid uint32) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &size); err != nil {
		return "", err
	}
	return filepath.Base(windows.UTF16ToString(buf[:size])), nil
}

func processElevated(pid uint32) (bool, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(handle)
	var token windows.Token
	if err := windows.OpenProcessToken(handle, windows.TOKEN_QUERY, &token); err != nil {
		return false, err
	}
	defer token.Close()
	var elevation struct{ TokenIsElevated uint32 }
	var returned uint32
	if err := windows.GetTokenInformation(token, windows.TokenElevation, (*byte)(unsafe.Pointer(&elevation)), uint32(unsafe.Sizeof(elevation)), &returned); err != nil {
		return false, err
	}
	return elevation.TokenIsElevated != 0, nil
}

// SelfElevated 返回当前进程是否以管理员权限运行。
func SelfElevated() (bool, error) {
	return processElevated(windows.GetCurrentProcessId())
}

// MatchProcess 按进程名匹配目标（忽略大小写与 .exe 后缀），跨语言客户端稳定。
func MatchProcess(processName, target string) bool {
	name := strings.TrimSuffix(processName, ".exe")
	aim := strings.TrimSuffix(target, ".exe")
	return name != "" && strings.EqualFold(name, aim)
}

//go:build windows

package win32

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	gdi32 = windows.NewLazySystemDLL("gdi32.dll")

	procGetDC          = user32.NewProc("GetDC")
	procReleaseDC      = user32.NewProc("ReleaseDC")
	procGetPixel       = gdi32.NewProc("GetPixel")
	procGetClientRect  = user32.NewProc("GetClientRect")
	procClientToScreen = user32.NewProc("ClientToScreen")
)

type RECT struct {
	Left, Top, Right, Bottom int32
}

type POINT struct {
	X, Y int32
}

// Screen 实现游戏适配层的像素源接口：把公开的 GDI 取色与窗口客户区几何交给适配器使用。
// 不读游戏内存、不注入进程、不加载驱动。
type Screen struct{}

// ClientRect 返回窗口客户区的宽高及左上角在屏幕上的绝对物理坐标。
func (Screen) ClientRect(hwnd uintptr) (w, h, originX, originY int32, ok bool) {
	w, h, originX, originY, err := GetWindowClientSizeAndOrigin(windows.Handle(hwnd))
	if err != nil {
		return 0, 0, 0, 0, false
	}
	return w, h, originX, originY, true
}

// Pixel 返回屏幕绝对物理坐标 (x, y) 处的 RGB 分量。
func (Screen) Pixel(x, y int32) (r, g, b uint8, ok bool) {
	r, g, b, err := GetPixelColor(x, y)
	if err != nil {
		return 0, 0, 0, false
	}
	return r, g, b, true
}

// GetPixelColor 获取屏幕绝对物理坐标 (x, y) 的 RGB 颜色。
// 基于标准 Win32 GDI API，单次耗时小于 0.05ms。
func GetPixelColor(x, y int32) (r, g, b uint8, err error) {
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return 0, 0, 0, errors.New("GetDC failed")
	}
	defer procReleaseDC.Call(0, hdc)

	color, _, _ := procGetPixel.Call(hdc, uintptr(x), uintptr(y))
	if color == 0xFFFFFFFF { // CLR_INVALID
		return 0, 0, 0, errors.New("GetPixel failed (CLR_INVALID)")
	}
	return uint8(color & 0xFF), uint8((color >> 8) & 0xFF), uint8((color >> 16) & 0xFF), nil
}

// GetWindowClientSizeAndOrigin 返回窗口客户区的宽高及左上角在屏幕上的绝对物理坐标。
func GetWindowClientSizeAndOrigin(hwnd windows.Handle) (w, h, originX, originY int32, err error) {
	var rc RECT
	ret, _, _ := procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc)))
	if ret == 0 {
		return 0, 0, 0, 0, errors.New("GetClientRect failed")
	}
	var pt POINT
	ret, _, _ = procClientToScreen.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pt)))
	if ret == 0 {
		return 0, 0, 0, 0, errors.New("ClientToScreen failed")
	}
	return rc.Right - rc.Left, rc.Bottom - rc.Top, pt.X, pt.Y, nil
}

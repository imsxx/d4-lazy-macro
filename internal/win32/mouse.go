//go:build windows

package win32

import (
	"fmt"
	"unsafe"
)

var (
	procGetCursorPos = user32.NewProc("GetCursorPos")
	procSetCursorPos = user32.NewProc("SetCursorPos")
)

// MousePosition 返回鼠标当前的屏幕绝对坐标。
func MousePosition() (x, y int32, err error) {
	var pt POINT
	ret, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	if ret == 0 {
		return 0, 0, fmt.Errorf("GetCursorPos 失败")
	}
	return pt.X, pt.Y, nil
}

// SetMousePos 把鼠标移到屏幕绝对坐标（一步到位，不做插值动画）。
//
// 注意：它移动的是玩家的真实光标。助手功能必须在开始时记下原位置、收尾时移回
//（D3keyHelper 也是这么做的：MouseGetPos 记下、结束时还原），否则会留下一个被挪走的光标。
func SetMousePos(x, y int32) error {
	ret, _, _ := procSetCursorPos.Call(uintptr(x), uintptr(y))
	if ret == 0 {
		return fmt.Errorf("SetCursorPos(%d, %d) 失败", x, y)
	}
	return nil
}

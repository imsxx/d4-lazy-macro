//go:build windows

package win32

import (
	"fmt"
	"unsafe"
)

var (
	procSendInput        = user32.NewProc("SendInput")
	procGetAsyncKeyState = user32.NewProc("GetAsyncKeyState")
)

// 虚拟键码。鼠标键沿用对应 VK，便于轮询与配置统一。
const (
	VKLeftMouse   = 0x01
	VKRightMouse  = 0x02
	VKMiddleMouse = 0x04
	VKXButton1    = 0x05 // 鼠标后退侧键
	VKXButton2    = 0x06 // 鼠标前进侧键
	VKF1          = 0x70
)

const (
	inputMouse    = 0
	inputKeyboard = 1
)

const (
	keyEventKeyUp = 0x0002

	mouseEventLeftDown   = 0x0002
	mouseEventLeftUp     = 0x0004
	mouseEventRightDown  = 0x0008
	mouseEventRightUp    = 0x0010
	mouseEventMiddleDown = 0x0020
	mouseEventMiddleUp   = 0x0040
	mouseEventXDown      = 0x0080
	mouseEventXUp        = 0x0100
)

type keybdInput struct {
	Vk        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type mouseInput struct {
	Dx        int32
	Dy        int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// input 对应 Win32 INPUT 结构：4 字节类型 + 4 字节对齐 + 32 字节联合体（amd64 共 40 字节）。
type input struct {
	Type uint32
	_    uint32
	data [32]byte
}

func (in *input) keybd() *keybdInput { return (*keybdInput)(unsafe.Pointer(&in.data[0])) }
func (in *input) mouse() *mouseInput { return (*mouseInput)(unsafe.Pointer(&in.data[0])) }

// AssertInputLayout 校验结构布局。布局错误时 SendInput 会静默失败，必须 Fail Fast。
func AssertInputLayout() error {
	if size := unsafe.Sizeof(input{}); size != 40 {
		return fmt.Errorf("win32 INPUT 布局校验失败：期望 40 字节，实际 %d 字节", size)
	}
	if size := unsafe.Sizeof(keybdInput{}); size > 32 {
		return fmt.Errorf("win32 KEYBDINPUT 超出联合体：%d 字节", size)
	}
	if size := unsafe.Sizeof(mouseInput{}); size > 32 {
		return fmt.Errorf("win32 MOUSEINPUT 超出联合体：%d 字节", size)
	}
	return nil
}

func sendInput(in *input) error {
	n, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(in)), unsafe.Sizeof(*in))
	if n != 1 {
		return fmt.Errorf("SendInput 未能注入事件（返回 %d，err=%v）", n, err)
	}
	return nil
}

// TapKey 合成一次键盘按键：按下并抬起。
func TapKey(vk uint16) error {
	down := input{Type: inputKeyboard}
	down.keybd().Vk = vk
	if err := sendInput(&down); err != nil {
		return err
	}
	up := input{Type: inputKeyboard}
	up.keybd().Vk = vk
	up.keybd().Flags = keyEventKeyUp
	return sendInput(&up)
}

// TapMouse 合成一次鼠标点击（LMB / RMB / MMB / X1 / X2）：按下并抬起。
func TapMouse(vk uint16) error {
	downFlags, upFlags, data, err := mouseFlags(vk)
	if err != nil {
		return err
	}
	down := input{Type: inputMouse}
	down.mouse().MouseData = data
	down.mouse().Flags = downFlags
	if err := sendInput(&down); err != nil {
		return err
	}
	up := input{Type: inputMouse}
	up.mouse().MouseData = data
	up.mouse().Flags = upFlags
	return sendInput(&up)
}

// Tap 按 VK 合成一次点击：鼠标键走鼠标事件，其余走键盘事件。
func Tap(vk uint16) error {
	if IsMouseVK(vk) {
		return TapMouse(vk)
	}
	return TapKey(vk)
}

func mouseFlags(vk uint16) (down, up, data uint32, err error) {
	switch vk {
	case VKLeftMouse:
		return mouseEventLeftDown, mouseEventLeftUp, 0, nil
	case VKRightMouse:
		return mouseEventRightDown, mouseEventRightUp, 0, nil
	case VKMiddleMouse:
		return mouseEventMiddleDown, mouseEventMiddleUp, 0, nil
	case VKXButton1:
		return mouseEventXDown, mouseEventXUp, 1, nil
	case VKXButton2:
		return mouseEventXDown, mouseEventXUp, 2, nil
	}
	return 0, 0, 0, fmt.Errorf("不支持的鼠标按键 VK 0x%02X", vk)
}

// IsMouseVK 判断 VK 是否对应鼠标按键。
func IsMouseVK(vk uint16) bool {
	_, _, _, err := mouseFlags(vk)
	return err == nil
}

// KeyPressed 通过 GetAsyncKeyState 轮询按键（含鼠标各键）当前是否被按下。
// 零钩子方案：不安装任何全局钩子，避免杀软误报。
func KeyPressed(vk uint16) bool {
	state, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return int16(state) < 0
}

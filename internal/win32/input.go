//go:build windows

package win32

import (
	"fmt"
	"unsafe"

	"github.com/imsxx/diablo-lazy-macro/internal/keys"
)

var (
	procSendInput        = user32.NewProc("SendInput")
	procGetAsyncKeyState = user32.NewProc("GetAsyncKeyState")
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

// KeyDown 按下键盘键（不抬起）。
func KeyDown(vk uint16) error {
	in := input{Type: inputKeyboard}
	in.keybd().Vk = vk
	return sendInput(&in)
}

// KeyUp 抬起键盘键。
func KeyUp(vk uint16) error {
	in := input{Type: inputKeyboard}
	in.keybd().Vk = vk
	in.keybd().Flags = keyEventKeyUp
	return sendInput(&in)
}

// TapKey 合成一次键盘按键：按下并抬起。
func TapKey(vk uint16) error {
	if err := KeyDown(vk); err != nil {
		return err
	}
	return KeyUp(vk)
}

// MouseDown 按下鼠标键（不抬起）。
func MouseDown(vk uint16) error {
	down, _, data, err := mouseFlags(vk)
	if err != nil {
		return err
	}
	in := input{Type: inputMouse}
	in.mouse().MouseData = data
	in.mouse().Flags = down
	return sendInput(&in)
}

// MouseUp 抬起鼠标键。
func MouseUp(vk uint16) error {
	_, up, data, err := mouseFlags(vk)
	if err != nil {
		return err
	}
	in := input{Type: inputMouse}
	in.mouse().MouseData = data
	in.mouse().Flags = up
	return sendInput(&in)
}

// TapMouse 合成一次鼠标点击：按下并抬起。
func TapMouse(vk uint16) error {
	if err := MouseDown(vk); err != nil {
		return err
	}
	return MouseUp(vk)
}

// Tap 按 VK 合成一次点击：鼠标键走鼠标事件，其余走键盘事件。
func Tap(vk uint16) error {
	if keys.IsMouse(vk) {
		return TapMouse(vk)
	}
	return TapKey(vk)
}

func mouseFlags(vk uint16) (down, up, data uint32, err error) {
	switch vk {
	case keys.VKLeftMouse:
		return mouseEventLeftDown, mouseEventLeftUp, 0, nil
	case keys.VKRightMouse:
		return mouseEventRightDown, mouseEventRightUp, 0, nil
	case keys.VKMiddleMouse:
		return mouseEventMiddleDown, mouseEventMiddleUp, 0, nil
	case keys.VKXButton1:
		return mouseEventXDown, mouseEventXUp, 1, nil
	case keys.VKXButton2:
		return mouseEventXDown, mouseEventXUp, 2, nil
	}
	return 0, 0, 0, fmt.Errorf("不支持的鼠标按键 VK 0x%02X", vk)
}

// KeyPressed 通过 GetAsyncKeyState 轮询按键（含鼠标各键）当前是否被按下。
// 零钩子方案：不安装任何全局钩子，避免杀软误报。
func KeyPressed(vk uint16) bool {
	state, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return int16(state) < 0
}

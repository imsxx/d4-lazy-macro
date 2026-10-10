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

// 虚拟键码。
const (
	VKXButton1 = 0x05 // 鼠标后退侧键
	VKXButton2 = 0x06 // 鼠标前进侧键
	VKF1       = 0x70
)

const (
	inputKeyboard = 1
	keyEventKeyUp = 0x0002
)

type keybdInput struct {
	Vk        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// input 对应 Win32 INPUT 结构：4 字节类型 + 4 字节对齐 + 32 字节联合体（amd64 共 40 字节）。
type input struct {
	Type uint32
	_    uint32
	Ki   keybdInput
	_    [8]byte
}

// AssertInputLayout 校验 INPUT 结构布局。布局错误时 SendInput 会静默失败，必须 Fail Fast。
func AssertInputLayout() error {
	if size := unsafe.Sizeof(input{}); size != 40 {
		return fmt.Errorf("win32 INPUT 布局校验失败：期望 40 字节，实际 %d 字节", size)
	}
	return nil
}

func sendKey(vk uint16, up bool) error {
	in := input{Type: inputKeyboard}
	in.Ki.Vk = vk
	if up {
		in.Ki.Flags = keyEventKeyUp
	}
	n, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if n != 1 {
		return fmt.Errorf("SendInput 未能注入事件（返回 %d，err=%v）", n, err)
	}
	return nil
}

// TapKey 合成一次按键：按下并抬起。
func TapKey(vk uint16) error {
	if err := sendKey(vk, false); err != nil {
		return err
	}
	return sendKey(vk, true)
}

// KeyPressed 通过 GetAsyncKeyState 轮询按键（含鼠标侧键）当前是否被按下。
// 零钩子方案：不安装任何全局钩子，避免杀软误报。
func KeyPressed(vk uint16) bool {
	state, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return int16(state) < 0
}

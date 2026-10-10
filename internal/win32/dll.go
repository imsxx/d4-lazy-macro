//go:build windows

// Package win32 封装本工具所需的 Win32 能力：
// SendInput 按键合成、GetAsyncKeyState 轮询、前台窗口与进程信息检测。
// 全部为系统 API 调用，不读游戏内存、不注入、不加载驱动。
package win32

import "golang.org/x/sys/windows"

var user32 = windows.NewLazySystemDLL("user32.dll")

//go:build windows

// D4 Lazy Macro · Phase 0 输入探针。
// 验证三件事：合成按键在游戏内是否生效、鼠标侧键按住能否被轮询识别、前台窗口保护是否可靠。
// 零钩子、零内存读取、零注入：只调用 SendInput 与 GetAsyncKeyState。
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/imsxx/d4-lazy-macro/internal/win32"
)

func main() {
	target := flag.String("process", "Diablo IV.exe", "目标游戏进程名")
	spam := flag.String("spam", "1", "连发按键序列，逗号分隔（如 1,2,3,4）")
	holdIntervalMS := flag.Int("interval", 250, "按住侧键时的连发间隔（毫秒）")
	toggleKey := flag.String("toggle", "F1", "持续连发的启停键")
	flag.Parse()

	if err := win32.AssertInputLayout(); err != nil {
		fmt.Fprintln(os.Stderr, "启动自检失败：", err)
		os.Exit(1)
	}
	spamKeys, err := parseKeyList(*spam)
	if err != nil {
		fmt.Fprintln(os.Stderr, "解析 -spam 失败：", err)
		os.Exit(1)
	}
	toggleVK, err := parseKey(*toggleKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, "解析 -toggle 失败：", err)
		os.Exit(1)
	}
	if *holdIntervalMS < 20 {
		fmt.Fprintln(os.Stderr, "连发间隔不得小于 20ms")
		os.Exit(1)
	}
	selfAdmin, err := win32.SelfElevated()
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取自身权限失败：", err)
		os.Exit(1)
	}

	fmt.Println("=== D4 Lazy Macro · Phase 0 输入探针 ===")
	fmt.Printf("目标进程：%s | 连发序列：%s | 按住间隔：%dms | 启停键：%s\n", *target, *spam, *holdIntervalMS, strings.ToUpper(*toggleKey))
	fmt.Printf("自身权限：%s\n", adminText(selfAdmin))
	fmt.Println("操作：按住鼠标侧键 1/2 = 连发；按启停键 = 持续连发开关；Ctrl+C 退出")
	fmt.Println("说明：本探针不读游戏内存、不装钩子，仅合成按键并轮询按键状态。")
	fmt.Println()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	var (
		toggleOn   bool
		prevToggle bool
		lastWindow string
		warnedUIPI bool
		nextHold   time.Time
		nextToggle time.Time
		sentCount  int
		spamCursor int
	)

	for now := range ticker.C {
		// 1) 启停键边沿检测（轮询，零钩子）
		pressed := win32.KeyPressed(toggleVK)
		if pressed && !prevToggle {
			toggleOn = !toggleOn
			nextToggle = now
			fmt.Printf("[切换] 持续连发 = %v\n", onOff(toggleOn))
		}
		prevToggle = pressed

		// 2) 侧键按住检测（轮询，零钩子）
		holdActive := win32.KeyPressed(win32.VKXButton1) || win32.KeyPressed(win32.VKXButton2)

		// 3) 前台窗口信息：变化时打印一次，用于采集本机真实标题/类名/进程
		fg, err := win32.GetForegroundWindow()
		if err != nil {
			continue
		}
		line := fmt.Sprintf("前台：%s | 类名：%s | 进程：%s (PID %d) | 进程管理员：%v",
			fg.Title, fg.ClassName, fg.ProcessName, fg.ProcessID, fg.Elevated)
		if line != lastWindow {
			lastWindow = line
			fmt.Println("[窗口]", line)
		}
		inGame := win32.MatchProcess(fg.ProcessName, *target)
		if inGame && fg.Elevated && !selfAdmin && !warnedUIPI {
			warnedUIPI = true
			fmt.Println("[警告] 目标进程以管理员运行而探针不是：Windows 会静默丢弃合成输入（UIPI）。请改用管理员终端重跑本探针。")
		}
		if !inGame {
			continue
		}

		// 4) 连发（仅在目标进程位于前台时）
		due := (holdActive && now.After(nextHold)) || (toggleOn && now.After(nextToggle))
		if !due {
			continue
		}
		vk := spamKeys[spamCursor%len(spamKeys)]
		spamCursor++
		if err := win32.TapKey(vk); err != nil {
			fmt.Println("[错误]", err)
			continue
		}
		sentCount++
		source := "持续"
		if holdActive {
			source = "按住"
		}
		fmt.Printf("[发送#%d|%s] key=0x%02X\n", sentCount, source, vk)
		if holdActive {
			nextHold = now.Add(time.Duration(*holdIntervalMS) * time.Millisecond)
		} else {
			nextToggle = now.Add(500 * time.Millisecond)
		}
	}
}

func adminText(admin bool) string {
	if admin {
		return "管理员"
	}
	return "普通用户"
}

func onOff(on bool) string {
	if on {
		return "true"
	}
	return "false"
}

func parseKeyList(list string) ([]uint16, error) {
	var keys []uint16
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		vk, err := parseKey(part)
		if err != nil {
			return nil, err
		}
		keys = append(keys, vk)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("连发序列为空")
	}
	return keys, nil
}

var namedKeys = map[string]uint16{
	"TAB":      0x09,
	"ESC":      0x1B,
	"SPACE":    0x20,
	"CAPSLOCK": 0x14,
	"F1":       0x70,
	"F2":       0x71,
	"F3":       0x72,
	"F4":       0x73,
	"F5":       0x74,
	"F6":       0x75,
	"F7":       0x76,
	"F8":       0x77,
	"F9":       0x78,
	"F10":      0x79,
	"F11":      0x7A,
	"F12":      0x7B,
}

func parseKey(token string) (uint16, error) {
	upper := strings.ToUpper(strings.TrimSpace(token))
	if strings.HasPrefix(upper, "0X") {
		var v uint16
		if _, err := fmt.Sscanf(upper, "0X%x", &v); err != nil {
			return 0, fmt.Errorf("无法解析按键 %q", token)
		}
		return v, nil
	}
	if vk, ok := namedKeys[upper]; ok {
		return vk, nil
	}
	if len(upper) == 1 {
		c := upper[0]
		isDigit := c >= '0' && c <= '9'
		isLetter := c >= 'A' && c <= 'Z'
		if isDigit || isLetter {
			return uint16(c), nil
		}
	}
	return 0, fmt.Errorf("不支持的按键 %q（支持 0-9 / A-Z / F1-F12 / TAB / ESC / SPACE / 0xNN）", token)
}

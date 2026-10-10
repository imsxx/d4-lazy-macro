//go:build windows

// Diablo Lazy Macro · Phase 0 输入探针。
// 验证三件事：合成输入在游戏内是否生效、触发键按住能否被轮询识别、前台窗口保护是否可靠。
// 零钩子、零内存读取、零注入：只调用 SendInput 与 GetAsyncKeyState。
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/imsxx/diablo-lazy-macro/internal/keys"
	"github.com/imsxx/diablo-lazy-macro/internal/win32"
)

func main() {
	target := flag.String("process", "Diablo IV.exe", "目标游戏进程名")
	spam := flag.String("spam", "1", "连发按键序列，逗号分隔（键盘键或 LMB/RMB/MMB/X1/X2）")
	hold := flag.String("hold", "X1,X2", "触发连发的按住键，逗号分隔（如 X1,X2,LMB,RMB,1,2,3,4；留空则只测启停键）")
	holdIntervalMS := flag.Int("interval", 250, "按住触发键时的连发间隔（毫秒）")
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
	var holdKeys []uint16
	if strings.TrimSpace(*hold) != "" {
		holdKeys, err = parseKeyList(*hold)
		if err != nil {
			fmt.Fprintln(os.Stderr, "解析 -hold 失败：", err)
			os.Exit(1)
		}
	}
	toggleVK, err := keys.Parse(*toggleKey)
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

	fmt.Println("=== Diablo Lazy Macro · Phase 0 输入探针 ===")
	fmt.Printf("目标进程：%s\n", *target)
	if len(holdKeys) == 0 {
		fmt.Println("触发键（按住=连发）：无（只测启停键）")
	} else {
		fmt.Printf("触发键（按住=连发）：%s\n", joinLabels(holdKeys))
	}
	fmt.Printf("连发序列：%s | 按住间隔：%dms | 启停键：%s\n", joinLabels(spamKeys), *holdIntervalMS, keys.Label(toggleVK))
	fmt.Printf("自身权限：%s\n", adminText(selfAdmin))
	fmt.Println("操作：按住任一触发键 = 连发；按启停键 = 持续连发开关；Ctrl+C 退出")
	fmt.Println("说明：只有上面列出的触发键会在按下/松开时打印 [触发] 行，其余按键不会有任何输出。")
	fmt.Println("      连发只在目标进程位于前台时发送；本探针不读游戏内存、不装钩子。")
	for _, vk := range spamKeys {
		for _, hk := range holdKeys {
			if vk == hk {
				fmt.Printf("[警告] 键 %s 同时在 -hold 和 -spam 里，会形成自我触发叠加，建议分开配置。\n", keys.Label(vk))
			}
		}
	}
	fmt.Println()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	prevHold := make([]bool, len(holdKeys))

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

		// 2) 触发键轮询：打印按下/松开边沿，汇总按住状态（零钩子）
		holdActive := false
		for i, vk := range holdKeys {
			down := win32.KeyPressed(vk)
			if down != prevHold[i] {
				prevHold[i] = down
				if down {
					fmt.Printf("[触发] %s 按住\n", keys.Label(vk))
				} else {
					fmt.Printf("[触发] %s 松开\n", keys.Label(vk))
				}
			}
			holdActive = holdActive || down
		}

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
		if err := win32.Tap(vk); err != nil {
			fmt.Println("[错误]", err)
			continue
		}
		sentCount++
		source := "持续"
		if holdActive {
			source = "按住"
		}
		fmt.Printf("[发送#%d|%s] %s\n", sentCount, source, keys.Label(vk))
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

func joinLabels(vks []uint16) string {
	labels := make([]string, 0, len(vks))
	for _, vk := range vks {
		labels = append(labels, keys.Label(vk))
	}
	return strings.Join(labels, ",")
}

func parseKeyList(list string) ([]uint16, error) {
	var vks []uint16
	for _, part := range strings.Split(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		vk, err := keys.Parse(part)
		if err != nil {
			return nil, err
		}
		vks = append(vks, vk)
	}
	if len(vks) == 0 {
		return nil, fmt.Errorf("列表为空")
	}
	return vks, nil
}

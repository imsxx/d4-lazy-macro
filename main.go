//go:build windows

// D4 Lazy Macro 桌面执行器：托盘常驻 + 全局热键 + 分享码导入/导出。
// 三条红线：不读游戏内存、不注入进程、不加载驱动；只合成键鼠输入，并且仅在游戏位于前台时发送。
package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imsxx/d4-lazy-macro/internal/config"
	"github.com/imsxx/d4-lazy-macro/internal/engine"
	"github.com/imsxx/d4-lazy-macro/internal/keys"
	"github.com/imsxx/d4-lazy-macro/internal/store"
	"github.com/imsxx/d4-lazy-macro/internal/win32"

	"github.com/lxn/walk"
)

const (
	appName       = "D4 Lazy Macro · 暗黑4懒人宏"
	version       = "0.1.0-dev"
	targetProcess = "Diablo IV.exe"
	tickInterval  = 10 * time.Millisecond
)

// winSender 把 internal/win32 的输入合成接入规则引擎。
type winSender struct{}

func (winSender) Tap(vk uint16) error     { return win32.Tap(vk) }
func (winSender) KeyDown(vk uint16) error { return win32.KeyDown(vk) }
func (winSender) KeyUp(vk uint16) error   { return win32.KeyUp(vk) }

// winKeyState 把 GetAsyncKeyState 轮询接入规则引擎。
type winKeyState struct{}

func (winKeyState) Pressed(vk uint16) bool { return win32.KeyPressed(vk) }

// runtimeState 保存会被 UI 线程与 tick 线程并发访问的状态。
type runtimeState struct {
	mu       sync.Mutex
	eng      *engine.Engine
	raw      *config.Raw
	toggleVK uint16
}

func (s *runtimeState) snapshot() (*engine.Engine, *config.Raw, uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.eng, s.raw, s.toggleVK
}

func (s *runtimeState) apply(eng *engine.Engine, raw *config.Raw, toggleVK uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eng = eng
	s.raw = raw
	s.toggleVK = toggleVK
}

type app struct {
	mw             *walk.MainWindow
	ni             *walk.NotifyIcon
	dir            string
	st             *runtimeState
	toggleAction   *walk.Action
	profileActions []*walk.Action
	subMenu        *walk.Menu

	notifyMu   sync.Mutex
	lastNotify time.Time
}

func main() {
	runtime.LockOSThread()
	a, err := newApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, "启动失败：", err)
		os.Exit(1)
	}
	if ms := os.Getenv("D4LM_SMOKE_EXIT_MS"); ms != "" {
		if n, convErr := strconv.Atoi(ms); convErr == nil && n > 0 {
			go func() {
				time.Sleep(time.Duration(n) * time.Millisecond)
				a.mw.Synchronize(func() { _ = a.mw.Close() })
			}()
		}
	}
	code := a.run()
	os.Exit(code)
}

func newApp() (*app, error) {
	mw, err := walk.NewMainWindow()
	if err != nil {
		return nil, fmt.Errorf("创建主窗口失败：%v", err)
	}
	ni, err := walk.NewNotifyIcon(mw)
	if err != nil {
		return nil, fmt.Errorf("创建托盘图标失败：%v", err)
	}
	dir, err := store.DefaultDir()
	if err != nil {
		return nil, err
	}
	a := &app{mw: mw, ni: ni, dir: dir, st: &runtimeState{}}

	if err := a.loadStored(); err != nil {
		if walk.MsgBox(mw, appName, "本地存档无法读取：\n"+err.Error()+"\n\n要把它重命名保留（.bad-时间戳）并继续吗？",
			walk.MsgBoxYesNo|walk.MsgBoxIconWarning) != walk.DlgCmdYes {
			return nil, fmt.Errorf("存档损坏，用户选择退出")
		}
		moved, qerr := store.Quarantine(dir)
		if qerr != nil {
			return nil, qerr
		}
		fmt.Println("损坏存档已隔离：", moved)
	}
	if err := a.rebuildMenu(); err != nil {
		return nil, err
	}
	a.refreshTray()
	if err := ni.SetVisible(true); err != nil {
		return nil, fmt.Errorf("显示托盘图标失败：%v", err)
	}
	a.startTicker()

	if eng, _, _ := a.st.snapshot(); eng == nil {
		if err := ni.ShowInfo(appName, "尚未导入配置。\n右键托盘图标 →「从剪贴板导入分享码」开始。"); err != nil {
			fmt.Println("[托盘] 气泡提示失败：", err)
		}
	}
	fmt.Printf("%s v%s 已启动（数据目录：%s）\n", appName, version, dir)
	return a, nil
}

func (a *app) run() int {
	defer func() {
		if eng, _, _ := a.st.snapshot(); eng != nil {
			eng.ReleaseAll()
		}
		if err := a.ni.SetVisible(false); err != nil {
			fmt.Println("[托盘] 隐藏图标失败：", err)
		}
	}()
	return a.mw.Run()
}

func (a *app) loadStored() error {
	snap, err := store.Load(a.dir)
	if err != nil {
		return err
	}
	if snap == nil {
		fmt.Println("未找到本地配置：等待从剪贴板导入分享码")
		return nil
	}
	parsed, err := config.Resolve(snap.Config)
	if err != nil {
		return err
	}
	eng, err := engine.New(winSender{}, winKeyState{}, parsed, a.notifyEngineError)
	if err != nil {
		return err
	}
	if err := eng.SetProfile(snap.ProfileIdx); err != nil {
		return err
	}
	raw := snap.Config
	a.st.apply(eng, &raw, parsed.ToggleVK)
	fmt.Printf("已加载配置：%s（方案：%s）\n", raw.Name, profileDisplayName(eng, eng.ProfileIndex()))
	return nil
}

func (a *app) rebuildMenu() error {
	menu := a.ni.ContextMenu()
	var buildErr error
	check := func(err error) {
		if err != nil && buildErr == nil {
			buildErr = err
		}
	}

	check(menu.Actions().Clear())
	if a.subMenu != nil {
		a.subMenu.Dispose()
		a.subMenu = nil
	}

	ver := walk.NewAction()
	check(ver.SetText(fmt.Sprintf("%s v%s", appName, version)))
	check(ver.SetEnabled(false))
	check(menu.Actions().Add(ver))

	a.toggleAction = walk.NewAction()
	check(a.toggleAction.SetCheckable(true))
	a.toggleAction.Triggered().Attach(a.toggleFromUI)
	check(menu.Actions().Add(a.toggleAction))

	a.profileActions = nil
	eng, _, _ := a.st.snapshot()
	if eng != nil && eng.ProfileCount() > 1 {
		sub, err := walk.NewMenu()
		if err != nil {
			return err
		}
		a.subMenu = sub
		subAct, err := menu.Actions().AddMenu(sub)
		if err != nil {
			return err
		}
		check(subAct.SetText("切换方案"))
		for i := 0; i < eng.ProfileCount(); i++ {
			idx := i
			name := eng.ProfileName(idx)
			if name == "" {
				name = fmt.Sprintf("方案 %d", idx+1)
			}
			act := walk.NewAction()
			check(act.SetText(name))
			check(act.SetCheckable(true))
			act.Triggered().Attach(func() { a.switchProfile(idx) })
			check(sub.Actions().Add(act))
			a.profileActions = append(a.profileActions, act)
		}
	}

	check(menu.Actions().Add(walk.NewSeparatorAction()))

	importAct := walk.NewAction()
	check(importAct.SetText("从剪贴板导入分享码…"))
	importAct.Triggered().Attach(a.importFromClipboard)
	check(menu.Actions().Add(importAct))

	exportAct := walk.NewAction()
	check(exportAct.SetText("复制当前分享码"))
	exportAct.Triggered().Attach(a.exportToClipboard)
	check(menu.Actions().Add(exportAct))

	openAct := walk.NewAction()
	check(openAct.SetText("打开数据目录"))
	openAct.Triggered().Attach(a.openDataDir)
	check(menu.Actions().Add(openAct))

	check(menu.Actions().Add(walk.NewSeparatorAction()))

	quitAct := walk.NewAction()
	check(quitAct.SetText("退出"))
	quitAct.Triggered().Attach(func() { _ = a.mw.Close() })
	check(menu.Actions().Add(quitAct))

	return buildErr
}

// refreshTray 必须在 UI 线程调用。
func (a *app) refreshTray() {
	eng, _, toggleVK := a.st.snapshot()

	tip := fmt.Sprintf("%s v%s", appName, version)
	if eng == nil {
		tip += " · 未配置"
	} else {
		stateText := "已关闭"
		if eng.Enabled() {
			stateText = "已开启"
		}
		tip += fmt.Sprintf(" · %s · 方案：%s", stateText, eng.ProfileName(eng.ProfileIndex()))
	}
	if err := a.ni.SetToolTip(tip); err != nil {
		fmt.Println("[托盘] 设置提示失败：", err)
	}

	if a.toggleAction != nil {
		switch {
		case eng == nil:
			setActionText(a.toggleAction, "启用宏（先导入配置）")
			setActionChecked(a.toggleAction, false)
		case eng.Enabled():
			setActionText(a.toggleAction, fmt.Sprintf("停用宏（%s）", keys.Label(toggleVK)))
			setActionChecked(a.toggleAction, true)
		default:
			setActionText(a.toggleAction, fmt.Sprintf("启用宏（%s）", keys.Label(toggleVK)))
			setActionChecked(a.toggleAction, false)
		}
	}
	if eng != nil {
		current := eng.ProfileIndex()
		for i, act := range a.profileActions {
			setActionChecked(act, i == current)
		}
	}
}

func setActionText(act *walk.Action, text string) {
	if err := act.SetText(text); err != nil {
		fmt.Println("[托盘] 设置菜单文本失败：", err)
	}
}

func setActionChecked(act *walk.Action, checked bool) {
	if err := act.SetChecked(checked); err != nil {
		fmt.Println("[托盘] 设置菜单勾选失败：", err)
	}
}

func (a *app) refreshTrayAsync() {
	a.mw.Synchronize(a.refreshTray)
}

func (a *app) toggleFromUI() {
	eng, _, _ := a.st.snapshot()
	if eng == nil {
		walk.MsgBox(a.mw, appName, "还没有配置。\n\n先从剪贴板导入一段分享码：托盘菜单 →「从剪贴板导入分享码」。",
			walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	eng.Toggle()
	a.refreshTray()
}

func (a *app) importFromClipboard() {
	text, err := walk.Clipboard().Text()
	if err != nil {
		walk.MsgBox(a.mw, appName, "读取剪贴板失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	raw, parsed, err := config.Decode(text)
	if err != nil {
		walk.MsgBox(a.mw, appName, "分享码无效：\n\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}

	var sb strings.Builder
	sb.WriteString("确认导入这份配置？\n\n")
	sb.WriteString(describeConfig(raw, parsed))
	if warns := config.Warnings(parsed); len(warns) > 0 {
		sb.WriteString("\n注意：\n")
		for _, w := range warns {
			sb.WriteString("· " + w + "\n")
		}
	}
	if walk.MsgBox(a.mw, "导入分享码", sb.String(), walk.MsgBoxYesNo|walk.MsgBoxIconInformation) != walk.DlgCmdYes {
		return
	}
	if err := a.applyConfig(raw, parsed, 0); err != nil {
		walk.MsgBox(a.mw, appName, "应用配置失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if err := a.ni.ShowInfo(appName, "配置已应用。\n进入游戏后按 "+keys.Label(parsed.ToggleVK)+" 启用宏。"); err != nil {
		fmt.Println("[托盘] 气泡提示失败：", err)
	}
}

func (a *app) applyConfig(raw *config.Raw, parsed *config.Parsed, profileIdx int) error {
	eng, err := engine.New(winSender{}, winKeyState{}, parsed, a.notifyEngineError)
	if err != nil {
		return err
	}
	if err := eng.SetProfile(profileIdx); err != nil {
		return err
	}
	if err := store.Save(a.dir, store.Snapshot{Config: *raw, ProfileIdx: profileIdx}); err != nil {
		return err
	}
	if old, _, _ := a.st.snapshot(); old != nil {
		old.SetEnabled(false)
		old.ReleaseAll()
	}
	a.st.apply(eng, raw, parsed.ToggleVK)
	if err := a.rebuildMenu(); err != nil {
		return err
	}
	a.refreshTray()
	fmt.Printf("已应用配置：%s（共 %d 个方案，启停键 %s）\n", raw.Name, len(parsed.Profiles), keys.Label(parsed.ToggleVK))
	return nil
}

func (a *app) switchProfile(i int) {
	eng, raw, _ := a.st.snapshot()
	if eng == nil || raw == nil {
		return
	}
	if err := eng.SetProfile(i); err != nil {
		walk.MsgBox(a.mw, appName, "切换方案失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if err := store.Save(a.dir, store.Snapshot{Config: *raw, ProfileIdx: i}); err != nil {
		walk.MsgBox(a.mw, appName, "方案已切换，但保存失败（下次启动会回到旧方案）：\n"+err.Error(),
			walk.MsgBoxOK|walk.MsgBoxIconWarning)
	}
	a.refreshTray()
}

func (a *app) exportToClipboard() {
	_, raw, _ := a.st.snapshot()
	if raw == nil {
		walk.MsgBox(a.mw, appName, "还没有配置可导出。\n\n当前版本请先导入一段分享码；图形化编辑器将在后续版本提供。",
			walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	code, err := config.Encode(*raw)
	if err != nil {
		walk.MsgBox(a.mw, appName, "生成分享码失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if err := walk.Clipboard().SetText(code); err != nil {
		walk.MsgBox(a.mw, appName, "写入剪贴板失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if err := a.ni.ShowInfo(appName, "分享码已复制到剪贴板（"+strconv.Itoa(len(code))+" 字符）"); err != nil {
		fmt.Println("[托盘] 气泡提示失败：", err)
	}
}

func (a *app) openDataDir() {
	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		walk.MsgBox(a.mw, appName, "创建目录失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if err := exec.Command("explorer", a.dir).Start(); err != nil {
		walk.MsgBox(a.mw, appName, "打开目录失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
	}
}

func (a *app) startTicker() {
	go func() {
		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()
		var (
			prevToggle  bool
			lastEnabled bool
			lastProfile = -1
		)
		for range ticker.C {
			eng, _, toggleVK := a.st.snapshot()

			pressed := toggleVK != 0 && win32.KeyPressed(toggleVK)
			if pressed && !prevToggle && eng != nil {
				eng.Toggle()
			}
			prevToggle = pressed

			if eng == nil {
				continue
			}

			fg, err := win32.GetForegroundWindow()
			foreground := err == nil && win32.MatchProcess(fg.ProcessName, targetProcess)
			eng.Tick(time.Now(), foreground)

			if enabled := eng.Enabled(); enabled != lastEnabled || eng.ProfileIndex() != lastProfile {
				lastEnabled = enabled
				lastProfile = eng.ProfileIndex()
				a.refreshTrayAsync()
			}
		}
	}()
}

func (a *app) notifyEngineError(err error) {
	fmt.Println("[发送错误]", err)
	a.notifyMu.Lock()
	skip := time.Since(a.lastNotify) < 10*time.Second
	a.notifyMu.Unlock()
	if skip {
		return
	}
	a.notifyMu.Lock()
	a.lastNotify = time.Now()
	a.notifyMu.Unlock()
	a.mw.Synchronize(func() {
		if showErr := a.ni.ShowError(appName, "按键发送失败：\n"+err.Error()); showErr != nil {
			fmt.Println("[托盘] 错误提示失败：", showErr)
		}
	})
}

func describeConfig(raw *config.Raw, parsed *config.Parsed) string {
	var sb strings.Builder
	if raw.Name != "" {
		sb.WriteString("配置：" + raw.Name + "\n")
	}
	if raw.Author != "" {
		sb.WriteString("作者：" + raw.Author + "\n")
	}
	sb.WriteString("启停键：" + keys.Label(parsed.ToggleVK) + "\n")
	for i, prof := range parsed.Profiles {
		name := prof.Name
		if name == "" {
			name = fmt.Sprintf("方案 %d", i+1)
		}
		mark := ""
		if i == 0 {
			mark = "（默认启用）"
		}
		sb.WriteString(fmt.Sprintf("\n%s%s\n", name, mark))
		for _, r := range prof.Rules {
			sb.WriteString("  · " + describeRule(r) + "\n")
		}
	}
	sb.WriteString("\n提示：只有 Diablo IV 位于前台时才会发送按键。")
	return sb.String()
}

func describeRule(r config.ParsedRule) string {
	trigger := "宏开启期间"
	if r.HoldVK != 0 {
		trigger = "按住 " + keys.Label(r.HoldVK) + " 时"
	}
	switch r.Kind {
	case config.ActionTap:
		return fmt.Sprintf("%s，每 %s 连点一次 %s", trigger, durationText(r.IntervalMS), keys.Label(r.Keys[0]))
	case config.ActionSeq:
		return fmt.Sprintf("%s，循环按 %s（每 %s 一次）", trigger, joinKeyLabels(r.Keys, " → "), durationText(r.IntervalMS))
	case config.ActionOnce:
		return fmt.Sprintf("%s，点击一次 %s", trigger, keys.Label(r.Keys[0]))
	case config.ActionHold:
		return fmt.Sprintf("%s，保持按住 %s 不放", trigger, keys.Label(r.Keys[0]))
	}
	panic("未知动作类型：" + string(r.Kind))
}

func durationText(ms int) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	case ms%1000 == 0:
		return fmt.Sprintf("%d 秒", ms/1000)
	default:
		return fmt.Sprintf("%.1f 秒", float64(ms)/1000)
	}
}

func joinKeyLabels(vks []uint16, sep string) string {
	labels := make([]string, 0, len(vks))
	for _, vk := range vks {
		labels = append(labels, keys.Label(vk))
	}
	return strings.Join(labels, sep)
}

func profileDisplayName(eng *engine.Engine, idx int) string {
	if name := eng.ProfileName(idx); name != "" {
		return name
	}
	return fmt.Sprintf("方案 %d", idx+1)
}

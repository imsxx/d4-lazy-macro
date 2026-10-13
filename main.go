//go:build windows

// Diablo Lazy Macro 桌面执行器：主窗口 + 托盘常驻 + 全局热键 + 分享码导入/导出 + 日志。
// 支持暗黑破坏神4 / 暗黑破坏神3 / 暗黑破坏神2：重制版，各自独立配置。
// 唯一底线：不做无人值守的挂机与全自动打怪。
// 技术约束：不读游戏内存、不注入进程、不加载驱动；只合成键鼠输入，屏幕识别仅用公开的截图/取色；仅在目标游戏位于前台时发送。
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

	"github.com/imsxx/diablo-lazy-macro/internal/config"
	"github.com/imsxx/diablo-lazy-macro/internal/engine"
	"github.com/imsxx/diablo-lazy-macro/internal/games"
	"github.com/imsxx/diablo-lazy-macro/internal/keys"
	"github.com/imsxx/diablo-lazy-macro/internal/logx"
	"github.com/imsxx/diablo-lazy-macro/internal/store"
	"github.com/imsxx/diablo-lazy-macro/internal/win32"

	"github.com/lxn/walk"
)

const (
	appName      = "Diablo Lazy Macro · 暗黑懒人宏"
	version      = "0.2.0-dev"
	tickInterval = 10 * time.Millisecond
)

// winSender 把 internal/win32 的输入合成接入规则引擎。
type winSender struct{}

func (winSender) Tap(vk uint16) error     { return win32.Tap(vk) }
func (winSender) KeyDown(vk uint16) error { return win32.KeyDown(vk) }
func (winSender) KeyUp(vk uint16) error   { return win32.KeyUp(vk) }

// winKeyState 把 GetAsyncKeyState 轮询接入规则引擎。
type winKeyState struct{}

func (winKeyState) Pressed(vk uint16) bool { return win32.KeyPressed(vk) }

// runtimeSnapshot 是 UI 线程与 tick 线程共享的一次性快照。
type runtimeSnapshot struct {
	Engine        *engine.Engine
	Raw           *config.Raw
	ToggleVK      uint16
	GameID        string
	TargetProcess string
	StartMode     int // 0: 懒人模式 (开关切换), 1: 仅按下时 (按住生效)
	QuickPause    store.QuickPauseSettings
	SmartPause    bool
}

// runtimeState 保存会被 UI 线程与 tick 线程并发访问的状态。
type runtimeState struct {
	mu sync.Mutex
	s  runtimeSnapshot
}

func (s *runtimeState) snapshot() runtimeSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.s
}

func (s *runtimeState) apply(snap runtimeSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s = snap
}

type app struct {
	mw   *walk.MainWindow // 隐藏的基础窗口：承载托盘与消息循环
	ni   *walk.NotifyIcon
	icon *walk.Icon // 应用图标（取自 exe 资源，窗口与托盘共用）
	dir  string
	log  *logx.Logger
	st   *runtimeState

	win *mainWindow // 程序主窗口（可见）

	settings *store.Settings // 仅 UI 线程访问

	// 快速暂停的轮询状态（仅 tick 线程访问）
	qpDown    bool
	qpLastTap time.Time

	// 智能暂停的轮询状态（仅 tick 线程访问；spPrev 的长度与 smartPauseStopKeys 一致）
	spTab  bool
	spPrev [3]bool

	toggleAction   *walk.Action
	profileActions []*walk.Action
	gameActions    []*walk.Action
	subMenuProfile *walk.Menu
	subMenuGame    *walk.Menu

	logDlg *walk.Dialog
	logTe  *walk.TextEdit

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
	if ms := os.Getenv("DLM_SMOKE_EXIT_MS"); ms != "" {
		if n, convErr := strconv.Atoi(ms); convErr == nil && n > 0 {
			go func() {
				time.Sleep(time.Duration(n) * time.Millisecond)
				a.mw.Synchronize(func() { _ = a.mw.Close() })
			}()
		}
	}
	os.Exit(a.run())
}

func newApp() (*app, error) {
	mw, err := walk.NewMainWindow()
	if err != nil {
		return nil, fmt.Errorf("创建基础窗口失败：%v", err)
	}
	dir, err := store.DefaultDir()
	if err != nil {
		return nil, err
	}
	logger, err := logx.New(dir)
	if err != nil {
		return nil, err
	}
	var ni *walk.NotifyIcon
	if iconObj, err := walk.NewNotifyIcon(mw); err == nil {
		ni = iconObj
	} else {
		logger.Printf("[托盘] 创建托盘图标失败（将仅使用主窗口模式）：%v", err)
	}
	a := &app{mw: mw, ni: ni, dir: dir, log: logger, st: &runtimeState{}}
	a.log.Printf("%s v%s 启动：%s/%s，数据目录 %s", appName, version, runtime.GOOS, runtime.GOARCH, dir)

	// 图标取自 exe 资源（winres.json 的 RT_GROUP_ICON #1）；加载失败退回系统默认图标，不影响功能。
	if icon, err := walk.NewIconFromResourceId(1); err == nil {
		a.icon = icon
		if err := a.mw.SetIcon(icon); err != nil {
			a.log.Printf("[图标] 主窗口图标设置失败：%v", err)
		}
		if ni != nil {
			if err := ni.SetIcon(icon); err != nil {
				a.log.Printf("[图标] 托盘图标设置失败：%v", err)
			}
		}
	} else {
		a.icon = walk.IconApplication()
		a.log.Printf("[图标] 资源图标加载失败，使用系统默认图标：%v", err)
	}

	settings, err := store.LoadSettings(dir)
	if err != nil {
		a.confirmQuarantine(store.SettingsFile, err)
		settings = store.DefaultSettings()
	}
	a.settings = settings
	a.log.Printf("当前游戏：%s，目标进程 %s", gameDisplayName(settings.CurrentGame), a.targetProcessOf(settings.CurrentGame))

	if err := a.loadGame(settings.CurrentGame); err != nil {
		a.confirmQuarantine(store.ConfigFile(settings.CurrentGame), err)
		if retryErr := a.loadGame(settings.CurrentGame); retryErr != nil {
			return nil, retryErr
		}
	}

	if err := a.rebuildMenu(); err != nil {
		return nil, err
	}
	a.refreshUI()
	if ni != nil {
		if err := ni.SetVisible(true); err != nil {
			a.log.Printf("[托盘] 显示图标失败：%v", err)
		}
		a.wireTrayClicks()
	}
	a.startTicker()

	if err := a.createMainWindow(); err != nil {
		a.log.Printf("[窗口] 创建失败，已退回托盘模式：%v", err)
		walk.MsgBox(a.mw, appName, "打开主窗口失败（仍可通过托盘使用）：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
	}
	return a, nil
}

func (a *app) run() int {
	defer func() {
		if eng := a.st.snapshot().Engine; eng != nil {
			eng.ReleaseAll()
		}
		// 退出时托盘的消息窗口往往已随主窗口销毁，隐藏失败属预期，故只记一行、不打印 walk 的堆栈。
		if a.ni != nil {
			if err := a.ni.SetVisible(false); err != nil {
				fmt.Fprintln(os.Stderr, "[托盘] 退出时隐藏图标失败（可忽略）")
			}
		}
		if err := a.log.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "[日志] 关闭失败：", err)
		}
	}()
	return a.mw.Run()
}

func (a *app) quitApp() {
	a.log.Printf("退出主程序")
	_ = a.mw.Close()
}

// owner 返回对话框应使用的宿主窗口（优先主窗口）。
func (a *app) owner() walk.Form {
	if a.win != nil && a.win.mw != nil {
		return a.win.mw
	}
	return a.mw
}

// wireTrayClicks 让双击托盘图标打开主窗口。
func (a *app) wireTrayClicks() {
	var lastClick time.Time
	a.ni.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		now := time.Now()
		if now.Sub(lastClick) < 400*time.Millisecond {
			lastClick = time.Time{}
			a.showMainWindow()
			return
		}
		lastClick = now
	})
}

// confirmQuarantine 询问后隔离无法读取的文件；失败只记录，不影响本次会话继续运行。
func (a *app) confirmQuarantine(name string, loadErr error) {
	if walk.MsgBox(a.owner(), appName, name+" 无法读取：\n"+loadErr.Error()+"\n\n要把它重命名保留（.bad-时间戳）并以默认状态继续吗？",
		walk.MsgBoxYesNo|walk.MsgBoxIconWarning) != walk.DlgCmdYes {
		a.log.Printf("%s 无法读取，用户选择保留原文件（本次会话以默认状态继续）：%v", name, loadErr)
		return
	}
	moved, err := store.Quarantine(a.dir, name)
	if err != nil {
		a.log.Printf("[错误] 隔离 %s 失败：%v", name, err)
		return
	}
	a.log.Printf("损坏文件已隔离：%s", moved)
}

func (a *app) loadGame(gameID string) error {
	target, err := a.settings.TargetProcess(gameID)
	if err != nil {
		return err
	}
	next := runtimeSnapshot{GameID: gameID, TargetProcess: target, QuickPause: a.settings.QuickPause, SmartPause: a.settings.SmartPause}

	snap, err := store.Load(a.dir, store.ConfigFile(gameID))
	if err != nil {
		return err
	}
	if snap != nil {
		raw := snap.Config
		// 旧版本存档就地升级：回填释放策略并提升版本号，保证「保持Buff」的屏幕守卫不丢失。
		config.UpgradeLegacy(&raw)
		for len(raw.Profiles) < 4 {
			idx := len(raw.Profiles)
			raw.Profiles = append(raw.Profiles, config.RawProfile{
				Name:  fmt.Sprintf("配置 %d", idx+1),
				Rules: []config.RawRule{{Trigger: "t", Action: "tap:1", IntervalMS: 250}},
			})
		}
		parsed, err := config.Resolve(raw)
		if err != nil {
			return err
		}
		eng, err := engine.New(winSender{}, winKeyState{}, parsed, a.notifyEngineError)
		if err != nil {
			return err
		}
		a.setupBuffChecker(eng)
		a.setupForceStand(eng)
		profIdx := snap.ProfileIdx
		if profIdx < 0 || profIdx >= len(parsed.Profiles) {
			profIdx = 0
		}
		if err := eng.SetProfile(profIdx); err != nil {
			return err
		}
		eng.SetEnabled(true)
		next.Engine = eng
		next.Raw = &raw
		next.ToggleVK = parsed.ToggleVK
		next.StartMode = snap.StartMode
	} else {
		defRaw := defaultConfig()
		defRaw.Game = gameID
		parsed, err := config.Resolve(defRaw)
		if err == nil {
			if eng, err := engine.New(winSender{}, winKeyState{}, parsed, a.notifyEngineError); err == nil {
				a.setupBuffChecker(eng)
				a.setupForceStand(eng)
				_ = eng.SetProfile(0)
				eng.SetEnabled(true)
				next.Engine = eng
				next.Raw = &defRaw
				next.ToggleVK = parsed.ToggleVK
				next.StartMode = 0
			}
		}
	}

	if old := a.st.snapshot(); old.Engine != nil {
		old.Engine.SetEnabled(false)
		old.Engine.ReleaseAll()
	}
	a.st.apply(next)
	if next.Engine != nil {
		a.log.Printf("已加载 %s 配置：%s（方案：%s）", gameDisplayName(gameID), next.Raw.Name, profileDisplayName(next.Engine, next.Engine.ProfileIndex()))
	} else {
		a.log.Printf("%s 暂无配置：等待导入或新建", gameDisplayName(gameID))
	}
	return nil
}

func (a *app) switchGame(gameID string) {
	if a.settings.CurrentGame == gameID {
		return
	}
	if err := a.loadGame(gameID); err != nil {
		a.confirmQuarantine(store.ConfigFile(gameID), err)
		if retryErr := a.loadGame(gameID); retryErr != nil {
			walk.MsgBox(a.owner(), appName, "切换游戏失败：\n"+retryErr.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
			return
		}
	}
	a.settings.CurrentGame = gameID
	if err := store.SaveSettings(a.dir, *a.settings); err != nil {
		walk.MsgBox(a.owner(), appName, "已切换游戏，但设置保存失败（下次启动会回到旧游戏）：\n"+err.Error(),
			walk.MsgBoxOK|walk.MsgBoxIconWarning)
	}
	if err := a.rebuildMenu(); err != nil {
		a.log.Printf("[错误] 重建菜单失败：%v", err)
	}
	a.afterStateChange()
	a.log.Printf("切换到 %s（目标进程 %s）", gameDisplayName(gameID), a.targetProcessOf(gameID))
}

func (a *app) rebuildMenu() error {
	if a.ni == nil {
		return nil
	}
	menu := a.ni.ContextMenu()
	var buildErr error
	check := func(err error) {
		if err != nil && buildErr == nil {
			buildErr = err
		}
	}

	check(menu.Actions().Clear())
	if a.subMenuProfile != nil {
		a.subMenuProfile.Dispose()
		a.subMenuProfile = nil
	}
	if a.subMenuGame != nil {
		a.subMenuGame.Dispose()
		a.subMenuGame = nil
	}

	ver := walk.NewAction()
	check(ver.SetText(fmt.Sprintf("%s v%s", appName, version)))
	check(ver.SetEnabled(false))
	check(menu.Actions().Add(ver))

	showAct := walk.NewAction()
	check(showAct.SetText("显示主窗口"))
	showAct.Triggered().Attach(a.showMainWindow)
	check(menu.Actions().Add(showAct))

	a.toggleAction = walk.NewAction()
	check(a.toggleAction.SetCheckable(true))
	a.toggleAction.Triggered().Attach(a.toggleFromUI)
	check(menu.Actions().Add(a.toggleAction))

	// 切换游戏
	gameSub, err := walk.NewMenu()
	if err != nil {
		return err
	}
	a.subMenuGame = gameSub
	gameSubAct, err := menu.Actions().AddMenu(gameSub)
	if err != nil {
		return err
	}
	check(gameSubAct.SetText("切换游戏"))
	a.gameActions = nil
	for _, g := range games.All() {
		id := g.ID
		act := walk.NewAction()
		check(act.SetText(g.Name))
		check(act.SetCheckable(true))
		act.Triggered().Attach(func() { a.switchGame(id) })
		check(gameSub.Actions().Add(act))
		a.gameActions = append(a.gameActions, act)
	}

	// 切换方案（仅当前游戏有多个方案时）
	snap := a.st.snapshot()
	a.profileActions = nil
	if snap.Engine != nil && snap.Engine.ProfileCount() > 1 {
		sub, err := walk.NewMenu()
		if err != nil {
			return err
		}
		a.subMenuProfile = sub
		subAct, err := menu.Actions().AddMenu(sub)
		if err != nil {
			return err
		}
		check(subAct.SetText("切换方案"))
		for i := 0; i < snap.Engine.ProfileCount(); i++ {
			idx := i
			act := walk.NewAction()
			check(act.SetText(profileDisplayName(snap.Engine, idx)))
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

	logAct := walk.NewAction()
	check(logAct.SetText("查看日志…"))
	logAct.Triggered().Attach(a.showLogWindow)
	check(menu.Actions().Add(logAct))

	openAct := walk.NewAction()
	check(openAct.SetText("打开数据目录"))
	openAct.Triggered().Attach(a.openDataDir)
	check(menu.Actions().Add(openAct))

	check(menu.Actions().Add(walk.NewSeparatorAction()))

	quitAct := walk.NewAction()
	check(quitAct.SetText("退出"))
	quitAct.Triggered().Attach(a.quitApp)
	check(menu.Actions().Add(quitAct))

	return buildErr
}

// refreshUI 刷新托盘与主窗口的运行状态；必须在 UI 线程调用。
func (a *app) refreshUI() {
	snap := a.st.snapshot()

	tip := fmt.Sprintf("%s v%s · %s", appName, version, gameDisplayName(a.settings.CurrentGame))
	if snap.Engine == nil {
		tip += " · 未配置"
	} else {
		stateText := "已关闭"
		if snap.Engine.Enabled() {
			stateText = "已开启"
		}
		tip += fmt.Sprintf(" · %s · 方案：%s", stateText, profileDisplayName(snap.Engine, snap.Engine.ProfileIndex()))
	}
	if a.ni != nil {
		if err := a.ni.SetToolTip(tip); err != nil {
			a.log.Printf("[托盘] 设置提示失败：%v", err)
		}
	}

	if a.toggleAction != nil {
		switch {
		case snap.Engine == nil:
			setActionText(a.toggleAction, "启用宏（先导入或新建配置）")
			setActionChecked(a.toggleAction, false)
		case snap.Engine.Enabled():
			setActionText(a.toggleAction, fmt.Sprintf("停用宏（%s）", keyName(snap.ToggleVK)))
			setActionChecked(a.toggleAction, true)
		default:
			setActionText(a.toggleAction, fmt.Sprintf("启用宏（%s）", keyName(snap.ToggleVK)))
			setActionChecked(a.toggleAction, false)
		}
	}
	for i, act := range a.gameActions {
		if i < len(games.All()) {
			setActionChecked(act, games.All()[i].ID == a.settings.CurrentGame)
		}
	}
	if snap.Engine != nil {
		current := snap.Engine.ProfileIndex()
		for i, act := range a.profileActions {
			setActionChecked(act, i == current)
		}
	}
	if a.win != nil {
		a.win.refreshStatus()
	}
}

func (a *app) refreshUIAsync() {
	a.mw.Synchronize(a.refreshUI)
}

// afterStateChange 在任何改变运行状态的动作后统一刷新界面。
func (a *app) afterStateChange() {
	a.refreshUI()
	if a.win != nil {
		a.win.loadFromState()
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

func (a *app) toggleFromUI() {
	snap := a.st.snapshot()
	if snap.Engine == nil {
		walk.MsgBox(a.owner(), appName, "当前游戏还没有配置。\n\n在主窗口里编辑规则并「保存并应用」，或从剪贴板导入分享码。",
			walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	a.log.Printf("[切换] %s → %s", keyName(snap.ToggleVK), onOffText(snap.Engine.Toggle()))
	a.refreshUI()
}

func (a *app) importFromClipboard() {
	text, err := walk.Clipboard().Text()
	if err != nil {
		walk.MsgBox(a.owner(), appName, "读取剪贴板失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	raw, parsed, err := config.Decode(text)
	if err != nil {
		a.log.Printf("[导入] 分享码无效：%v", err)
		walk.MsgBox(a.owner(), appName, "分享码无效：\n\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}

	var sb strings.Builder
	sb.WriteString("确认导入这份配置？\n\n")
	sb.WriteString(a.describeConfig(raw, parsed))
	if parsed.Game != "" && parsed.Game != a.settings.CurrentGame {
		sb.WriteString(fmt.Sprintf("\n注意：这份配置标注为「%s」，当前游戏是「%s」，导入后会用于当前游戏。\n",
			gameDisplayName(parsed.Game), gameDisplayName(a.settings.CurrentGame)))
	}
	if warns := config.Warnings(parsed); len(warns) > 0 {
		sb.WriteString("\n注意：\n")
		for _, w := range warns {
			sb.WriteString("· " + w + "\n")
		}
	}
	if walk.MsgBox(a.owner(), "导入分享码", sb.String(), walk.MsgBoxYesNo|walk.MsgBoxIconInformation) != walk.DlgCmdYes {
		return
	}
	// 确保导入的配置具备 4 个完整方案
	for len(raw.Profiles) < 4 {
		idx := len(raw.Profiles)
		raw.Profiles = append(raw.Profiles, config.RawProfile{
			Name:  fmt.Sprintf("配置 %d", idx+1),
			Rules: []config.RawRule{{Trigger: "t", Action: "tap:1", IntervalMS: 250}},
		})
	}
	parsed, err = config.Resolve(*raw)
	if err != nil {
		walk.MsgBox(a.owner(), appName, "配置校验失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}

	startMode := a.st.snapshot().StartMode
	if err := a.applyConfig(raw, parsed, 0, startMode); err != nil {
		a.log.Printf("[导入] 应用失败：%v", err)
		walk.MsgBox(a.owner(), appName, "应用配置失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if a.ni != nil {
		if err := a.ni.ShowInfo(appName, "配置已应用。\n进入游戏后按 "+keyName(parsed.ToggleVK)+" 启用宏。"); err != nil {
			a.log.Printf("[托盘] 气泡提示失败：%v", err)
		}
	}
}

func (a *app) applyConfig(raw *config.Raw, parsed *config.Parsed, profileIdx int, startMode int) error {
	gameID := a.settings.CurrentGame
	if raw.Game == "" {
		raw.Game = gameID
	}
	target, err := a.settings.TargetProcess(gameID)
	if err != nil {
		return err
	}
	eng, err := engine.New(winSender{}, winKeyState{}, parsed, a.notifyEngineError)
	if err != nil {
		return err
	}
	a.setupBuffChecker(eng)
	a.setupForceStand(eng)
	if err := eng.SetProfile(profileIdx); err != nil {
		return err
	}
	eng.SetEnabled(true)
	if err := store.Save(a.dir, store.ConfigFile(gameID), store.Snapshot{
		Config:     *raw,
		ProfileIdx: profileIdx,
		StartMode:  startMode,
	}); err != nil {
		return err
	}
	if old := a.st.snapshot(); old.Engine != nil {
		old.Engine.SetEnabled(false)
		old.Engine.ReleaseAll()
	}
	a.st.apply(runtimeSnapshot{
		Engine:        eng,
		Raw:           raw,
		ToggleVK:      parsed.ToggleVK,
		GameID:        gameID,
		TargetProcess: target,
		StartMode:     startMode,
		QuickPause:    a.settings.QuickPause,
		SmartPause:    a.settings.SmartPause,
	})
	if err := a.rebuildMenu(); err != nil {
		a.log.Printf("[错误] 重建菜单失败：%v", err)
	}
	a.afterStateChange()
	a.log.Printf("已应用 %s 配置：%s（%d 个方案，启停键 %s）", gameDisplayName(gameID), raw.Name, len(parsed.Profiles), keyName(parsed.ToggleVK))
	return nil
}

func (a *app) switchProfile(i int) {
	snap := a.st.snapshot()
	if snap.Engine == nil || snap.Raw == nil {
		return
	}
	if snap.Engine.ProfileIndex() == i {
		return
	}
	if err := snap.Engine.SetProfile(i); err != nil {
		walk.MsgBox(a.owner(), appName, "切换方案失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if err := store.Save(a.dir, store.ConfigFile(snap.GameID), store.Snapshot{
		Config:     *snap.Raw,
		ProfileIdx: i,
		StartMode:  snap.StartMode,
	}); err != nil {
		walk.MsgBox(a.owner(), appName, "方案已切换，但保存失败（下次启动会回到旧方案）：\n"+err.Error(),
			walk.MsgBoxOK|walk.MsgBoxIconWarning)
	}
	a.refreshUI()
	if a.settings.SoundCue {
		win32.PlaySoundCue() // 切换成功给一声听觉反馈
	}
	a.log.Printf("[切换] 方案 → %s", profileDisplayName(snap.Engine, i))
}

func (a *app) exportToClipboard() {
	snap := a.st.snapshot()
	if snap.Raw == nil {
		walk.MsgBox(a.owner(), appName, "当前游戏还没有配置可导出。\n\n在主窗口里编辑规则并「保存并应用」，或从剪贴板导入一段分享码。",
			walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	code, err := config.Encode(*snap.Raw)
	if err != nil {
		walk.MsgBox(a.owner(), appName, "生成分享码失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if err := walk.Clipboard().SetText(code); err != nil {
		walk.MsgBox(a.owner(), appName, "写入剪贴板失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	a.log.Printf("[导出] 分享码已复制（%d 字符，%s）", len(code), gameDisplayName(snap.GameID))
	if a.ni != nil {
		if err := a.ni.ShowInfo(appName, "分享码已复制到剪贴板（"+strconv.Itoa(len(code))+" 字符）"); err != nil {
			a.log.Printf("[托盘] 气泡提示失败：%v", err)
		}
	}
}

func (a *app) openDataDir() {
	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		walk.MsgBox(a.owner(), appName, "创建目录失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if err := exec.Command("explorer", a.dir).Start(); err != nil {
		walk.MsgBox(a.owner(), appName, "打开目录失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
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
			prevSwitch  []bool
		)
		for range ticker.C {
			snap := a.st.snapshot()

			if snap.StartMode == 1 { // 仅按下时 (按住生效)
				down := snap.ToggleVK != 0 && win32.KeyPressed(snap.ToggleVK)
				if down != prevToggle && snap.Engine != nil {
					snap.Engine.SetEnabled(down)
					if down {
						a.log.Printf("[按键] %s 按下 → 宏启动", keyName(snap.ToggleVK))
					} else {
						a.log.Printf("[按键] %s 松开 → 宏停止", keyName(snap.ToggleVK))
					}
				}
				prevToggle = down
			} else { // 懒人模式 (开关切换)
				pressed := snap.ToggleVK != 0 && win32.KeyPressed(snap.ToggleVK)
				if pressed && !prevToggle && snap.Engine != nil {
					a.log.Printf("[热键] %s → %s", keyName(snap.ToggleVK), onOffText(snap.Engine.Toggle()))
				}
				prevToggle = pressed
			}

			if snap.Engine == nil {
				continue
			}
			// 方案切换键：任一方案的切换键被按下即切到该方案（全局生效，与前台无关）
			if n := snap.Engine.ProfileCount(); n > 0 {
				if len(prevSwitch) != n {
					prevSwitch = make([]bool, n)
				}
				current := snap.Engine.ProfileIndex()
				for i := 0; i < n; i++ {
					vk := snap.Engine.SwitchVK(i)
					down := vk != 0 && win32.KeyPressed(vk)
					if down && !prevSwitch[i] && i != current {
						idx := i
						a.log.Printf("[热键] %s → 切换方案 %s", keyName(vk), profileDisplayName(snap.Engine, idx))
						a.mw.Synchronize(func() { a.switchProfile(idx) })
					}
					prevSwitch[i] = down
				}
			}
			// 快速暂停：按鼠标键（单击 / 双击 / 压住）临时停手
			a.tickQuickPause(snap)
			// 智能暂停：聊天 / 开地图 / 回城时自动让路
			a.tickSmartPause(snap)

			fg, err := win32.GetForegroundWindow()
			foreground := err == nil && win32.MatchProcess(fg.ProcessName, snap.TargetProcess)
			snap.Engine.Tick(time.Now(), foreground)

			if enabled := snap.Engine.Enabled(); enabled != lastEnabled || snap.Engine.ProfileIndex() != lastProfile {
				lastEnabled = enabled
				lastProfile = snap.Engine.ProfileIndex()
				a.refreshUIAsync()
			}
		}
	}()
}

// smartPauseStopKeys 是智能暂停里「按下即停止宏」的键：回车（聊天）、T（回城）、M（世界地图）。
// 这三个键会让游戏接管输入，此时继续发技能只会误操作。
var smartPauseStopKeys = []uint16{keys.VKEnter, uint16('T'), uint16('M')}

// tickSmartPause 在轮询循环里检测智能暂停的触发键（语义对齐 D3keyHelper）：
// 按 Tab 暂停 / 恢复宏；按回车、T、M 停止宏。
func (a *app) tickSmartPause(snap runtimeSnapshot) {
	if !snap.SmartPause || snap.Engine == nil {
		return
	}
	eng := snap.Engine
	now := time.Now()

	// Tab：切换暂停 / 恢复
	tabDown := win32.KeyPressed(keys.VKTab)
	if tabDown && !a.spTab {
		if eng.Paused() {
			eng.Resume()
			a.log.Printf("[智能暂停] Tab → 恢复宏")
		} else {
			eng.QuickPause(now, 0, 0)
			a.log.Printf("[智能暂停] Tab → 暂停宏")
		}
	}
	a.spTab = tabDown

	// 回车 / T / M：停止宏
	for i, vk := range smartPauseStopKeys {
		down := win32.KeyPressed(vk)
		if down && !a.spPrev[i] && eng.Enabled() {
			eng.SetEnabled(false)
			a.log.Printf("[智能暂停] %s → 停止宏", keyName(vk))
		}
		a.spPrev[i] = down
	}
}

// updateSmartPause 由界面在设置变更后调用：更新运行期快照，无需重建引擎。
func (a *app) updateSmartPause(on bool) {
	snap := a.st.snapshot()
	snap.SmartPause = on
	a.st.apply(snap)
}

// quickPauseDoubleClickWindow 是双击判定的时间窗。
const quickPauseDoubleClickWindow = 400 * time.Millisecond

// quickPauseMouseVK 把设置里的鼠标键序号转成虚拟键码（1 左键 / 2 右键 / 3 中键 / 4 侧键1 / 5 侧键2）。
func quickPauseMouseVK(idx int) uint16 {
	switch idx {
	case 1:
		return keys.VKLeftMouse
	case 2:
		return keys.VKRightMouse
	case 3:
		return keys.VKMiddleMouse
	case 4:
		return keys.VKXButton1
	case 5:
		return keys.VKXButton2
	}
	return 0
}

// quickPauseClickVK 返回暂停期间需要连点的键（0 表示仅暂停、不连点）。
func quickPauseClickVK(action int) uint16 {
	if action == store.QuickPauseActionClickLeft {
		return keys.VKLeftMouse
	}
	return 0
}

// tickQuickPause 在轮询循环里检测快速暂停的触发键。
// 三种触发方式：双击（两次按下间隔小于 400ms）、单击、压住（按住期间暂停、松开恢复）。
func (a *app) tickQuickPause(snap runtimeSnapshot) {
	qp := snap.QuickPause
	if !qp.Enabled || snap.Engine == nil {
		return
	}
	vk := quickPauseMouseVK(qp.MouseKey)
	if vk == 0 {
		return
	}
	eng := snap.Engine
	down := win32.KeyPressed(vk)
	now := time.Now()

	if down && !a.qpDown { // 按下沿
		switch qp.Trigger {
		case store.QuickPauseDoubleClick:
			if !a.qpLastTap.IsZero() && now.Sub(a.qpLastTap) <= quickPauseDoubleClickWindow {
				a.qpLastTap = time.Time{}
				a.startQuickPause(eng, qp, now)
				return
			}
			a.qpLastTap = now
		case store.QuickPauseSingleClick, store.QuickPauseHold:
			a.startQuickPause(eng, qp, now)
		}
	} else if !down && a.qpDown && qp.Trigger == store.QuickPauseHold {
		eng.Resume()
		a.log.Printf("[暂停] %s 松开 → 恢复宏", keyName(vk))
	}
	a.qpDown = down
}

// startQuickPause 触发一次快速暂停；「压住」模式传 0 表示持续到松开。
func (a *app) startQuickPause(eng *engine.Engine, qp store.QuickPauseSettings, now time.Time) {
	d := time.Duration(qp.DelayMS) * time.Millisecond
	if qp.Trigger == store.QuickPauseHold {
		d = 0
	}
	eng.QuickPause(now, d, quickPauseClickVK(qp.Action))
	if d > 0 {
		a.log.Printf("[暂停] 快速暂停 %d 毫秒", qp.DelayMS)
		return
	}
	a.log.Printf("[暂停] 快速暂停（按住期间）")
}

// updateQuickPause 由界面在设置变更后调用：只更新运行期快照，无需重建引擎。
func (a *app) updateQuickPause(qp store.QuickPauseSettings) {
	snap := a.st.snapshot()
	snap.QuickPause = qp
	a.st.apply(snap)
	if snap.Engine != nil && !qp.Enabled {
		snap.Engine.Resume() // 关闭功能时立即恢复，避免卡在暂停态
	}
}

func (a *app) notifyEngineError(err error) {
	a.log.Printf("[发送错误] %v", err)
	a.notifyMu.Lock()
	skip := time.Since(a.lastNotify) < 10*time.Second
	if !skip {
		a.lastNotify = time.Now()
	}
	a.notifyMu.Unlock()
	if skip {
		return
	}
	a.mw.Synchronize(func() {
		if a.ni != nil {
			if showErr := a.ni.ShowError(appName, "按键发送失败：\n"+err.Error()); showErr != nil {
				a.log.Printf("[托盘] 错误提示失败：%v", showErr)
			}
		}
	})
}

// setupBuffChecker 为引擎注入屏幕 Buff 状态检测器：入参为技能键，返回该键的 Buff 是否仍在生效。
// 引擎只在声明了「保持Buff」策略的规则上调用它（并据此做优先级仲裁）；
// 具体识别算法由游戏适配器提供（暗黑3 为发光环取色，其余游戏暂无守卫恒返回 false），
// 通用层不再出现游戏标识判断。
func (a *app) setupBuffChecker(eng *engine.Engine) {
	if eng == nil {
		return
	}
	eng.SetBuffChecker(func(vk uint16) bool {
		snap := a.st.snapshot()
		adapter, ok := games.For(snap.GameID)
		if !ok {
			return false
		}
		fg, err := win32.GetForegroundWindow()
		if err != nil {
			return false
		}
		if !win32.MatchProcess(fg.ProcessName, snap.TargetProcess) {
			return false
		}
		if a.win == nil {
			return false
		}
		profIdx := eng.ProfileIndex()
		if profIdx < 0 || profIdx >= maxProfiles {
			return false
		}
		slotIdx := -1
		for s := 0; s < numSlots; s++ {
			if parsedVK, err := keys.Parse(a.win.profileSlots[profIdx][s].Key); err == nil && parsedVK == vk {
				slotIdx = s
				break
			}
		}
		if slotIdx < 0 {
			return false
		}
		// 由游戏适配器判定该槽位技能的 Buff 是否仍在生效（无守卫的游戏恒为 false）。
		return adapter.BuffActive(uintptr(fg.Handle), slotIdx, win32.Screen{})
	})
}

// setupForceStand 依据本机按键映射设置与游戏适配器，为引擎注入「点击前先按住强制站立键」。
// 设置来自每游戏按键映射（不随分享码分发）：玩家可自定义键位，留空则用该游戏默认值。
func (a *app) setupForceStand(eng *engine.Engine) {
	if eng == nil {
		return
	}
	gameID := a.settings.CurrentGame
	gk := a.settings.GameKeyMap(gameID)
	adapter, ok := games.For(gameID)
	if !ok || !gk.ForceStandEnabled {
		eng.SetForceStand(0, 0)
		return
	}
	targets := adapter.ForceStandKeys()
	if len(targets) == 0 {
		// 该游戏尚未启用这一机制（未实测），保持关闭。
		eng.SetForceStand(0, 0)
		return
	}
	keyName := gk.ForceStandKey
	if keyName == "" {
		keyName = adapter.DefaultForceStandKey()
	}
	modVK, err := keys.Parse(keyName)
	if err != nil || modVK == 0 {
		a.log.Printf("[设置] 强制站立键 %q 无效，已跳过", keyName)
		eng.SetForceStand(0, 0)
		return
	}
	applyVK, err := keys.Parse(targets[0])
	if err != nil || applyVK == 0 {
		eng.SetForceStand(0, 0)
		return
	}
	eng.SetForceStand(applyVK, modVK)
}

// setTargetProcess 更新某游戏的目标进程名（空字符串表示恢复默认），并同步运行期状态。
func (a *app) setTargetProcess(gameID, proc string) error {
	if a.settings.Processes == nil {
		a.settings.Processes = map[string]string{}
	}
	if proc == "" {
		delete(a.settings.Processes, gameID)
	} else {
		a.settings.Processes[gameID] = proc
	}
	if err := store.SaveSettings(a.dir, *a.settings); err != nil {
		return err
	}
	snap := a.st.snapshot()
	if snap.GameID == gameID {
		resolved, err := a.settings.TargetProcess(gameID)
		if err != nil {
			return err
		}
		snap.TargetProcess = resolved
		a.st.apply(snap)
	}
	return nil
}

func (a *app) targetProcessOf(gameID string) string {
	p, err := a.settings.TargetProcess(gameID)
	if err != nil {
		return ""
	}
	return p
}

func (a *app) describeConfig(raw *config.Raw, parsed *config.Parsed) string {
	var sb strings.Builder
	if parsed.Game != "" {
		sb.WriteString("游戏：" + gameDisplayName(parsed.Game) + "\n")
	}
	if raw.Name != "" {
		sb.WriteString("配置：" + raw.Name + "\n")
	}
	if raw.Author != "" {
		sb.WriteString("作者：" + raw.Author + "\n")
	}
	sb.WriteString("启停键：" + keyName(parsed.ToggleVK) + "\n")
	for i, prof := range parsed.Profiles {
		name := prof.Name
		if name == "" {
			name = fmt.Sprintf("方案 %d", i+1)
		}
		mark := ""
		if i == 0 {
			mark = "（默认启用）"
		}
		if prof.SwitchVK != 0 {
			mark += "（切换键 " + keyName(prof.SwitchVK) + "）"
		}
		sb.WriteString(fmt.Sprintf("\n%s%s\n", name, mark))
		for _, r := range prof.Rules {
			sb.WriteString("  · " + describeRule(r) + "\n")
		}
	}
	sb.WriteString("\n提示：只有目标游戏位于前台时才会发送按键。")
	return sb.String()
}

func describeRule(r config.ParsedRule) string {
	trigger := "宏开启期间"
	if r.HoldVK != 0 {
		trigger = "按住 " + keyName(r.HoldVK) + " 时"
	}
	switch r.Kind {
	case config.ActionTap:
		return fmt.Sprintf("%s，每 %s 连点一次 %s", trigger, durationText(r.IntervalMS), keyName(r.Keys[0]))
	case config.ActionSeq:
		return fmt.Sprintf("%s，循环按 %s（每 %s 一次）", trigger, joinKeyLabels(r.Keys, " → "), durationText(r.IntervalMS))
	case config.ActionOnce:
		return fmt.Sprintf("%s，点击一次 %s", trigger, keyName(r.Keys[0]))
	case config.ActionHold:
		return fmt.Sprintf("%s，保持按住 %s 不放", trigger, keyName(r.Keys[0]))
	}
	panic("未知动作类型：" + string(r.Kind))
}

// describeRawRule 是主窗口中使用的宽容版描述：字段可能尚未填好，不做校验。
func describeRawRule(r config.RawRule) string {
	trigger := "宏开启期间"
	if strings.HasPrefix(r.Trigger, "h:") {
		trigger = "按住 " + keyNameOfToken(strings.TrimPrefix(r.Trigger, "h:")) + " 时"
	}
	detail := r.Action
	if kind, arg, ok := strings.Cut(r.Action, ":"); ok {
		switch kind {
		case "tap":
			detail = "连点 " + keyNameOfToken(arg)
		case "seq":
			detail = "序列 " + keySequenceNames(arg)
		case "once":
			detail = "单点 " + keyNameOfToken(arg)
		case "hold":
			detail = "按住不放 " + keyNameOfToken(arg)
		}
	}
	if detail == "" {
		detail = "（未设置动作）"
	}
	if r.IntervalMS > 0 {
		return fmt.Sprintf("%s · %s · 每 %s", trigger, detail, durationText(r.IntervalMS))
	}
	return fmt.Sprintf("%s · %s", trigger, detail)
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
		labels = append(labels, keyName(vk))
	}
	return strings.Join(labels, sep)
}

func profileDisplayName(eng *engine.Engine, idx int) string {
	if name := eng.ProfileName(idx); name != "" {
		return name
	}
	return fmt.Sprintf("方案 %d", idx+1)
}

func gameDisplayName(id string) string {
	if g, ok := games.Lookup(id); ok {
		return g.Name
	}
	return id
}

func gameNames() []string {
	all := games.All()
	names := make([]string, 0, len(all))
	for _, g := range all {
		names = append(names, g.Name)
	}
	return names
}

func gameIndex(id string) int {
	for i, g := range games.All() {
		if g.ID == id {
			return i
		}
	}
	return -1
}

func onOffText(on bool) string {
	if on {
		return "已开启"
	}
	return "已关闭"
}

// defaultConfig 是新建配置的起手模板：固定 4 个方案，开箱即用。
func defaultConfig() config.Raw {
	profs := make([]config.RawProfile, maxProfiles)
	for i := 0; i < maxProfiles; i++ {
		profs[i] = config.RawProfile{
			Name:  fmt.Sprintf("配置 %d", i+1),
			Rules: []config.RawRule{{Trigger: "t", Action: "tap:1", IntervalMS: 250}},
		}
	}
	return config.Raw{
		Version:  config.Version,
		Profiles: profs,
	}
}

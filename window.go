//go:build windows

package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/imsxx/diablo-lazy-macro/internal/config"
	"github.com/imsxx/diablo-lazy-macro/internal/games"
	"github.com/imsxx/diablo-lazy-macro/internal/helper"
	"github.com/imsxx/diablo-lazy-macro/internal/store"

	webview2 "github.com/jchv/go-webview2"
	"github.com/lxn/walk"
	"golang.org/x/sys/windows"
)

//go:embed web/index.html web/style.css web/app.js
var webAssets embed.FS

var (
	user32DLL            = windows.NewLazySystemDLL("user32.dll")
	procShowWindow       = user32DLL.NewProc("ShowWindow")
	procSetForeground    = user32DLL.NewProc("SetForegroundWindow")
	procSetWindowLongPtr = user32DLL.NewProc("SetWindowLongPtrW")
	procCallWindowProc   = user32DLL.NewProc("CallWindowProcW")
)

const (
	maxProfiles = 4
	numSlots    = 6

	fixedWinWidth  = 880
	fixedWinHeight = 560

	SW_HIDE     = 0
	SW_SHOW     = 5
	SW_MINIMIZE = 6

	WM_CLOSE     = 0x0010
)

const (
	strategyDisabled = config.StrategyDisabled
	strategyHold     = config.StrategyHold
	strategySpam     = config.StrategySpam
	strategyBuff     = config.StrategyBuff
)

type skillSlotData struct {
	Name        string `json:"name"`
	DefaultKey  string `json:"defaultKey"`
	Key         string `json:"key"`
	Strategy    int    `json:"strategy"`
	IntervalMS  int    `json:"intervalMS"`
	DelayMS     int    `json:"delayMS"`
	RandomDelay bool   `json:"randomDelay"`
}

func defaultSlots() [numSlots]skillSlotData {
	var out [numSlots]skillSlotData
	defs := games.DefaultAdapter().Slots()
	for i := 0; i < numSlots && i < len(defs); i++ {
		out[i] = skillSlotData{
			Name:       defs[i].Name,
			DefaultKey: defs[i].DefaultKey,
			Key:        defs[i].DefaultKey,
			IntervalMS: 300,
		}
	}
	return out
}

type frontendSlot struct {
	Name        string `json:"name"`
	Key         string `json:"key"`
	Strategy    int    `json:"strategy"`
	IntervalMS  int    `json:"intervalMS"`
	DelayMS     int    `json:"delayMS"`
	RandomDelay bool   `json:"randomDelay"`
}

type frontendProfile struct {
	Name  string         `json:"name"`
	HK    string         `json:"hk"`
	Mode  string         `json:"mode"`
	Slots []frontendSlot `json:"slots"`
}

type helperMeta struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type frontendState struct {
	CurrentGame       string            `json:"currentGame"`
	CurrentProfileIdx int               `json:"currentProfileIdx"`
	ProfileNames      []string          `json:"profileNames"`
	Profiles          []frontendProfile `json:"profiles"`
	ToggleKey         string            `json:"toggleKey"`
	StartMode         int               `json:"startMode"`
	QuickPause        struct {
		Enabled  bool `json:"enabled"`
		Trigger  int  `json:"trigger"`
		MouseKey int  `json:"mouseKey"`
		Action   int  `json:"action"`
		DelayMS  int  `json:"delayMS"`
	} `json:"quickPause"`
	Helpers struct {
		Hotkey        string          `json:"hotkey"`
		DelayMS       int             `json:"delayMS"`
		ReforgeTarget int             `json:"reforgeTarget"`
		DevMode       bool            `json:"devMode"`
		Enabled       map[string]bool `json:"enabled"`
	} `json:"helpers"`
	AllHelpers        []helperMeta `json:"allHelpers"`
	SmartPause        bool         `json:"smartPause"`
	SoundCue          bool         `json:"soundCue"`
	ForceStandEnabled bool         `json:"forceStandEnabled"`
	ForceStandKey     string       `json:"forceStandKey"`
	TargetProcess     string       `json:"targetProcess"`
	MacroRunning      bool         `json:"macroRunning"`
	MacroPaused       bool         `json:"macroPaused"`
}

type mainWindow struct {
	app *app
	wv  webview2.WebView
	hwnd uintptr

	profileSlots [maxProfiles][numSlots]skillSlotData
	profileNames [maxProfiles]string
	profileHKs   [maxProfiles]string
	profileModes [maxProfiles]string
	startMode    int

	profileIdx int
	raw        config.Raw
	loading    bool

	hiddenHintShown bool
	quitting        bool
}

func (a *app) createMainWindow() error {
	w := &mainWindow{
		app: a,
	}

	htmlBytes, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		return fmt.Errorf("读取 index.html 失败：%v", err)
	}
	cssBytes, err := webAssets.ReadFile("web/style.css")
	if err != nil {
		return fmt.Errorf("读取 style.css 失败：%v", err)
	}
	jsBytes, err := webAssets.ReadFile("web/app.js")
	if err != nil {
		return fmt.Errorf("读取 app.js 失败：%v", err)
	}

	fullHtml := string(htmlBytes)
	fullHtml = strings.Replace(fullHtml, `<link rel="stylesheet" href="style.css">`, "<style>"+string(cssBytes)+"</style>", 1)
	fullHtml = strings.Replace(fullHtml, `<script src="app.js"></script>`, "<script>"+string(jsBytes)+"</script>", 1)

	// 创建 WebView2 窗口
	wv := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug: false,
		WindowOptions: webview2.WindowOptions{
			Title:  fmt.Sprintf("%s v%s", appName, version),
			Width:  fixedWinWidth,
			Height: fixedWinHeight,
			IconId: 1,
			Center: true,
		},
	})
	if wv == nil {
		return fmt.Errorf("初始化 WebView2 运行时失败，请确认系统已安装 Edge WebView2 运行时")
	}

	wv.SetSize(fixedWinWidth, fixedWinHeight, webview2.HintFixed)

	w.wv = wv
	w.hwnd = uintptr(wv.Window())
	a.win = w

	w.subclass()
	w.initBindings()

	wv.SetHtml(fullHtml)

	w.loadFromState()

	a.log.Printf("[窗口] 现代 WebView2 电竞 UI 初始化完成（固定尺寸 %dx%d）", fixedWinWidth, fixedWinHeight)
	return nil
}

func (w *mainWindow) subclass() {
	if w.hwnd == 0 {
		return
	}
	var oldWndProc uintptr
	cb := windows.NewCallback(func(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
		if msg == WM_CLOSE && !w.quitting {
			procShowWindow.Call(hwnd, uintptr(SW_HIDE))
			if !w.hiddenHintShown {
				w.hiddenHintShown = true
				if w.app != nil && w.app.ni != nil {
					_ = w.app.ni.ShowInfo(appName, "已最小化到托盘，宏会继续运行。\n双击托盘图标可重新打开。")
				}
			}
			return 0
		}
		r, _, _ := procCallWindowProc.Call(oldWndProc, hwnd, uintptr(msg), wp, lp)
		return r
	})

	gwlp := -4
	gwlpIndex := uintptr(gwlp)
	if procSetWindowLongPtr.Find() == nil {
		r, _, _ := procSetWindowLongPtr.Call(w.hwnd, gwlpIndex, cb)
		oldWndProc = r
	} else {
		procSetWindowLong := user32DLL.NewProc("SetWindowLongW")
		r, _, _ := procSetWindowLong.Call(w.hwnd, gwlpIndex, cb)
		oldWndProc = r
	}
}

// initBindings 将 Go 方法暴露给前端 JS
func (w *mainWindow) initBindings() {
	_ = w.wv.Bind("api_getState", func() (*frontendState, error) {
		return w.buildFrontendState(), nil
	})

	_ = w.wv.Bind("api_switchGame", func(gameID string) error {
		if gameID == w.app.settings.CurrentGame {
			return nil
		}
		w.saveToState(true)
		w.app.switchGame(gameID)
		return nil
	})

	_ = w.wv.Bind("api_switchProfile", func(idx int) error {
		if idx < 0 || idx >= maxProfiles {
			return nil
		}
		w.profileIdx = idx
		w.app.switchProfile(idx)
		w.refreshStatus()
		return nil
	})

	_ = w.wv.Bind("api_updateSlot", func(tabIdx, slotIdx int, key string, strategy, interval, delay int, random bool) error {
		if tabIdx < 0 || tabIdx >= maxProfiles || slotIdx < 0 || slotIdx >= numSlots {
			return nil
		}
		slot := &w.profileSlots[tabIdx][slotIdx]
		slot.Key = key
		slot.Strategy = strategy
		slot.IntervalMS = interval
		slot.DelayMS = delay
		slot.RandomDelay = random

		w.saveToState(true)
		return nil
	})

	_ = w.wv.Bind("api_updateProfileMeta", func(tabIdx int, name, hk, mode string) error {
		if tabIdx < 0 || tabIdx >= maxProfiles {
			return nil
		}
		if name == "" {
			name = fmt.Sprintf("配置 %d", tabIdx+1)
		}
		w.profileNames[tabIdx] = name
		w.profileHKs[tabIdx] = hk
		w.profileModes[tabIdx] = mode

		w.saveToState(true)
		w.refreshStatus()
		return nil
	})

	_ = w.wv.Bind("api_updateToggle", func(token string) error {
		w.raw.Toggle = token
		w.saveToState(true)
		w.refreshStatus()
		return nil
	})

	_ = w.wv.Bind("api_updateStartMode", func(mode int) error {
		w.startMode = mode
		w.saveToState(true)
		w.refreshStatus()
		return nil
	})

	_ = w.wv.Bind("api_updateQuickPause", func(enabled bool, trigger, mouseKey, action, delayMS int) error {
		qp := store.QuickPauseSettings{
			Enabled:  enabled,
			Trigger:  trigger,
			MouseKey: mouseKey,
			Action:   action,
			DelayMS:  delayMS,
		}
		w.app.settings.QuickPause = qp
		_ = store.SaveSettings(w.app.dir, *w.app.settings)
		w.app.updateQuickPause(qp)
		return nil
	})

	_ = w.wv.Bind("api_updateHelpers", func(hotkey string, speedMS, reforgeTarget int, devMode bool, enabled []string) error {
		hs := w.app.settings.Helpers
		hs.Hotkey = hotkey
		hs.DelayMS = speedMS
		hs.ReforgeTarget = reforgeTarget
		hs.DevMode = devMode
		hs.Enabled = map[string]bool{}
		for _, id := range enabled {
			hs.Enabled[id] = true
		}
		w.app.settings.Helpers = hs
		_ = store.SaveSettings(w.app.dir, *w.app.settings)
		return nil
	})

	_ = w.wv.Bind("api_updatePreference", func(smart, sound, force bool, forceKey string) error {
		w.app.settings.SmartPause = smart
		w.app.settings.SoundCue = sound

		gk := w.app.settings.GameKeyMap(w.app.settings.CurrentGame)
		gk.ForceStandEnabled = force
		gk.ForceStandKey = forceKey
		w.app.settings.SetGameKeyMap(w.app.settings.CurrentGame, gk)

		_ = store.SaveSettings(w.app.dir, *w.app.settings)
		w.app.updateSmartPause(smart)

		if snap := w.app.st.snapshot(); snap.Engine != nil {
			w.app.setupForceStand(snap.Engine)
		}
		return nil
	})

	_ = w.wv.Bind("api_importClipboard", func() (bool, error) {
		w.app.importFromClipboard()
		return true, nil
	})

	_ = w.wv.Bind("api_exportClipboard", func() error {
		w.exportCode()
		return nil
	})

	_ = w.wv.Bind("api_openLogWindow", func() error {
		w.app.showLogWindow()
		return nil
	})

	_ = w.wv.Bind("api_minimize", func() error {
		procShowWindow.Call(w.hwnd, uintptr(SW_MINIMIZE))
		return nil
	})

	_ = w.wv.Bind("api_close", func() error {
		procShowWindow.Call(w.hwnd, uintptr(SW_HIDE))
		if !w.hiddenHintShown {
			w.hiddenHintShown = true
			if w.app.ni != nil {
				_ = w.app.ni.ShowInfo(appName, "已最小化到托盘，宏会继续运行。\n双击托盘图标可重新打开。")
			}
		}
		return nil
	})
}

func (w *mainWindow) buildFrontendState() *frontendState {
	snap := w.app.st.snapshot()
	s := &frontendState{
		CurrentGame:       w.app.settings.CurrentGame,
		CurrentProfileIdx: w.profileIdx,
		ProfileNames:      make([]string, maxProfiles),
		Profiles:          make([]frontendProfile, maxProfiles),
		ToggleKey:         config.DefaultToggleKey,
		StartMode:         w.startMode,
		SmartPause:        w.app.settings.SmartPause,
		SoundCue:          w.app.settings.SoundCue,
		TargetProcess:     w.app.targetProcessOf(w.app.settings.CurrentGame),
	}

	if w.raw.Toggle != "" {
		s.ToggleKey = keyNameOfToken(w.raw.Toggle)
	}

	for t := 0; t < maxProfiles; t++ {
		s.ProfileNames[t] = w.profileNames[t]
		slots := make([]frontendSlot, numSlots)
		for i := 0; i < numSlots; i++ {
			sd := w.profileSlots[t][i]
			slots[i] = frontendSlot{
				Name:        sd.Name,
				Key:         keyNameOfToken(sd.Key),
				Strategy:    sd.Strategy,
				IntervalMS:  sd.IntervalMS,
				DelayMS:     sd.DelayMS,
				RandomDelay: sd.RandomDelay,
			}
		}
		s.Profiles[t] = frontendProfile{
			Name:  w.profileNames[t],
			HK:    w.profileHKs[t],
			Mode:  w.profileModes[t],
			Slots: slots,
		}
	}

	// 快速暂停
	qp := w.app.settings.QuickPause
	s.QuickPause.Enabled = qp.Enabled
	s.QuickPause.Trigger = qp.Trigger
	s.QuickPause.MouseKey = qp.MouseKey
	s.QuickPause.Action = qp.Action
	s.QuickPause.DelayMS = qp.DelayMS

	// 城镇助手
	hs := w.app.settings.Helpers
	s.Helpers.Hotkey = hs.Hotkey
	s.Helpers.DelayMS = hs.DelayMS
	s.Helpers.ReforgeTarget = hs.ReforgeTarget
	s.Helpers.DevMode = hs.DevMode
	s.Helpers.Enabled = hs.Enabled

	allH := helper.All()
	s.AllHelpers = make([]helperMeta, len(allH))
	for i, h := range allH {
		s.AllHelpers[i] = helperMeta{ID: h.ID(), Name: h.Name()}
	}

	// 强制站立
	gk := w.app.settings.GameKeyMap(w.app.settings.CurrentGame)
	s.ForceStandEnabled = gk.ForceStandEnabled
	s.ForceStandKey = w.forceStandKeyName()

	// 运行状态
	if snap.Engine != nil {
		s.MacroRunning = snap.Engine.Enabled()
	}

	return s
}

func (w *mainWindow) forceStandKeyName() string {
	gk := w.app.settings.GameKeyMap(w.app.settings.CurrentGame)
	if gk.ForceStandKey != "" {
		return gk.ForceStandKey
	}
	if adapter, ok := games.For(w.app.settings.CurrentGame); ok {
		return adapter.DefaultForceStandKey()
	}
	return "Shift"
}

func (a *app) showMainWindow() {
	if a.win == nil {
		if err := a.createMainWindow(); err != nil {
			a.log.Printf("[窗口] 创建失败：%v", err)
			return
		}
	}
	if a.win.hwnd != 0 {
		procShowWindow.Call(a.win.hwnd, uintptr(SW_SHOW))
		procSetForeground.Call(a.win.hwnd)
		a.win.refreshStatus()
	}
}

func (w *mainWindow) loadFromState() {
	snap := w.app.st.snapshot()
	w.loading = true
	defer func() { w.loading = false }()

	if snap.Raw != nil {
		w.raw = *snap.Raw
	} else {
		w.raw = defaultConfig()
		w.raw.Game = w.app.settings.CurrentGame
	}

	w.startMode = snap.StartMode

	for i := 0; i < maxProfiles; i++ {
		if i < len(w.raw.Profiles) {
			p := w.raw.Profiles[i]
			w.profileSlots[i] = profileToSlots(p)
			w.profileNames[i] = p.Name
			if w.profileNames[i] == "" {
				w.profileNames[i] = fmt.Sprintf("配置 %d", i+1)
			}
			w.profileHKs[i] = p.HK
			w.profileModes[i] = p.Mode
		} else {
			w.profileSlots[i] = defaultSlots()
			w.profileNames[i] = fmt.Sprintf("配置 %d", i+1)
			w.profileHKs[i] = ""
			w.profileModes[i] = config.ModeConcurrent
		}
	}

	w.profileIdx = 0
	if snap.Engine != nil {
		w.profileIdx = snap.Engine.ProfileIndex()
	}
	if w.profileIdx < 0 || w.profileIdx >= maxProfiles {
		w.profileIdx = 0
	}

	w.refreshStatus()
}

func (w *mainWindow) refreshStatus() {
	if w.wv == nil {
		return
	}
	st := w.buildFrontendState()
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	w.wv.Dispatch(func() {
		w.wv.Eval("if(window.onStateUpdate){window.onStateUpdate(" + string(b) + ");}")
	})
}

func (w *mainWindow) saveToState(silent bool) error {
	profs := make([]config.RawProfile, maxProfiles)
	for i := 0; i < maxProfiles; i++ {
		name := w.profileNames[i]
		if name == "" {
			name = fmt.Sprintf("配置 %d", i+1)
		}
		profs[i] = slotsToProfile(w.profileSlots[i], name, w.profileHKs[i], w.profileModes[i])
	}

	w.raw.Version = config.Version
	w.raw.Game = w.app.settings.CurrentGame
	w.raw.Profiles = profs
	if w.raw.Toggle == "" {
		w.raw.Toggle = config.DefaultToggleKey
	}

	raw := w.raw
	parsed, err := config.Resolve(raw)
	if err != nil {
		return err
	}

	if err := w.app.applyConfig(&raw, parsed, w.profileIdx, w.startMode); err != nil {
		return err
	}
	return nil
}

func (w *mainWindow) exportCode() {
	profs := make([]config.RawProfile, maxProfiles)
	for i := 0; i < maxProfiles; i++ {
		name := w.profileNames[i]
		if name == "" {
			name = fmt.Sprintf("配置 %d", i+1)
		}
		profs[i] = slotsToProfile(w.profileSlots[i], name, w.profileHKs[i], w.profileModes[i])
	}
	w.raw.Profiles = profs

	code, err := config.Encode(w.raw)
	if err != nil {
		walk.MsgBox(w.app.owner(), appName, "生成分享码失败：\n\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if err := walk.Clipboard().SetText(code); err != nil {
		walk.MsgBox(w.app.owner(), appName, "写入剪贴板失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	walk.MsgBox(w.app.owner(), appName, "分享码已复制到剪贴板！可以直接发送给好友。", walk.MsgBoxOK|walk.MsgBoxIconInformation)
}

func profileToSlots(prof config.RawProfile) [numSlots]skillSlotData {
	slots := defaultSlots()
	usedSlots := make([]bool, numSlots)

	for _, r := range prof.Rules {
		kind, targetKey, _ := strings.Cut(r.Action, ":")
		if targetKey == "" {
			targetKey = r.Action
		}

		slotIdx := -1
		for i := 0; i < numSlots; i++ {
			if !usedSlots[i] && (strings.EqualFold(slots[i].DefaultKey, targetKey) || strings.EqualFold(slots[i].Key, targetKey)) {
				slotIdx = i
				break
			}
		}
		if slotIdx == -1 {
			for i := 0; i < numSlots; i++ {
				if !usedSlots[i] && slots[i].Strategy == strategyDisabled {
					slotIdx = i
					break
				}
			}
		}
		if slotIdx == -1 {
			continue
		}

		usedSlots[slotIdx] = true
		slots[slotIdx].Key = targetKey
		if r.IntervalMS > 0 {
			slots[slotIdx].IntervalMS = r.IntervalMS
		}
		slots[slotIdx].DelayMS = r.DelayMS
		slots[slotIdx].RandomDelay = r.RandomDelay

		switch {
		case r.Strategy != config.StrategyDisabled:
			slots[slotIdx].Strategy = r.Strategy
		default:
			switch kind {
			case "hold":
				slots[slotIdx].Strategy = strategyHold
			case "tap":
				if r.IntervalMS == 300 {
					slots[slotIdx].Strategy = strategyBuff
				} else {
					slots[slotIdx].Strategy = strategySpam
				}
			default:
				slots[slotIdx].Strategy = strategySpam
			}
		}
	}

	return slots
}

func clampInterval(v, fallback int) int {
	if v < 20 {
		return fallback
	}
	if v > 600000 {
		return 600000
	}
	return v
}

func slotsToProfile(slots [numSlots]skillSlotData, name string, switchKey string, mode string) config.RawProfile {
	rules := make([]config.RawRule, 0, numSlots)

	for _, s := range slots {
		if s.Strategy == strategyDisabled || s.Key == "" {
			continue
		}

		switch s.Strategy {
		case strategyHold:
			rules = append(rules, config.RawRule{
				Trigger:  "t",
				Action:   "hold:" + s.Key,
				Strategy: config.StrategyHold,
			})
		case strategySpam:
			rule := config.RawRule{
				Trigger:    "t",
				Action:     "tap:" + s.Key,
				IntervalMS: clampInterval(s.IntervalMS, 20),
				Strategy:   config.StrategySpam,
			}
			if s.DelayMS != 0 {
				rule.DelayMS = s.DelayMS
				rule.RandomDelay = s.RandomDelay
			}
			rules = append(rules, rule)
		case strategyBuff:
			rules = append(rules, config.RawRule{
				Trigger:    "t",
				Action:     "tap:" + s.Key,
				IntervalMS: clampInterval(s.IntervalMS, 300),
				Strategy:   config.StrategyBuff,
			})
		}
	}

	if len(rules) == 0 {
		rules = append(rules, config.RawRule{Trigger: "t", Action: "tap:1", IntervalMS: 250})
	}

	return config.RawProfile{
		Name:  name,
		HK:    switchKey,
		Mode:  mode,
		Rules: rules,
	}
}

func slotExplanation(s skillSlotData) string {
	keyText := keyNameOfToken(s.Key)
	switch s.Strategy {
	case 0:
		return "手动释放（不自动触发）"
	case 1:
		return fmt.Sprintf("持续保持按下「%s」（旋风斩/扫射/引导类技能专长）", keyText)
	case 2:
		return fmt.Sprintf("每 %dms 持续连点「%s」", s.IntervalMS, keyText)
	case 3:
		return fmt.Sprintf("智能屏幕找色保持「%s」Buff（发光环未结束绝不按键）", keyText)
	default:
		return ""
	}
}

func (w *mainWindow) fatal(err error) {
	if w != nil && w.app != nil && w.app.log != nil {
		w.app.log.Printf("[窗口] 界面操作失败：%v", err)
	}
}

//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/imsxx/diablo-lazy-macro/internal/config"
	"github.com/imsxx/diablo-lazy-macro/internal/games"
	"github.com/imsxx/diablo-lazy-macro/internal/keys"

	"github.com/lxn/walk"
	decl "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

const (
	maxProfiles = 4
	numSlots    = 6

	fixedWinWidth  = 860
	fixedWinHeight = 540
)

// 槽位释放策略取值，顺序与 strategyNames 一一对应；取值以协议常量 config.Strategy* 为准。
const (
	strategyDisabled = config.StrategyDisabled // 禁用：不自动触发
	strategyHold     = config.StrategyHold     // 按住不放：战斗中持续按下（引导类技能）
	strategySpam     = config.StrategySpam     // 连点：按间隔持续点击
	strategyBuff     = config.StrategyBuff     // 保持Buff：屏幕守卫检测或定时续航
)

var strategyNames = []string{
	"禁用",
	"按住不放",
	"连点",
	"保持Buff",
}

var startModeNames = []string{
	"懒人模式 (开关切换)",
	"仅按下时 (按住生效)",
}

type skillSlotData struct {
	Name       string
	DefaultKey string
	Key        string
	Strategy   int // 0:禁用, 1:按住不放, 2:连点, 3:保持Buff
	IntervalMS int
}

// defaultSlots 按游戏适配器给出的槽位定义构造初始槽位（默认策略为禁用、间隔 250ms）。
func defaultSlots() [numSlots]skillSlotData {
	var out [numSlots]skillSlotData
	defs := games.DefaultAdapter().Slots()
	for i := 0; i < numSlots && i < len(defs); i++ {
		out[i] = skillSlotData{
			Name:       defs[i].Name,
			DefaultKey: defs[i].DefaultKey,
			Key:        defs[i].DefaultKey,
			IntervalMS: 250,
		}
	}
	return out
}

// mainWindow 是程序主窗口：左右双栏仪表盘架构（左栏方案与技能、右栏全局总控与状态、底栏激活状态）。
type mainWindow struct {
	app *app
	mw  *walk.MainWindow

	// 右栏全局控制与状态
	gameCombo      *walk.ComboBox
	toggleKeyBtn   *walk.PushButton
	startModeCombo *walk.ComboBox
	statusLabel    *walk.Label
	targetProcLbl  *walk.Label
	activeProfLbl  *walk.Label

	// 左栏原生 TabWidget (4套独立方案页)
	tabWidget *walk.TabWidget

	// 4个配置 × 6个核心技能槽位的行控件
	slotKeyBtns       [maxProfiles][numSlots]*walk.PushButton
	slotStrategyComs  [maxProfiles][numSlots]*walk.ComboBox
	slotIntervalEdits [maxProfiles][numSlots]*walk.LineEdit

	// 4个配置对应的设置项
	profileNameEdits [maxProfiles]*walk.LineEdit
	switchKeyBtns    [maxProfiles]*walk.PushButton

	// 内存状态：4个配置各对应一套 6 个技能槽位
	profileSlots [maxProfiles][numSlots]skillSlotData
	profileNames [maxProfiles]string
	profileHKs   [maxProfiles]string
	startMode    int

	profileIdx int
	raw        config.Raw
	loading    bool

	// 快捷键捕获状态
	capturingTab  int
	capturingSlot int

	hiddenHintShown bool
}

func (a *app) createMainWindow() error {
	w := &mainWindow{
		app:           a,
		capturingTab:  -1,
		capturingSlot: -1,
	}

	// 1. 获取屏幕物理分辨率，居中计算固定坐标
	screenWidth := int(win.GetSystemMetrics(win.SM_CXSCREEN))
	screenHeight := int(win.GetSystemMetrics(win.SM_CYSCREEN))
	posX := (screenWidth - fixedWinWidth) / 2
	posY := (screenHeight - fixedWinHeight) / 2
	if posX < 0 {
		posX = 50
	}
	if posY < 0 {
		posY = 50
	}

	// 2. 声明式构建窗口（Visible: false，避免白屏闪烁；固定尺寸，锁死宽高）
	err := decl.MainWindow{
		AssignTo:        &w.mw,
		Title:           fmt.Sprintf("%s v%s", appName, version),
		Icon:            a.icon,
		Font:            decl.Font{Family: "Microsoft YaHei UI", PointSize: 9},
		Bounds:          decl.Rectangle{X: posX, Y: posY, Width: fixedWinWidth, Height: fixedWinHeight},
		Size:            decl.Size{Width: fixedWinWidth, Height: fixedWinHeight},
		MinSize:         decl.Size{Width: fixedWinWidth, Height: fixedWinHeight},
		MaxSize:         decl.Size{Width: fixedWinWidth, Height: fixedWinHeight},
		Visible:         false,
		DoubleBuffering: true,
		Layout:          decl.VBox{Margins: decl.Margins{Left: 10, Top: 8, Right: 10, Bottom: 6}, Spacing: 6},
		OnKeyDown:       w.handleGlobalKeyDown,
		Children: []decl.Widget{
			// 主体区域：经典左右双栏架构 (左栏 530px 方案技能，右栏 300px 全局控制)
			decl.Composite{
				Layout: decl.HBox{MarginsZero: true, Spacing: 8},
				Children: []decl.Widget{
					// ================= 左栏：方案技能与按键设置 =================
					decl.Composite{
						MinSize: decl.Size{Width: 525},
						MaxSize: decl.Size{Width: 525},
						Layout:  decl.VBox{MarginsZero: true, Spacing: 4},
						Children: []decl.Widget{
							decl.TabWidget{
								AssignTo:              &w.tabWidget,
								OnCurrentIndexChanged: w.onTabChanged,
								Pages: []decl.TabPage{
									w.createTabPage(0),
									w.createTabPage(1),
									w.createTabPage(2),
									w.createTabPage(3),
								},
							},
						},
					},

					// ================= 右栏：全局仪表盘与总控制 =================
					decl.Composite{
						MinSize: decl.Size{Width: 295},
						MaxSize: decl.Size{Width: 295},
						Layout:  decl.VBox{MarginsZero: true, Spacing: 6},
						Children: []decl.Widget{
							// 1. 战斗宏启停总控卡片 (视觉重心，红字加粗突出)
							decl.GroupBox{
								Title:  "战斗宏总控",
								Layout: decl.VBox{Margins: decl.Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}, Spacing: 6},
								Children: []decl.Widget{
									decl.Composite{
										Layout: decl.HBox{MarginsZero: true, Spacing: 6},
										Children: []decl.Widget{
											decl.Label{
												Text:    "★ 启动快捷键：",
												Font:    decl.Font{Bold: true, PointSize: 9},
												MinSize: decl.Size{Width: 100},
											},
											decl.PushButton{
												AssignTo:  &w.toggleKeyBtn,
												Text:      "F1",
												Font:      decl.Font{Bold: true, PointSize: 10},
												MinSize:   decl.Size{Width: 90, Height: 30},
												MaxSize:   decl.Size{Width: 90, Height: 30},
												OnClicked: func() { w.startCapture(-1, 100) },
												OnKeyDown: w.handleGlobalKeyDown,
												ToolTipText: "左键点击捕获按键；右键菜单可快速绑定鼠标侧键(X1/X2)或中键",
												ContextMenuItems: []decl.MenuItem{
													decl.Action{Text: "🖱️ 设为 鼠标侧键1 (X1)", OnTriggered: func() { w.setToggleKey("X1") }},
													decl.Action{Text: "🖱️ 设为 鼠标侧键2 (X2)", OnTriggered: func() { w.setToggleKey("X2") }},
													decl.Action{Text: "🖱️ 设为 鼠标中键 (MMB)", OnTriggered: func() { w.setToggleKey("MMB") }},
													decl.Separator{},
													decl.Action{Text: "⌨️ 设为 F1", OnTriggered: func() { w.setToggleKey("F1") }},
													decl.Action{Text: "⌨️ 设为 F2", OnTriggered: func() { w.setToggleKey("F2") }},
													decl.Action{Text: "⌨️ 设为 ` (波浪号)", OnTriggered: func() { w.setToggleKey("`") }},
												},
											},
											decl.HSpacer{},
										},
									},
									decl.Composite{
										Layout: decl.HBox{MarginsZero: true, Spacing: 6},
										Children: []decl.Widget{
											decl.Label{Text: "宏启动方式：", MinSize: decl.Size{Width: 80}},
											decl.ComboBox{
												AssignTo:              &w.startModeCombo,
												MinSize:               decl.Size{Width: 160},
												MaxSize:               decl.Size{Width: 160},
												OnCurrentIndexChanged: w.onStartModeChanged,
												ToolTipText:           "懒人模式：按一下开，再按一下关\n仅按下时：按住快捷键(如侧键)触发，松开立即停止",
											},
											decl.HSpacer{},
										},
									},
								},
							},

							// 2. 目标游戏与运行状态卡片
							decl.GroupBox{
								Title:  "目标游戏与状态",
								Layout: decl.VBox{Margins: decl.Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}, Spacing: 6},
								Children: []decl.Widget{
									decl.Composite{
										Layout: decl.HBox{MarginsZero: true, Spacing: 6},
										Children: []decl.Widget{
											decl.Label{Text: "当前游戏：", MinSize: decl.Size{Width: 70}},
											decl.ComboBox{
												AssignTo:              &w.gameCombo,
												MinSize:               decl.Size{Width: 170},
												MaxSize:               decl.Size{Width: 170},
												OnCurrentIndexChanged: w.onGameChanged,
											},
											decl.HSpacer{},
										},
									},
									decl.Label{
										AssignTo: &w.statusLabel,
										Text:     "🟢 宏已就绪（前台自动生效）",
										Font:     decl.Font{Bold: true},
									},
									decl.Label{
										AssignTo: &w.targetProcLbl,
										Text:     "🎮 目标进程：Diablo IV.exe",
									},
								},
							},

							// 3. 快捷操作卡片
							decl.GroupBox{
								Title:  "快捷操作",
								Layout: decl.VBox{Margins: decl.Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}, Spacing: 6},
								Children: []decl.Widget{
									decl.PushButton{
										Text:      "💾 保存并应用配置",
										Font:      decl.Font{Bold: true, PointSize: 9},
										MinSize:   decl.Size{Height: 28},
										OnClicked: w.saveAndApply,
									},
									decl.Composite{
										Layout: decl.HBox{MarginsZero: true, Spacing: 6},
										Children: []decl.Widget{
											decl.PushButton{Text: "📋 导入分享码", MinSize: decl.Size{Height: 26}, OnClicked: func() { a.importFromClipboard() }},
											decl.PushButton{Text: "📤 复制分享码", MinSize: decl.Size{Height: 26}, OnClicked: w.exportCode},
										},
									},
									decl.PushButton{Text: "📄 查看运行日志", MinSize: decl.Size{Height: 26}, OnClicked: a.showLogWindow},
								},
							},
							decl.VSpacer{},
						},
					},
				},
			},

			// 底部状态栏：显式标红突出当前激活配置与安全保障
			decl.Composite{
				Layout: decl.HBox{Margins: decl.Margins{Left: 4, Top: 2, Right: 4, Bottom: 2}, Spacing: 10},
				Children: []decl.Widget{
					decl.Label{
						AssignTo: &w.activeProfLbl,
						Text:     "当前激活配置：配置 1",
						Font:     decl.Font{Bold: true, PointSize: 9},
					},
					decl.HSpacer{},
					decl.Label{
						Text: "Win32 SendInput · 零驱动零注入 · 智能找色续航已就绪",
					},
				},
			},
		},
	}.Create()
	if err != nil {
		return err
	}

	// 3. 锁定 Win32 窗口样式：彻底移除 WS_MAXIMIZEBOX (最大化按钮) 和 WS_THICKFRAME (拖拽边框)
	hwnd := w.mw.Handle()
	style := win.GetWindowLong(hwnd, win.GWL_STYLE)
	style &^= (win.WS_MAXIMIZEBOX | win.WS_THICKFRAME)
	win.SetWindowLong(hwnd, win.GWL_STYLE, style)
	win.SetWindowPos(hwnd, 0, int32(posX), int32(posY), int32(fixedWinWidth), int32(fixedWinHeight),
		win.SWP_NOZORDER|win.SWP_FRAMECHANGED)

	w.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		*canceled = true
		w.mw.Hide()
		if !w.hiddenHintShown {
			w.hiddenHintShown = true
			if a.ni != nil {
				if err := a.ni.ShowInfo(appName, "已最小化到托盘，宏会继续运行。\n双击托盘图标可以再次打开本窗口。"); err != nil {
					a.log.Printf("[托盘] 气泡提示失败：%v", err)
				}
			}
		}
	})

	a.win = w

	// 4. 在完全加载配置、调整好坐标并完成初次渲染后再显示，彻底杜绝白屏闪烁
	w.loadFromState()
	w.mw.Show()
	a.log.Printf("[窗口] 主窗口已打开（固定尺寸 %dx%d，左右双栏布局）", fixedWinWidth, fixedWinHeight)
	return nil
}

func (w *mainWindow) createTabPage(tabIdx int) decl.TabPage {
	t := tabIdx
	return decl.TabPage{
		Title:  fmt.Sprintf("配置 %d", t+1),
		Layout: decl.VBox{Margins: decl.Margins{Left: 6, Top: 6, Right: 6, Bottom: 6}, Spacing: 4},
		Children: []decl.Widget{
			// 按键宏设置 GroupBox (高密度紧凑网格，去除冗长文字，采用 ToolTip)
			decl.GroupBox{
				Title:  "按键宏设置",
				Layout: decl.VBox{Margins: decl.Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 4},
				Children: []decl.Widget{
					decl.Composite{
						Layout: decl.HBox{Spacing: 8},
						Children: []decl.Widget{
							decl.Label{Text: "技能槽位", MinSize: decl.Size{Width: 65}, MaxSize: decl.Size{Width: 65}, Font: decl.Font{Bold: true}},
							decl.Label{Text: "快捷键", MinSize: decl.Size{Width: 80}, MaxSize: decl.Size{Width: 80}, Font: decl.Font{Bold: true}},
							decl.Label{Text: "释放策略", MinSize: decl.Size{Width: 105}, MaxSize: decl.Size{Width: 105}, Font: decl.Font{Bold: true}},
							decl.Label{Text: "执行间隔", MinSize: decl.Size{Width: 80}, MaxSize: decl.Size{Width: 80}, Font: decl.Font{Bold: true}},
							decl.Label{Text: "单位", MinSize: decl.Size{Width: 35}, MaxSize: decl.Size{Width: 35}, Font: decl.Font{Bold: true}},
							decl.HSpacer{},
						},
					},
					w.createSlotRow(t, 0, "技能一："),
					w.createSlotRow(t, 1, "技能二："),
					w.createSlotRow(t, 2, "技能三："),
					w.createSlotRow(t, 3, "技能四："),
					w.createSlotRow(t, 4, "左键技能："),
					w.createSlotRow(t, 5, "右键技能："),
				},
			},
			// 本配置设置 GroupBox
			decl.GroupBox{
				Title:  "本配置设置",
				Layout: decl.HBox{Margins: decl.Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 8},
				Children: []decl.Widget{
					decl.Label{Text: "配置名：", MinSize: decl.Size{Width: 55}},
					decl.LineEdit{
						AssignTo:          &w.profileNameEdits[t],
						MinSize:           decl.Size{Width: 110},
						MaxSize:           decl.Size{Width: 110},
						OnEditingFinished: func() { w.onProfileNameEdited(t) },
						ToolTipText:       "为本套配置起一个容易识别的名称（如：速刷大米 / 攻坚冲层）",
					},
					decl.Label{Text: "切换快捷键：", MinSize: decl.Size{Width: 75}},
					decl.PushButton{
						AssignTo:    &w.switchKeyBtns[t],
						Text:        "无",
						MinSize:     decl.Size{Width: 70, Height: 24},
						MaxSize:     decl.Size{Width: 70, Height: 24},
						OnClicked:   func() { w.startCapture(t, 101) },
						OnKeyDown:   w.handleGlobalKeyDown,
						ToolTipText: "在游戏中按下此键，可无缝秒切至本套配置；右键可清除",
						ContextMenuItems: []decl.MenuItem{
							decl.Action{Text: "❌ 清除切换键 (设为无)", OnTriggered: func() { w.clearSwitchKey(t) }},
						},
					},
					decl.HSpacer{},
				},
			},
		},
	}
}

func (w *mainWindow) createSlotRow(t, s int, labelText string) decl.Widget {
	return decl.Composite{
		Layout: decl.HBox{Spacing: 8},
		Children: []decl.Widget{
			decl.Label{Text: labelText, MinSize: decl.Size{Width: 65}, MaxSize: decl.Size{Width: 65}},
			decl.PushButton{
				AssignTo:  &w.slotKeyBtns[t][s],
				Text:      "1",
				MinSize:   decl.Size{Width: 80, Height: 24},
				MaxSize:   decl.Size{Width: 80, Height: 24},
				OnClicked: func() { w.startCapture(t, s) },
				OnKeyDown: w.handleGlobalKeyDown,
				ToolTipText: "左键点击后按键盘任意键录入；右键弹出菜单快速选择鼠标侧键/按键",
				ContextMenuItems: []decl.MenuItem{
					decl.Action{Text: "🖱️ 设为 鼠标左键 (LMB)", OnTriggered: func() { w.setSlotKey(t, s, "LMB") }},
					decl.Action{Text: "🖱️ 设为 鼠标右键 (RMB)", OnTriggered: func() { w.setSlotKey(t, s, "RMB") }},
					decl.Action{Text: "🖱️ 设为 鼠标中键 (MMB)", OnTriggered: func() { w.setSlotKey(t, s, "MMB") }},
					decl.Action{Text: "🖱️ 设为 鼠标侧键1 (X1)", OnTriggered: func() { w.setSlotKey(t, s, "X1") }},
					decl.Action{Text: "🖱️ 设为 鼠标侧键2 (X2)", OnTriggered: func() { w.setSlotKey(t, s, "X2") }},
					decl.Separator{},
					decl.Action{Text: "⌨️ 恢复默认键", OnTriggered: func() { w.resetSlotKey(t, s) }},
				},
			},
			decl.ComboBox{
				AssignTo:              &w.slotStrategyComs[t][s],
				MinSize:               decl.Size{Width: 105},
				MaxSize:               decl.Size{Width: 105},
				OnCurrentIndexChanged: func() { w.onSlotStrategyChanged(t, s) },
				ToolTipText: "策略说明：\n· 禁用：不自动触发\n· 按住不放：旋风斩/扫射引导技能，战斗中持续按下\n· 连点：按指定间隔持续点击\n· 保持Buff：智能检测发光环(D3)或定时补放，未断绝不抢按",
			},
			decl.LineEdit{
				AssignTo:      &w.slotIntervalEdits[t][s],
				MinSize:       decl.Size{Width: 80},
				MaxSize:       decl.Size{Width: 80},
				OnTextChanged: func() { w.onSlotIntervalChanged(t, s) },
				ToolTipText:   "执行间隔（毫秒）：\n· 连点模式：技能连发的间隔\n· 保持Buff模式：屏幕找色采样的频率或定时续航周期",
			},
			decl.Label{
				Text:    "毫秒",
				MinSize: decl.Size{Width: 35},
				MaxSize: decl.Size{Width: 35},
			},
			decl.HSpacer{},
		},
	}
}

// showMainWindow 由托盘菜单与双击托盘图标调用。
func (a *app) showMainWindow() {
	if a.win == nil {
		if err := a.createMainWindow(); err != nil {
			a.log.Printf("[窗口] 创建失败：%v", err)
			walk.MsgBox(a.mw, appName, "打开主窗口失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		}
		return
	}
	a.win.mw.Show()
	if err := a.win.mw.SetFocus(); err != nil {
		a.log.Printf("[窗口] 置前失败：%v", err)
	}
}

// loadFromState 用运行期状态初始化并重绘整个窗口。
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
		} else {
			w.profileSlots[i] = defaultSlots()
			w.profileNames[i] = fmt.Sprintf("配置 %d", i+1)
			w.profileHKs[i] = ""
		}
	}

	w.profileIdx = 0
	if snap.Engine != nil {
		w.profileIdx = snap.Engine.ProfileIndex()
	}
	if w.profileIdx < 0 || w.profileIdx >= maxProfiles {
		w.profileIdx = 0
	}

	if err := w.mw.SetTitle(fmt.Sprintf("%s v%s — %s", appName, version, gameDisplayName(w.app.settings.CurrentGame))); err != nil {
		w.app.log.Printf("[窗口] 设置标题失败：%v", err)
	}

	w.setCombo(w.gameCombo, gameNames(), gameIndex(w.app.settings.CurrentGame))
	w.setCombo(w.startModeCombo, startModeNames, w.startMode)

	for t := 0; t < maxProfiles; t++ {
		for s := 0; s < numSlots; s++ {
			w.setCombo(w.slotStrategyComs[t][s], strategyNames, 0)
		}
	}

	// 将内存数据刷新到每个 Tab 对应的控件中
	for t := 0; t < maxProfiles; t++ {
		w.setText(w.profileNameEdits[t], w.profileNames[t])
		if w.tabWidget != nil && t < w.tabWidget.Pages().Len() {
			_ = w.tabWidget.Pages().At(t).SetTitle(w.profileNames[t])
		}
		if w.profileHKs[t] == "" {
			w.setButtonText(w.switchKeyBtns[t], "无")
		} else {
			w.setButtonText(w.switchKeyBtns[t], keyNameOfToken(w.profileHKs[t]))
		}

		for s := 0; s < numSlots; s++ {
			slot := w.profileSlots[t][s]
			w.setButtonText(w.slotKeyBtns[t][s], keyNameOfToken(slot.Key))
			_ = w.slotStrategyComs[t][s].SetCurrentIndex(slot.Strategy)
			switch slot.Strategy {
			case 0, 1: // 禁用, 按住不放
				w.setText(w.slotIntervalEdits[t][s], "—")
				w.slotIntervalEdits[t][s].SetEnabled(false)
			case 2: // 连点
				w.setText(w.slotIntervalEdits[t][s], strconv.Itoa(slot.IntervalMS))
				w.slotIntervalEdits[t][s].SetEnabled(true)
			case 3: // 保持Buff
				if slot.IntervalMS <= 0 {
					slot.IntervalMS = 300
				}
				w.setText(w.slotIntervalEdits[t][s], strconv.Itoa(slot.IntervalMS))
				w.slotIntervalEdits[t][s].SetEnabled(true)
			}
		}
	}

	if w.tabWidget != nil && w.tabWidget.CurrentIndex() != w.profileIdx {
		_ = w.tabWidget.SetCurrentIndex(w.profileIdx)
	}

	w.refreshGlobalViews()
}

func (w *mainWindow) refreshGlobalViews() {
	snap := w.app.st.snapshot()
	toggleKey := w.raw.Toggle
	if toggleKey == "" {
		toggleKey = "F1"
	}
	if snap.ToggleVK != 0 {
		toggleKey = keyName(snap.ToggleVK)
	}
	w.setButtonText(w.toggleKeyBtn, toggleKey)

	for t := 0; t < maxProfiles; t++ {
		title := w.profileNames[t]
		if title == "" {
			title = fmt.Sprintf("配置 %d", t+1)
		}
		if w.tabWidget != nil && t < w.tabWidget.Pages().Len() {
			_ = w.tabWidget.Pages().At(t).SetTitle(title)
		}
	}

	w.refreshStatus()
}

// refreshStatus 刷新宏运行状态文本与底栏信息。
func (w *mainWindow) refreshStatus() {
	if w.mw == nil {
		return
	}
	snap := w.app.st.snapshot()
	toggleName := "F1"
	if snap.ToggleVK != 0 {
		toggleName = keyName(snap.ToggleVK)
	} else if w.raw.Toggle != "" {
		toggleName = keyNameOfToken(w.raw.Toggle)
	}

	// 更新底栏激活配置信息
	profName := fmt.Sprintf("配置 %d", w.profileIdx+1)
	if w.profileIdx >= 0 && w.profileIdx < maxProfiles && w.profileNames[w.profileIdx] != "" {
		profName = w.profileNames[w.profileIdx]
	}
	w.setLabel(w.activeProfLbl, fmt.Sprintf("当前激活配置：%s", profName))
	w.setLabel(w.targetProcLbl, fmt.Sprintf("🎮 目标进程：%s", snap.TargetProcess))

	if snap.StartMode == 1 { // 仅按下时 (按住生效)
		if snap.Engine != nil && snap.Engine.Enabled() {
			w.setLabel(w.statusLabel, fmt.Sprintf("🟢 宏运行中（按住 %s 生效中）", toggleName))
		} else {
			w.setLabel(w.statusLabel, fmt.Sprintf("🟡 按住生效模式（按住 %s 触发，松开即停）", toggleName))
		}
		return
	}

	// 懒人模式 (开关切换)
	switch {
	case snap.Engine == nil:
		w.setLabel(w.statusLabel, "🟢 宏已就绪（进入游戏窗口自动生效）")
	case snap.Engine.Enabled():
		w.setLabel(w.statusLabel, fmt.Sprintf("🟢 宏已就绪（前台自动生效 · 按 %s 暂停）", toggleName))
	default:
		w.setLabel(w.statusLabel, fmt.Sprintf("⏸️ 宏已暂停（在游戏内按 %s 恢复运行）", toggleName))
	}
}

func (w *mainWindow) onTabChanged() {
	if w.loading || w.tabWidget == nil {
		return
	}
	idx := w.tabWidget.CurrentIndex()
	if idx < 0 || idx >= maxProfiles || idx == w.profileIdx {
		return
	}
	w.flushAllTabs()
	w.profileIdx = idx
	w.app.switchProfile(idx)
	w.refreshStatus()
}

func (w *mainWindow) flushAllTabs() {
	for t := 0; t < maxProfiles; t++ {
		if w.profileNameEdits[t] != nil {
			name := strings.TrimSpace(w.profileNameEdits[t].Text())
			if name != "" {
				w.profileNames[t] = name
			}
		}
		for s := 0; s < numSlots; s++ {
			slot := &w.profileSlots[t][s]
			if w.slotStrategyComs[t][s] != nil {
				slot.Strategy = w.slotStrategyComs[t][s].CurrentIndex()
			}
			if w.slotIntervalEdits[t][s] != nil && (slot.Strategy == strategySpam || slot.Strategy == strategyBuff) {
				if n, err := strconv.Atoi(strings.TrimSpace(w.slotIntervalEdits[t][s].Text())); err == nil && n > 0 {
					slot.IntervalMS = n
				}
			}
		}
	}
	if w.startModeCombo != nil {
		w.startMode = w.startModeCombo.CurrentIndex()
	}
}

func (w *mainWindow) onSlotStrategyChanged(t, s int) {
	if w.loading {
		return
	}
	slot := &w.profileSlots[t][s]
	slot.Strategy = w.slotStrategyComs[t][s].CurrentIndex()

	switch slot.Strategy {
	case 0, 1: // 禁用, 按住不放
		w.setText(w.slotIntervalEdits[t][s], "—")
		w.slotIntervalEdits[t][s].SetEnabled(false)
	case 2: // 连点
		w.slotIntervalEdits[t][s].SetEnabled(true)
		if slot.IntervalMS <= 0 {
			slot.IntervalMS = 250
		}
		w.setText(w.slotIntervalEdits[t][s], strconv.Itoa(slot.IntervalMS))
	case 3: // 保持Buff
		w.slotIntervalEdits[t][s].SetEnabled(true)
		if slot.IntervalMS <= 0 {
			slot.IntervalMS = 300
		}
		w.setText(w.slotIntervalEdits[t][s], strconv.Itoa(slot.IntervalMS))
	}
}

func (w *mainWindow) onSlotIntervalChanged(t, s int) {
	if w.loading {
		return
	}
	slot := &w.profileSlots[t][s]
	if slot.Strategy != 2 && slot.Strategy != 3 {
		return
	}
	text := strings.TrimSpace(w.slotIntervalEdits[t][s].Text())
	if n, err := strconv.Atoi(text); err == nil && n > 0 {
		slot.IntervalMS = n
	}
}

func (w *mainWindow) onProfileNameEdited(t int) {
	if w.loading {
		return
	}
	name := strings.TrimSpace(w.profileNameEdits[t].Text())
	if name == "" {
		name = fmt.Sprintf("配置 %d", t+1)
	}
	w.profileNames[t] = name
	if w.tabWidget != nil && t < w.tabWidget.Pages().Len() {
		_ = w.tabWidget.Pages().At(t).SetTitle(name)
	}
	w.refreshStatus()
}

func (w *mainWindow) onStartModeChanged() {
	if w.loading {
		return
	}
	w.startMode = w.startModeCombo.CurrentIndex()
	_ = w.saveToState(true)
	w.refreshStatus()
}

// 快捷键录入捕获系统
func (w *mainWindow) startCapture(tabIdx, slotIdx int) {
	if w.capturingSlot == slotIdx && w.capturingTab == tabIdx {
		w.cancelCapture()
		return
	}
	w.cancelCapture()
	w.capturingTab = tabIdx
	w.capturingSlot = slotIdx
	btn := w.targetButton(tabIdx, slotIdx)
	if btn != nil {
		w.setButtonText(btn, "【按键】")
		_ = btn.SetFocus()
	}
}

func (w *mainWindow) cancelCapture() {
	if w.capturingSlot < 0 {
		return
	}
	t := w.capturingTab
	s := w.capturingSlot
	w.capturingTab = -1
	w.capturingSlot = -1
	btn := w.targetButton(t, s)
	if btn == nil {
		return
	}

	switch {
	case s >= 0 && s < numSlots && t >= 0 && t < maxProfiles:
		w.setButtonText(btn, keyNameOfToken(w.profileSlots[t][s].Key))
	case s == 100:
		tk := w.raw.Toggle
		if tk == "" {
			tk = "F1"
		}
		w.setButtonText(btn, keyNameOfToken(tk))
	case s == 101 && t >= 0 && t < maxProfiles:
		hk := w.profileHKs[t]
		if hk == "" {
			w.setButtonText(btn, "无")
		} else {
			w.setButtonText(btn, keyNameOfToken(hk))
		}
	}
}

func (w *mainWindow) targetButton(tabIdx, slotIdx int) *walk.PushButton {
	switch {
	case slotIdx >= 0 && slotIdx < numSlots && tabIdx >= 0 && tabIdx < maxProfiles:
		return w.slotKeyBtns[tabIdx][slotIdx]
	case slotIdx == 100:
		return w.toggleKeyBtn
	case slotIdx == 101 && tabIdx >= 0 && tabIdx < maxProfiles:
		return w.switchKeyBtns[tabIdx]
	default:
		return nil
	}
}

func (w *mainWindow) handleGlobalKeyDown(key walk.Key) {
	if w.capturingSlot < 0 {
		return
	}
	vk := uint16(key)
	if vk == 0x1B { // Escape: 取消录入
		w.cancelCapture()
		return
	}

	token := keys.Label(vk)
	if token == "" {
		return
	}

	t := w.capturingTab
	s := w.capturingSlot
	w.capturingTab = -1
	w.capturingSlot = -1

	switch {
	case s >= 0 && s < numSlots && t >= 0 && t < maxProfiles:
		w.profileSlots[t][s].Key = token
		btn := w.slotKeyBtns[t][s]
		w.setButtonText(btn, keyNameOfToken(token))
	case s == 100:
		w.raw.Toggle = token
		w.setButtonText(w.toggleKeyBtn, keyNameOfToken(token))
		w.refreshStatus()
	case s == 101 && t >= 0 && t < maxProfiles:
		w.profileHKs[t] = token
		w.setButtonText(w.switchKeyBtns[t], keyNameOfToken(token))
	}
}

func (w *mainWindow) setToggleKey(token string) {
	w.cancelCapture()
	w.raw.Toggle = token
	w.setButtonText(w.toggleKeyBtn, keyNameOfToken(token))
	w.refreshStatus()
}

func (w *mainWindow) setSlotKey(t, s int, token string) {
	w.cancelCapture()
	w.profileSlots[t][s].Key = token
	w.setButtonText(w.slotKeyBtns[t][s], keyNameOfToken(token))
}

func (w *mainWindow) resetSlotKey(t, s int) {
	w.cancelCapture()
	slot := &w.profileSlots[t][s]
	slot.Key = slot.DefaultKey
	w.setButtonText(w.slotKeyBtns[t][s], keyNameOfToken(slot.Key))
}

func (w *mainWindow) clearSwitchKey(t int) {
	w.cancelCapture()
	w.profileHKs[t] = ""
	w.setButtonText(w.switchKeyBtns[t], "无")
}

func (w *mainWindow) onGameChanged() {
	if w.loading {
		return
	}
	idx := w.gameCombo.CurrentIndex()
	all := games.All()
	if idx < 0 || idx >= len(all) {
		return
	}
	id := all[idx].ID
	if id == w.app.settings.CurrentGame {
		return
	}

	w.flushAllTabs()
	_ = w.saveToState(true)
	w.app.switchGame(id)
}

func (w *mainWindow) saveToState(silent bool) error {
	w.flushAllTabs()

	profs := make([]config.RawProfile, maxProfiles)
	for i := 0; i < maxProfiles; i++ {
		name := w.profileNames[i]
		if name == "" {
			name = fmt.Sprintf("配置 %d", i+1)
		}
		profs[i] = slotsToProfile(w.profileSlots[i], name, w.profileHKs[i])
	}

	w.raw.Version = config.Version
	w.raw.Game = w.app.settings.CurrentGame
	w.raw.Profiles = profs
	if w.raw.Toggle == "" {
		w.raw.Toggle = "F1"
	}

	raw := w.raw
	parsed, err := config.Resolve(raw)
	if err != nil {
		return err
	}

	if err := w.app.applyConfig(&raw, parsed, w.profileIdx, w.startMode); err != nil {
		return err
	}

	if !silent {
		if warns := config.Warnings(parsed); len(warns) > 0 {
			var sb strings.Builder
			sb.WriteString("配置已保存并生效，但有以下提示：\n\n")
			for _, msg := range warns {
				sb.WriteString("· " + msg + "\n")
			}
			walk.MsgBox(w.mw, appName, sb.String(), walk.MsgBoxOK|walk.MsgBoxIconWarning)
		} else {
			walk.MsgBox(w.mw, appName, "配置已成功保存并立即生效！\n\n进入游戏即可使用。", walk.MsgBoxOK|walk.MsgBoxIconInformation)
		}
	}
	return nil
}

func (w *mainWindow) saveAndApply() {
	if err := w.saveToState(false); err != nil {
		walk.MsgBox(w.mw, appName, "配置校验失败，未能保存：\n\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	w.refreshGlobalViews()
}

func (w *mainWindow) exportCode() {
	w.flushAllTabs()

	profs := make([]config.RawProfile, maxProfiles)
	for i := 0; i < maxProfiles; i++ {
		name := w.profileNames[i]
		if name == "" {
			name = fmt.Sprintf("配置 %d", i+1)
		}
		profs[i] = slotsToProfile(w.profileSlots[i], name, w.profileHKs[i])
	}
	w.raw.Profiles = profs

	code, err := config.Encode(w.raw)
	if err != nil {
		walk.MsgBox(w.mw, appName, "生成分享码失败：\n\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if err := walk.Clipboard().SetText(code); err != nil {
		walk.MsgBox(w.mw, appName, "写入剪贴板失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	walk.MsgBox(w.mw, appName, "分享码已复制到剪贴板！可以直接发送给好友。", walk.MsgBoxOK|walk.MsgBoxIconInformation)
}

// 辅助方法
func (w *mainWindow) fatal(err error) {
	if w != nil && w.app != nil && w.app.log != nil {
		w.app.log.Printf("[窗口] 界面操作失败：%v", err)
	}
	if w != nil && w.mw != nil {
		walk.MsgBox(w.mw, appName, "界面操作失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
	}
}

func (w *mainWindow) setText(le *walk.LineEdit, s string) {
	if le == nil {
		return
	}
	_ = le.SetText(s)
}

func (w *mainWindow) setLabel(l *walk.Label, s string) {
	if l == nil {
		return
	}
	_ = l.SetText(s)
}

func (w *mainWindow) setButtonText(b *walk.PushButton, s string) {
	if b == nil {
		return
	}
	_ = b.SetText(s)
}

func (w *mainWindow) setCombo(cb *walk.ComboBox, items []string, idx int) {
	if cb == nil {
		return
	}
	_ = cb.SetModel(items)
	if idx < 0 || idx >= len(items) {
		idx = 0
	}
	if len(items) > 0 {
		_ = cb.SetCurrentIndex(idx)
	}
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

		// 优先采用配置里显式声明的策略；v1 旧码没有该字段，按动作类型与间隔推断。
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

// clampInterval 把间隔收敛到协议允许的范围；低于下限时按策略取兜底值。
func clampInterval(v, fallback int) int {
	if v < 20 {
		return fallback
	}
	if v > 600000 {
		return 600000
	}
	return v
}

func slotsToProfile(slots [numSlots]skillSlotData, name string, switchKey string) config.RawProfile {
	rules := make([]config.RawRule, 0, numSlots)

	for _, s := range slots {
		if s.Strategy == strategyDisabled || s.Key == "" {
			continue
		}

		switch s.Strategy {
		case strategyHold: // 按住不放
			rules = append(rules, config.RawRule{
				Trigger:  "t",
				Action:   "hold:" + s.Key,
				Strategy: config.StrategyHold,
			})
		case strategySpam: // 连点
			rules = append(rules, config.RawRule{
				Trigger:    "t",
				Action:     "tap:" + s.Key,
				IntervalMS: clampInterval(s.IntervalMS, 20),
				Strategy:   config.StrategySpam,
			})
		case strategyBuff: // 保持Buff：暗黑3 由屏幕守卫按需补放，其他游戏退化为定时续航
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

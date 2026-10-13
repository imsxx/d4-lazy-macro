//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/imsxx/diablo-lazy-macro/internal/config"
	"github.com/imsxx/diablo-lazy-macro/internal/games"
	"github.com/imsxx/diablo-lazy-macro/internal/helper"
	"github.com/imsxx/diablo-lazy-macro/internal/keys"
	"github.com/imsxx/diablo-lazy-macro/internal/store"

	"github.com/lxn/walk"
	decl "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

const (
	maxProfiles = 4
	numSlots    = 6

	// 对齐 D3keyHelper 经典左右双栏比例：宽 870，高 550
	fixedWinWidth  = 870
	fixedWinHeight = 550
)

// 槽位表紧凑列宽（严格按 D3keyHelper 规格定宽，杜绝暴力拉扯）
const (
	colSlotName   = 68
	colKey        = 56
	colStrategy   = 88
	colInterval   = 62
	colDelay      = 52
	colRandom     = 58
	colRowSpacing = 4
	ctrlHeight    = 24
)

const maxDelayUI = 30000

const (
	strategyDisabled = config.StrategyDisabled
	strategyHold     = config.StrategyHold
	strategySpam     = config.StrategySpam
	strategyBuff     = config.StrategyBuff
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

var modeNames = []string{
	"并发按键",
	"循环按键",
}

var (
	quickPauseTriggerNames = []string{"双击", "单击", "压住"}
	quickPauseMouseNames   = []string{"左键", "右键", "中键", "侧键1", "侧键2"}
	quickPauseActionNames  = []string{"暂停按键宏", "暂停且连点左键"}
)

type skillSlotData struct {
	Name        string
	DefaultKey  string
	Key         string
	Strategy    int
	IntervalMS  int
	DelayMS     int
	RandomDelay bool
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

type helperEntry struct {
	id    string
	check *walk.CheckBox
}

var (
	reforgeTargetNames  = []string{"只重铸一次", "普通传奇", "远古传奇", "神圣装备", "太古传奇"}
	reforgeTargetValues = []int{0, 2, 3, 4, 5}
	helperSpeedNames    = []string{"极快 (50ms)", "快速 (100ms)", "标准 (150ms)", "平稳 (250ms)", "宽松 (400ms)"}
	helperSpeedMS       = []int{50, 100, 150, 250, 400}
)

type mainWindow struct {
	app *app
	mw  *walk.MainWindow

	// 顶部 Header 游戏切换按钮
	d3Btn  *walk.PushButton
	d4Btn  *walk.PushButton
	d2rBtn *walk.PushButton

	// 右栏全局战斗宏启动快捷键与启动方式
	statusLabel    *walk.Label
	toggleKeyBtn   *walk.PushButton
	startModeCombo *walk.ComboBox

	// 左栏原生 TabWidget (4套方案页)
	tabWidget *walk.TabWidget

	// 4个配置 × 6个技能槽位的控件
	slotKeyBtns       [maxProfiles][numSlots]*walk.PushButton
	slotStrategyComs  [maxProfiles][numSlots]*walk.ComboBox
	slotIntervalEdits [maxProfiles][numSlots]*walk.LineEdit
	slotDelayEdits    [maxProfiles][numSlots]*walk.LineEdit
	slotRandomChecks  [maxProfiles][numSlots]*walk.CheckBox

	// 4个配置对应的配置项控件
	profileNameEdits [maxProfiles]*walk.LineEdit
	switchKeyBtns    [maxProfiles]*walk.PushButton
	modeComs         [maxProfiles]*walk.ComboBox
	profileModes     [maxProfiles]string

	// 内存状态
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

	// 右栏暗黑3专属助手区域容器与控件
	helperContainer *walk.Composite
	nonD3HintLabel  *walk.Label
	helperHotkey    *walk.LineEdit
	helperSpeed     *walk.ComboBox
	helperDev       *walk.CheckBox
	helperChecks    []helperEntry
	reforgeTarget   *walk.ComboBox

	// 快速暂停控件 (放在左栏下方「本配置设置」)
	quickPauseCheck    *walk.CheckBox
	quickPauseTrigger  *walk.ComboBox
	quickPauseMouseKey *walk.ComboBox
	quickPauseAction   *walk.ComboBox
	quickPauseDelay    *walk.LineEdit

	// 右栏通用偏好设置 (智能暂停、强制站立、提示音)
	smartPauseCheck  *walk.CheckBox
	forceStandCheck  *walk.CheckBox
	forceStandKeyBtn *walk.PushButton
	soundCueCheck    *walk.CheckBox

	// 底部状态栏
	activeProfLbl *walk.Label
	targetProcLbl *walk.Label
	autoSaveLbl   *walk.Label
}

func (a *app) createMainWindow() error {
	w := &mainWindow{
		app:           a,
		capturingTab:  -1,
		capturingSlot: -1,
	}

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

	// 左右双栏布局，左边 510px（技能表+方案设置），右边 330px（全局总控+助手+偏好）
	err := decl.MainWindow{
		AssignTo: &w.mw,
		Title:    fmt.Sprintf("%s v%s", appName, version),
		Icon:     a.icon,
		Font:     decl.Font{Family: "Microsoft YaHei UI", PointSize: 9},
		Bounds:   decl.Rectangle{X: posX, Y: posY, Width: fixedWinWidth, Height: fixedWinHeight},
		Size:     decl.Size{Width: fixedWinWidth, Height: fixedWinHeight},
		MinSize:  decl.Size{Width: fixedWinWidth, Height: fixedWinHeight},
		MaxSize:  decl.Size{Width: fixedWinWidth, Height: fixedWinHeight},
		Visible:  false,
		Layout:   decl.VBox{Margins: decl.Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 5},
		OnKeyDown: w.handleGlobalKeyDown,
		Children: []decl.Widget{
			// ================= 顶栏：游戏选择与快捷动作 =================
			decl.Composite{
				Layout: decl.HBox{MarginsZero: true, Spacing: 6},
				Children: []decl.Widget{
					decl.Label{Text: "目标游戏：", Font: decl.Font{Bold: true}},
					decl.PushButton{
						AssignTo:  &w.d3Btn,
						Text:      "暗黑破坏神3",
						MinSize:   decl.Size{Width: 95, Height: ctrlHeight},
						MaxSize:   decl.Size{Width: 95, Height: ctrlHeight},
						OnClicked: func() { w.switchGameDirect("d3") },
					},
					decl.PushButton{
						AssignTo:  &w.d4Btn,
						Text:      "暗黑破坏神4",
						MinSize:   decl.Size{Width: 95, Height: ctrlHeight},
						MaxSize:   decl.Size{Width: 95, Height: ctrlHeight},
						OnClicked: func() { w.switchGameDirect("d4") },
					},
					decl.PushButton{
						AssignTo:  &w.d2rBtn,
						Text:      "暗黑破坏神2：重制版",
						MinSize:   decl.Size{Width: 130, Height: ctrlHeight},
						MaxSize:   decl.Size{Width: 130, Height: ctrlHeight},
						OnClicked: func() { w.switchGameDirect("d2r") },
					},
					decl.HSpacer{},
					decl.PushButton{
						Text:      "导入分享码",
						MinSize:   decl.Size{Width: 76, Height: ctrlHeight},
						MaxSize:   decl.Size{Width: 76, Height: ctrlHeight},
						OnClicked: func() { a.importFromClipboard() },
					},
					decl.PushButton{
						Text:      "复制分享码",
						MinSize:   decl.Size{Width: 76, Height: ctrlHeight},
						MaxSize:   decl.Size{Width: 76, Height: ctrlHeight},
						OnClicked: w.exportCode,
					},
					decl.PushButton{
						Text:      "运行日志",
						MinSize:   decl.Size{Width: 64, Height: ctrlHeight},
						MaxSize:   decl.Size{Width: 64, Height: ctrlHeight},
						OnClicked: a.showLogWindow,
					},
				},
			},

			// ================= 主体：左右双栏仪表盘 =================
			decl.Composite{
				Layout: decl.HBox{MarginsZero: true, Spacing: 8},
				Children: []decl.Widget{
					// ---------- 左栏：方案技能表 + 方案设置 (515px) ----------
					decl.Composite{
						MinSize: decl.Size{Width: 515},
						MaxSize: decl.Size{Width: 515},
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

					// ---------- 右栏：全局战斗宏总控 + 辅助功能 + 偏好 (335px) ----------
					decl.Composite{
						MinSize: decl.Size{Width: 335},
						MaxSize: decl.Size{Width: 335},
						Layout:  decl.VBox{MarginsZero: true, Spacing: 6},
						Children: []decl.Widget{
							// 1. 战斗宏启动快捷键 (醒目红字对齐 D3keyHelper)
							decl.Composite{
								Layout: decl.HBox{MarginsZero: true, Spacing: 6},
								Children: []decl.Widget{
									decl.Label{
										Text:      "战斗宏启动快捷键：",
										TextColor: walk.RGB(200, 20, 20),
										Font:      decl.Font{Bold: true, PointSize: 9},
									},
									decl.PushButton{
										AssignTo:    &w.toggleKeyBtn,
										Text:        "X1",
										Font:        decl.Font{Bold: true},
										MinSize:     decl.Size{Width: 68, Height: ctrlHeight},
										MaxSize:     decl.Size{Width: 68, Height: ctrlHeight},
										OnClicked:   func() { w.startCapture(-1, 100) },
										OnKeyDown:   w.handleGlobalKeyDown,
										ToolTipText: "左键点击录入；右键快捷选鼠标侧键/中键/F2",
										ContextMenuItems: []decl.MenuItem{
											decl.Action{Text: "设为 鼠标侧键1 (X1)", OnTriggered: func() { w.setToggleKey("X1") }},
											decl.Action{Text: "设为 鼠标侧键2 (X2)", OnTriggered: func() { w.setToggleKey("X2") }},
											decl.Action{Text: "设为 鼠标中键 (MMB)", OnTriggered: func() { w.setToggleKey("MMB") }},
											decl.Separator{},
											decl.Action{Text: "设为 F2", OnTriggered: func() { w.setToggleKey("F2") }},
											decl.Action{Text: "设为 ` (波浪号)", OnTriggered: func() { w.setToggleKey("`") }},
										},
									},
									decl.HSpacer{},
								},
							},

							// 2. 宏状态指示行
							decl.Composite{
								Layout: decl.HBox{MarginsZero: true, Spacing: 6},
								Children: []decl.Widget{
									decl.Label{
										AssignTo: &w.statusLabel,
										Text:     "● 宏已就绪 (前台生效)",
										Font:     decl.Font{Bold: true},
									},
									decl.HSpacer{},
									decl.Label{Text: "启动方式："},
									decl.ComboBox{
										AssignTo:              &w.startModeCombo,
										Model:                 startModeNames,
										MinSize:               decl.Size{Width: 120, Height: ctrlHeight},
										MaxSize:               decl.Size{Width: 120, Height: ctrlHeight},
										OnCurrentIndexChanged: w.onStartModeChanged,
									},
								},
							},

							// 3. 辅助功能卡片 (仅在暗黑3下显示助手，非暗黑3下显示对应提示)
							decl.GroupBox{
								Title:  "辅助功能",
								Layout: decl.VBox{Margins: decl.Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 5},
								Children: []decl.Widget{
									// 暗黑3专属面板
									w.createHelperContent(),

									// 非暗黑3下的清爽提示
									decl.Label{
										AssignTo:  &w.nonD3HintLabel,
										Visible:   false,
										Text:      "当前游戏无需城镇辅助。\n宏只发送技能按键与连点，纯后台静默运行。",
										TextColor: walk.RGB(120, 120, 120),
									},
								},
							},

							// 4. 全局偏好与防护 (平铺无弹窗)
							decl.GroupBox{
								Title:  "操作偏好与防护",
								Layout: decl.VBox{Margins: decl.Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 5},
								Children: []decl.Widget{
									decl.Composite{
										Layout: decl.HBox{MarginsZero: true, Spacing: 8},
										Children: []decl.Widget{
											decl.CheckBox{
												AssignTo:         &w.smartPauseCheck,
												Text:             "智能暂停 (Tab/回车/T/M)",
												Checked:          w.app.settings.SmartPause,
												OnCheckedChanged: w.savePreferences,
												ToolTipText:      "Tab 暂停/恢复；回车(打字)、T(回城)、M(地图) 自动停止宏",
											},
											decl.CheckBox{
												AssignTo:         &w.soundCueCheck,
												Text:             "切换提示音",
												Checked:          w.app.settings.SoundCue,
												OnCheckedChanged: w.savePreferences,
												ToolTipText:      "按热键切换配置成功时播放提示音",
											},
										},
									},
									decl.Composite{
										Layout: decl.HBox{MarginsZero: true, Spacing: 6},
										Children: []decl.Widget{
											decl.CheckBox{
												AssignTo:         &w.forceStandCheck,
												Text:             "连点左键自动按住站立键：",
												OnCheckedChanged: w.onForceStandToggled,
											},
											decl.PushButton{
												AssignTo:    &w.forceStandKeyBtn,
												Text:        "Shift",
												Font:        decl.Font{Bold: true},
												MinSize:     decl.Size{Width: 54, Height: ctrlHeight},
												MaxSize:     decl.Size{Width: 54, Height: ctrlHeight},
												OnClicked:   func() { w.startCapture(-1, 102) },
												OnKeyDown:   w.handleGlobalKeyDown,
												ToolTipText: "强制站立键：默认 LShift",
												ContextMenuItems: []decl.MenuItem{
													decl.Action{Text: "恢复默认 (Shift)", OnTriggered: func() { w.setForceStandKey("") }},
													decl.Action{Text: "设为 LShift", OnTriggered: func() { w.setForceStandKey("LSHIFT") }},
													decl.Action{Text: "设为 LCtrl", OnTriggered: func() { w.setForceStandKey("LCTRL") }},
													decl.Action{Text: "设为 LAlt", OnTriggered: func() { w.setForceStandKey("LALT") }},
												},
											},
											decl.HSpacer{},
										},
									},
								},
							},
						},
					},
				},
			},

			// ================= 底部状态栏 =================
			decl.Composite{
				Layout: decl.HBox{Margins: decl.Margins{Left: 4, Top: 1, Right: 4, Bottom: 1}, Spacing: 12},
				Children: []decl.Widget{
					decl.Label{
						AssignTo:  &w.activeProfLbl,
						Text:      "当前激活配置：配置 1",
						TextColor: walk.RGB(200, 20, 20),
						Font:      decl.Font{Bold: true},
					},
					decl.Label{
						AssignTo:  &w.targetProcLbl,
						Text:      "目标进程：Diablo III64.exe",
						TextColor: walk.RGB(100, 100, 100),
					},
					decl.Label{
						AssignTo:  &w.autoSaveLbl,
						Text:      "✓ 实时生效",
						TextColor: walk.RGB(40, 140, 40),
					},
					decl.HSpacer{},
					decl.Label{
						Text:      "Win32 SendInput · 零注入零驱动",
						TextColor: walk.RGB(120, 120, 120),
					},
				},
			},
		},
	}.Create()
	if err != nil {
		return err
	}

	hwnd := w.mw.Handle()
	style := win.GetWindowLong(hwnd, win.GWL_STYLE)
	style &^= (win.WS_MAXIMIZEBOX | win.WS_THICKFRAME)
	win.SetWindowLong(hwnd, win.GWL_STYLE, style)
	win.SetWindowPos(hwnd, 0, int32(posX), int32(posY), int32(fixedWinWidth), int32(fixedWinHeight),
		win.SWP_NOZORDER|win.SWP_FRAMECHANGED|win.SWP_NOREDRAW)

	w.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		*canceled = true
		w.mw.Hide()
		if !w.hiddenHintShown {
			w.hiddenHintShown = true
			if a.ni != nil {
				if err := a.ni.ShowInfo(appName, "已最小化到托盘，宏会继续运行。\n双击托盘图标可重新打开。"); err != nil {
					a.log.Printf("[托盘] 气泡提示失败：%v", err)
				}
			}
		}
	})

	a.win = w
	w.loadFromState()
	w.mw.Show()
	a.log.Printf("[窗口] 主窗口已打开（对齐 D3keyHelper 左右双栏 %dx%d）", fixedWinWidth, fixedWinHeight)
	return nil
}

// createTabPage 构建单套技能配置页 (完全对齐 D3keyHelper 布局)
func (w *mainWindow) createTabPage(tabIdx int) decl.TabPage {
	t := tabIdx
	qp := w.app.settings.QuickPause
	return decl.TabPage{
		Title:           fmt.Sprintf("配置%d", t+1),
		DoubleBuffering: true,
		Layout:          decl.VBox{Margins: decl.Margins{Left: 6, Top: 6, Right: 6, Bottom: 6}, Spacing: 4},
		Children: []decl.Widget{
			// 1. 按键宏设置 GroupBox
			decl.GroupBox{
				Title:  "按键宏设置",
				Layout: decl.VBox{Margins: decl.Margins{Left: 6, Top: 6, Right: 6, Bottom: 6}, Spacing: 3},
				Children: []decl.Widget{
					// 表头行
					decl.Composite{
						Layout: decl.HBox{Spacing: colRowSpacing},
						Children: []decl.Widget{
							decl.HSpacer{Size: colSlotName},
							decl.Label{Text: "快捷键", MinSize: decl.Size{Width: colKey}, MaxSize: decl.Size{Width: colKey}, Alignment: decl.AlignHCenterVCenter},
							decl.Label{Text: "策略", MinSize: decl.Size{Width: colStrategy}, MaxSize: decl.Size{Width: colStrategy}, Alignment: decl.AlignHCenterVCenter},
							decl.Label{Text: "执行间隔 (毫秒)", MinSize: decl.Size{Width: colInterval}, MaxSize: decl.Size{Width: colInterval}, Alignment: decl.AlignHCenterVCenter},
							decl.Label{Text: "延迟 (毫秒)", MinSize: decl.Size{Width: colDelay}, MaxSize: decl.Size{Width: colDelay}, Alignment: decl.AlignHCenterVCenter},
							decl.Label{Text: "延迟随机", MinSize: decl.Size{Width: colRandom}, MaxSize: decl.Size{Width: colRandom}, Alignment: decl.AlignHCenterVCenter},
							decl.HSpacer{},
						},
					},
					w.createSlotRow(t, 0, "技能一:"),
					w.createSlotRow(t, 1, "技能二:"),
					w.createSlotRow(t, 2, "技能三:"),
					w.createSlotRow(t, 3, "技能四:"),
					w.createSlotRow(t, 4, "左键技能:"),
					w.createSlotRow(t, 5, "右键技能:"),
				},
			},

			// 2. 额外设置 GroupBox (方案级设置 + 快速暂停)
			decl.GroupBox{
				Title:  "额外设置",
				Layout: decl.VBox{Margins: decl.Margins{Left: 6, Top: 5, Right: 6, Bottom: 5}, Spacing: 4},
				Children: []decl.Widget{
					// 行 1: 方案名与一键秒切热键、按键模式
					decl.Composite{
						Layout: decl.HBox{MarginsZero: true, Spacing: 4},
						Children: []decl.Widget{
							decl.Label{Text: "快速切换至本配置："},
							decl.PushButton{
								AssignTo:    &w.switchKeyBtns[t],
								Text:        "无",
								Font:        decl.Font{Bold: true},
								MinSize:     decl.Size{Width: 50, Height: ctrlHeight},
								MaxSize:     decl.Size{Width: 50, Height: ctrlHeight},
								OnClicked:   func() { w.startCapture(t, 101) },
								OnKeyDown:   w.handleGlobalKeyDown,
								ToolTipText: "游戏中按此键秒切本套配置；右键清除",
								ContextMenuItems: []decl.MenuItem{
									decl.Action{Text: "清除切换键 (设为无)", OnTriggered: func() { w.clearSwitchKey(t) }},
								},
							},
							decl.Label{Text: "配置名："},
							decl.LineEdit{
								AssignTo:          &w.profileNameEdits[t],
								MinSize:           decl.Size{Width: 80, Height: ctrlHeight},
								MaxSize:           decl.Size{Width: 80, Height: ctrlHeight},
								OnEditingFinished: func() { w.onProfileNameEdited(t) },
							},
							decl.Label{Text: "按键模式："},
							decl.ComboBox{
								AssignTo:              &w.modeComs[t],
								Model:                 modeNames,
								MinSize:               decl.Size{Width: 88, Height: ctrlHeight},
								MaxSize:               decl.Size{Width: 88, Height: ctrlHeight},
								OnCurrentIndexChanged: func() { w.onProfileModeChanged(t) },
								ToolTipText:           "并发按键：各自独立计时到点就按\n循环按键：按槽位顺序轮流按",
							},
							decl.HSpacer{},
						},
					},

					// 行 2: 快速暂停设置
					decl.Composite{
						Layout: decl.HBox{MarginsZero: true, Spacing: 4},
						Children: []decl.Widget{
							decl.CheckBox{
								AssignTo:         &w.quickPauseCheck,
								Text:             "快速暂停：",
								Checked:          qp.Enabled,
								OnCheckedChanged: w.savePreferences,
							},
							decl.ComboBox{
								AssignTo:              &w.quickPauseTrigger,
								Model:                 quickPauseTriggerNames,
								CurrentIndex:          qp.Trigger - 1,
								MinSize:               decl.Size{Width: 54, Height: ctrlHeight},
								MaxSize:               decl.Size{Width: 54, Height: ctrlHeight},
								OnCurrentIndexChanged: w.savePreferences,
							},
							decl.ComboBox{
								AssignTo:              &w.quickPauseMouseKey,
								Model:                 quickPauseMouseNames,
								CurrentIndex:          qp.MouseKey - 1,
								MinSize:               decl.Size{Width: 62, Height: ctrlHeight},
								MaxSize:               decl.Size{Width: 62, Height: ctrlHeight},
								OnCurrentIndexChanged: w.savePreferences,
							},
							decl.Label{Text: "则"},
							decl.ComboBox{
								AssignTo:              &w.quickPauseAction,
								Model:                 quickPauseActionNames,
								CurrentIndex:          qp.Action - 1,
								MinSize:               decl.Size{Width: 105, Height: ctrlHeight},
								MaxSize:               decl.Size{Width: 105, Height: ctrlHeight},
								OnCurrentIndexChanged: w.savePreferences,
							},
							decl.LineEdit{
								AssignTo:          &w.quickPauseDelay,
								Text:              strconv.Itoa(qp.DelayMS),
								MinSize:           decl.Size{Width: 46, Height: ctrlHeight},
								MaxSize:           decl.Size{Width: 46, Height: ctrlHeight},
								OnEditingFinished: w.savePreferences,
							},
							decl.Label{Text: "毫秒"},
							decl.HSpacer{},
						},
					},
				},
			},
		},
	}
}

func (w *mainWindow) createSlotRow(t, s int, labelText string) decl.Widget {
	return decl.Composite{
		Layout: decl.HBox{Spacing: colRowSpacing},
		Children: []decl.Widget{
			decl.Label{Text: labelText, MinSize: decl.Size{Width: colSlotName}, MaxSize: decl.Size{Width: colSlotName}, Alignment: decl.AlignHFarVCenter},
			decl.PushButton{
				AssignTo:    &w.slotKeyBtns[t][s],
				Text:        "1",
				Font:        decl.Font{Bold: true},
				MinSize:     decl.Size{Width: colKey, Height: ctrlHeight},
				MaxSize:     decl.Size{Width: colKey, Height: ctrlHeight},
				OnClicked:   func() { w.startCapture(t, s) },
				OnKeyDown:   w.handleGlobalKeyDown,
				ToolTipText: "左键点击录入；右键快捷选鼠标按键",
				ContextMenuItems: []decl.MenuItem{
					decl.Action{Text: "设为 鼠标左键 (LMB)", OnTriggered: func() { w.setSlotKey(t, s, "LMB") }},
					decl.Action{Text: "设为 鼠标右键 (RMB)", OnTriggered: func() { w.setSlotKey(t, s, "RMB") }},
					decl.Action{Text: "设为 鼠标中键 (MMB)", OnTriggered: func() { w.setSlotKey(t, s, "MMB") }},
					decl.Action{Text: "设为 鼠标侧键1 (X1)", OnTriggered: func() { w.setSlotKey(t, s, "X1") }},
					decl.Action{Text: "设为 鼠标侧键2 (X2)", OnTriggered: func() { w.setSlotKey(t, s, "X2") }},
					decl.Separator{},
					decl.Action{Text: "恢复默认键", OnTriggered: func() { w.resetSlotKey(t, s) }},
				},
			},
			decl.ComboBox{
				AssignTo:              &w.slotStrategyComs[t][s],
				Model:                 strategyNames,
				MinSize:               decl.Size{Width: colStrategy, Height: ctrlHeight},
				MaxSize:               decl.Size{Width: colStrategy, Height: ctrlHeight},
				OnCurrentIndexChanged: func() { w.onSlotStrategyChanged(t, s) },
			},
			decl.LineEdit{
				AssignTo:          &w.slotIntervalEdits[t][s],
				MinSize:           decl.Size{Width: colInterval, Height: ctrlHeight},
				MaxSize:           decl.Size{Width: colInterval, Height: ctrlHeight},
				OnEditingFinished: func() { w.onSlotIntervalChanged(t, s) },
				ToolTipText:       "执行间隔 (毫秒)",
			},
			decl.LineEdit{
				AssignTo:          &w.slotDelayEdits[t][s],
				MinSize:           decl.Size{Width: colDelay, Height: ctrlHeight},
				MaxSize:           decl.Size{Width: colDelay, Height: ctrlHeight},
				OnEditingFinished: func() { w.onSlotTuningChanged(t, s) },
				ToolTipText:       "延迟 (毫秒)：正数延后，负数提前",
			},
			decl.CheckBox{
				AssignTo:         &w.slotRandomChecks[t][s],
				MinSize:          decl.Size{Width: colRandom, Height: ctrlHeight},
				MaxSize:          decl.Size{Width: colRandom, Height: ctrlHeight},
				OnCheckedChanged: func() { w.onSlotTuningChanged(t, s) },
				ToolTipText:      "延迟随机",
			},
			decl.HSpacer{},
		},
	}
}

// createHelperContent 构建右栏暗黑3专属「辅助功能」内容
func (w *mainWindow) createHelperContent() decl.Widget {
	hs := w.app.settings.Helpers
	allHelpers := helper.All()
	w.helperChecks = make([]helperEntry, len(allHelpers))
	checks := make([]decl.Widget, 0, len(allHelpers))
	for i, h := range allHelpers {
		w.helperChecks[i] = helperEntry{id: h.ID()}
		checks = append(checks, decl.CheckBox{
			AssignTo:         &w.helperChecks[i].check,
			Text:             h.Name(),
			Checked:          hs.IsEnabled(h.ID()),
			OnCheckedChanged: w.onHelperChanged,
			MinSize:          decl.Size{Height: ctrlHeight},
		})
	}

	targetIdx := 0
	for i, v := range reforgeTargetValues {
		if v == hs.ReforgeTarget {
			targetIdx = i
			break
		}
	}
	speedIdx := helperSpeedIndex(hs.DelayMS)

	return decl.Composite{
		AssignTo: &w.helperContainer,
		Layout:   decl.VBox{MarginsZero: true, Spacing: 4},
		Children: []decl.Widget{
			// 助手宏启动快捷键 (对齐 D3keyHelper 红字)
			decl.Composite{
				Layout: decl.HBox{MarginsZero: true, Spacing: 4},
				Children: []decl.Widget{
					decl.Label{
						Text:      "助手宏启动快捷键：",
						TextColor: walk.RGB(200, 20, 20),
						Font:      decl.Font{Bold: true},
					},
					decl.LineEdit{
						AssignTo:          &w.helperHotkey,
						Text:              hs.Hotkey,
						MinSize:           decl.Size{Width: 42, Height: ctrlHeight},
						MaxSize:           decl.Size{Width: 42, Height: ctrlHeight},
						OnEditingFinished: w.onHelperChanged,
						ToolTipText:       "游戏内按此键触发助手，运行中再按可打断",
					},
					decl.Label{Text: "速度："},
					decl.ComboBox{
						AssignTo:              &w.helperSpeed,
						Model:                 helperSpeedNames,
						CurrentIndex:          speedIdx,
						MinSize:               decl.Size{Width: 100, Height: ctrlHeight},
						MaxSize:               decl.Size{Width: 100, Height: ctrlHeight},
						OnCurrentIndexChanged: w.onHelperChanged,
					},
					decl.HSpacer{},
				},
			},
			decl.Composite{
				Layout: decl.HBox{MarginsZero: true, Spacing: 4},
				Children: []decl.Widget{
					decl.Label{Text: "魔盒重铸目标："},
					decl.ComboBox{
						AssignTo:              &w.reforgeTarget,
						Model:                 reforgeTargetNames,
						CurrentIndex:          targetIdx,
						MinSize:               decl.Size{Width: 95, Height: ctrlHeight},
						MaxSize:               decl.Size{Width: 95, Height: ctrlHeight},
						OnCurrentIndexChanged: w.onHelperChanged,
					},
					decl.CheckBox{
						AssignTo:         &w.helperDev,
						Text:             "开发者详细日志",
						Checked:          hs.DevMode,
						OnCheckedChanged: w.onHelperChanged,
					},
					decl.HSpacer{},
				},
			},
			// 助手开关清单
			decl.Composite{
				Layout:   decl.VBox{MarginsZero: true, Spacing: 2},
				Children: checks,
			},
			decl.Label{
				Text:      "* 城镇助手要求游戏窗口化运行，宏停止时按热键触发",
				TextColor: walk.RGB(150, 95, 20),
			},
		},
	}
}

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

func (w *mainWindow) switchGameDirect(gameID string) {
	if w.loading || gameID == w.app.settings.CurrentGame {
		return
	}
	w.flushAllTabs()
	_ = w.saveToState(true)
	w.app.switchGame(gameID)
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
				w.profileNames[i] = fmt.Sprintf("配置%d", i+1)
			}
			w.profileHKs[i] = p.HK
			w.profileModes[i] = p.Mode
		} else {
			w.profileSlots[i] = defaultSlots()
			w.profileNames[i] = fmt.Sprintf("配置%d", i+1)
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

	w.updateGameButtons()
	w.updateHelperVisibility()

	if err := w.mw.SetTitle(fmt.Sprintf("%s v%s — %s", appName, version, gameDisplayName(w.app.settings.CurrentGame))); err != nil {
		w.app.log.Printf("[窗口] 设置标题失败：%v", err)
	}

	w.setCombo(w.startModeCombo, startModeNames, w.startMode)
	for t := 0; t < maxProfiles; t++ {
		w.setCombo(w.modeComs[t], modeNames, modeToIndex(w.profileModes[t]))
	}
	w.loadForceStand()

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
			w.loadSlotTuning(t, s, slot)
		}
	}

	if w.tabWidget != nil && w.tabWidget.CurrentIndex() != w.profileIdx && w.tabWidget.CurrentIndex() < maxProfiles {
		_ = w.tabWidget.SetCurrentIndex(w.profileIdx)
	}

	w.refreshGlobalViews()
}

func (w *mainWindow) updateGameButtons() {
	cur := w.app.settings.CurrentGame
	if w.d3Btn != nil {
		if cur == "d3" {
			w.setButtonText(w.d3Btn, "● 暗黑破坏神3")
		} else {
			w.setButtonText(w.d3Btn, "暗黑破坏神3")
		}
	}
	if w.d4Btn != nil {
		if cur == "d4" {
			w.setButtonText(w.d4Btn, "● 暗黑破坏神4")
		} else {
			w.setButtonText(w.d4Btn, "暗黑破坏神4")
		}
	}
	if w.d2rBtn != nil {
		if cur == "d2r" {
			w.setButtonText(w.d2rBtn, "● 暗黑破坏神2：重制版")
		} else {
			w.setButtonText(w.d2rBtn, "暗黑破坏神2：重制版")
		}
	}
}

// updateHelperVisibility 严格隔离暗黑3专属助手：暗黑3显示，暗黑4与暗黑2隐藏
func (w *mainWindow) updateHelperVisibility() {
	isD3 := w.app.settings.CurrentGame == "d3"
	if w.helperContainer != nil {
		w.helperContainer.SetVisible(isD3)
	}
	if w.nonD3HintLabel != nil {
		w.nonD3HintLabel.SetVisible(!isD3)
	}
}

func (w *mainWindow) refreshGlobalViews() {
	snap := w.app.st.snapshot()
	toggleKey := w.raw.Toggle
	if toggleKey == "" {
		toggleKey = config.DefaultToggleKey
	}
	if snap.ToggleVK != 0 {
		toggleKey = keyName(snap.ToggleVK)
	}
	w.setButtonText(w.toggleKeyBtn, toggleKey)

	for t := 0; t < maxProfiles; t++ {
		title := w.profileNames[t]
		if title == "" {
			title = fmt.Sprintf("配置%d", t+1)
		}
		if w.tabWidget != nil && t < w.tabWidget.Pages().Len() {
			_ = w.tabWidget.Pages().At(t).SetTitle(title)
		}
	}

	w.refreshStatus()
}

func (w *mainWindow) refreshStatus() {
	if w.mw == nil {
		return
	}
	snap := w.app.st.snapshot()
	toggleName := config.DefaultToggleKey
	if snap.ToggleVK != 0 {
		toggleName = keyName(snap.ToggleVK)
	} else if w.raw.Toggle != "" {
		toggleName = keyNameOfToken(w.raw.Toggle)
	}

	statusText := ""
	statusColor := walk.RGB(22, 140, 50)
	if snap.StartMode == 1 {
		if snap.Engine != nil && snap.Engine.Enabled() {
			statusText = fmt.Sprintf("● 宏运行中 (按住 %s 生效)", toggleName)
			statusColor = walk.RGB(230, 90, 10)
		} else {
			statusText = fmt.Sprintf("● 按住生效模式 (按住 %s 触发)", toggleName)
			statusColor = walk.RGB(22, 140, 50)
		}
	} else {
		switch {
		case snap.Engine == nil:
			statusText = "宏就绪 (进入游戏自动生效)"
			statusColor = walk.RGB(120, 120, 120)
		case snap.Engine.Enabled():
			statusText = fmt.Sprintf("● 宏已就绪 (前台生效 · 按 %s 暂停)", toggleName)
			statusColor = walk.RGB(22, 140, 50)
		default:
			statusText = fmt.Sprintf("⏸ 宏已暂停 (在游戏内按 %s 恢复)", toggleName)
			statusColor = walk.RGB(210, 40, 40)
		}
	}

	if w.statusLabel != nil {
		w.setLabel(w.statusLabel, statusText)
		w.statusLabel.SetTextColor(statusColor)
	}

	profName := "配置1"
	if w.profileIdx >= 0 && w.profileIdx < maxProfiles && w.profileNames[w.profileIdx] != "" {
		profName = w.profileNames[w.profileIdx]
	}
	w.setLabel(w.activeProfLbl, fmt.Sprintf("当前激活配置：%s", profName))

	procName := w.app.targetProcessOf(w.app.settings.CurrentGame)
	if procName == "" {
		procName = "(未知)"
	}
	w.setLabel(w.targetProcLbl, fmt.Sprintf("目标进程：%s", procName))
}

func (w *mainWindow) onTabChanged() {
	if w.loading || w.tabWidget == nil {
		return
	}
	idx := w.tabWidget.CurrentIndex()
	if idx < 0 || idx >= maxProfiles {
		return
	}
	if idx != w.profileIdx {
		w.profileIdx = idx
		w.app.switchProfile(idx)
		w.refreshStatus()
	}
}

func (w *mainWindow) autoSave() {
	if w.loading {
		return
	}
	w.flushAllTabs()
	_ = w.saveToState(true)
	if w.autoSaveLbl != nil {
		w.setLabel(w.autoSaveLbl, "✓ 实时生效")
	}
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
			w.flushSlot(t, s)
		}
	}
	if w.startModeCombo != nil {
		w.startMode = w.startModeCombo.CurrentIndex()
	}
	for t := 0; t < maxProfiles; t++ {
		if w.modeComs[t] != nil {
			w.profileModes[t] = indexToMode(w.modeComs[t].CurrentIndex())
		}
	}
}

func (w *mainWindow) onSlotStrategyChanged(t, s int) {
	if w.loading {
		return
	}
	slot := &w.profileSlots[t][s]
	slot.Strategy = w.slotStrategyComs[t][s].CurrentIndex()

	switch slot.Strategy {
	case strategySpam:
		if slot.IntervalMS <= 0 {
			slot.IntervalMS = 300
		}
	case strategyBuff:
		slot.DelayMS = 0
		slot.RandomDelay = false
		if slot.IntervalMS <= 0 {
			slot.IntervalMS = 300
		}
	default:
		slot.DelayMS = 0
		slot.RandomDelay = false
	}
	w.loadSlotTuning(t, s, *slot)
	w.autoSave()
}

func (w *mainWindow) loadSlotTuning(t, s int, slot skillSlotData) {
	switch slot.Strategy {
	case strategySpam:
		w.setText(w.slotIntervalEdits[t][s], strconv.Itoa(slot.IntervalMS))
	case strategyBuff:
		if slot.IntervalMS <= 0 {
			slot.IntervalMS = 300
		}
		w.setText(w.slotIntervalEdits[t][s], strconv.Itoa(slot.IntervalMS))
	default:
		w.setText(w.slotIntervalEdits[t][s], "—")
	}

	if slot.DelayMS != 0 {
		w.setText(w.slotDelayEdits[t][s], strconv.Itoa(slot.DelayMS))
	} else {
		w.setText(w.slotDelayEdits[t][s], "")
	}
	w.setCheckedSilently(w.slotRandomChecks[t][s], slot.RandomDelay)
	w.applySlotTuningEnabled(t, s, slot.Strategy)
}

func (w *mainWindow) applySlotTuningEnabled(t, s, strategy int) {
	spam := strategy == strategySpam
	buff := strategy == strategyBuff
	if c := w.slotIntervalEdits[t][s]; c != nil {
		c.SetEnabled(spam || buff)
	}
	if c := w.slotDelayEdits[t][s]; c != nil {
		c.SetEnabled(spam)
	}
	if c := w.slotRandomChecks[t][s]; c != nil {
		c.SetEnabled(spam)
	}
}

func (w *mainWindow) onSlotTuningChanged(t, s int) {
	if w.loading {
		return
	}
	slot := &w.profileSlots[t][s]
	switch slot.Strategy {
	case strategySpam:
		slot.DelayMS = parseIntField(w.slotDelayEdits[t][s], -maxDelayUI, maxDelayUI, 0)
		if c := w.slotRandomChecks[t][s]; c != nil {
			if slot.DelayMS == 0 {
				slot.RandomDelay = false
				w.setCheckedSilently(c, false)
			} else {
				slot.RandomDelay = c.Checked()
			}
		}
	}
	w.autoSave()
}

func (w *mainWindow) flushSlot(t, s int) {
	slot := &w.profileSlots[t][s]
	if c := w.slotStrategyComs[t][s]; c != nil {
		slot.Strategy = c.CurrentIndex()
	}
	if (slot.Strategy == strategySpam || slot.Strategy == strategyBuff) && w.slotIntervalEdits[t][s] != nil {
		if n, err := strconv.Atoi(strings.TrimSpace(w.slotIntervalEdits[t][s].Text())); err == nil && n > 0 {
			slot.IntervalMS = n
		}
	}

	switch slot.Strategy {
	case strategySpam:
		slot.DelayMS = parseIntField(w.slotDelayEdits[t][s], -maxDelayUI, maxDelayUI, 0)
		slot.RandomDelay = false
		if c := w.slotRandomChecks[t][s]; c != nil && slot.DelayMS != 0 {
			slot.RandomDelay = c.Checked()
		}
	case strategyBuff:
		slot.DelayMS = 0
		slot.RandomDelay = false
	default:
		slot.DelayMS = 0
		slot.RandomDelay = false
	}
}

func (w *mainWindow) setCheckedSilently(cb *walk.CheckBox, on bool) {
	if cb == nil || cb.Checked() == on {
		return
	}
	prev := w.loading
	w.loading = true
	cb.SetChecked(on)
	w.loading = prev
}

func parseIntField(le *walk.LineEdit, min, max, fallback int) int {
	if le == nil {
		return fallback
	}
	text := strings.TrimSpace(le.Text())
	if text == "" {
		return fallback
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		return fallback
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
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
		w.autoSave()
	}
}

func modeToIndex(mode string) int {
	if mode == config.ModeSequential {
		return 1
	}
	return 0
}

func indexToMode(idx int) string {
	if idx == 1 {
		return config.ModeSequential
	}
	return config.ModeConcurrent
}

func (w *mainWindow) onProfileModeChanged(t int) {
	if w.loading || w.modeComs[t] == nil {
		return
	}
	w.profileModes[t] = indexToMode(w.modeComs[t].CurrentIndex())
	w.autoSave()
}

func (w *mainWindow) onProfileNameEdited(t int) {
	if w.loading {
		return
	}
	name := strings.TrimSpace(w.profileNameEdits[t].Text())
	if name == "" {
		name = fmt.Sprintf("配置%d", t+1)
	}
	w.profileNames[t] = name
	if w.tabWidget != nil && t < w.tabWidget.Pages().Len() {
		_ = w.tabWidget.Pages().At(t).SetTitle(name)
	}
	w.refreshStatus()
	w.autoSave()
}

func (w *mainWindow) onStartModeChanged() {
	if w.loading {
		return
	}
	w.startMode = w.startModeCombo.CurrentIndex()
	w.autoSave()
	w.refreshStatus()
}

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
			tk = config.DefaultToggleKey
		}
		w.setButtonText(btn, keyNameOfToken(tk))
	case s == 102:
		w.setButtonText(btn, keyNameOfToken(w.forceStandKeyName()))
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
	case slotIdx == 102:
		return w.forceStandKeyBtn
	default:
		return nil
	}
}

func (w *mainWindow) handleGlobalKeyDown(key walk.Key) {
	if w.capturingSlot < 0 {
		return
	}
	vk := uint16(key)
	if vk == 0x1B {
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
		w.autoSave()
	case s == 100:
		w.raw.Toggle = token
		w.setButtonText(w.toggleKeyBtn, keyNameOfToken(token))
		w.refreshStatus()
		w.autoSave()
	case s == 101 && t >= 0 && t < maxProfiles:
		w.profileHKs[t] = token
		w.setButtonText(w.switchKeyBtns[t], keyNameOfToken(token))
		w.autoSave()
	case s == 102:
		w.setForceStandKey(token)
	}
}

func (w *mainWindow) forceStandKeyName() string {
	gk := w.app.settings.GameKeyMap(w.app.settings.CurrentGame)
	if gk.ForceStandKey != "" {
		return gk.ForceStandKey
	}
	if adapter, ok := games.For(w.app.settings.CurrentGame); ok {
		return adapter.DefaultForceStandKey()
	}
	return ""
}

func (w *mainWindow) loadForceStand() {
	gk := w.app.settings.GameKeyMap(w.app.settings.CurrentGame)
	w.setCheckedSilently(w.forceStandCheck, gk.ForceStandEnabled)
	key := w.forceStandKeyName()
	if key == "" {
		key = "Shift"
	}
	w.setButtonText(w.forceStandKeyBtn, keyNameOfToken(key))
}

func (w *mainWindow) setForceStandKey(token string) {
	w.cancelCapture()
	gk := w.app.settings.GameKeyMap(w.app.settings.CurrentGame)
	gk.ForceStandKey = token
	w.applyForceStandSettings(gk)
}

func (w *mainWindow) onForceStandToggled() {
	if w.loading || w.forceStandCheck == nil {
		return
	}
	gk := w.app.settings.GameKeyMap(w.app.settings.CurrentGame)
	gk.ForceStandEnabled = w.forceStandCheck.Checked()
	w.applyForceStandSettings(gk)
}

func (w *mainWindow) applyForceStandSettings(gk store.GameKeys) {
	gameID := w.app.settings.CurrentGame
	w.app.settings.SetGameKeyMap(gameID, gk)
	if err := store.SaveSettings(w.app.dir, *w.app.settings); err != nil {
		w.fatal(err)
		return
	}
	w.loadForceStand()

	if snap := w.app.st.snapshot(); snap.Engine != nil {
		w.app.setupForceStand(snap.Engine)
	}
	state := "关闭"
	if gk.ForceStandEnabled {
		state = "开启"
	}
	w.app.log.Printf("[设置] %s 左键自动站立：%s（键 %s）", gameDisplayName(gameID), state, w.forceStandKeyName())
}

func (w *mainWindow) savePreferences() {
	if w.loading {
		return
	}
	qp := store.QuickPauseSettings{
		Enabled:  w.quickPauseCheck.Checked(),
		Trigger:  w.quickPauseTrigger.CurrentIndex() + 1,
		MouseKey: w.quickPauseMouseKey.CurrentIndex() + 1,
		Action:   w.quickPauseAction.CurrentIndex() + 1,
		DelayMS:  parseIntField(w.quickPauseDelay, store.QuickPauseMinDelayMS, store.QuickPauseMaxDelayMS, store.DefaultQuickPause().DelayMS),
	}
	smart := w.smartPauseCheck.Checked()
	sound := w.soundCueCheck.Checked()
	w.app.settings.QuickPause = qp
	w.app.settings.SmartPause = smart
	w.app.settings.SoundCue = sound

	if err := store.SaveSettings(w.app.dir, *w.app.settings); err != nil {
		w.fatal(err)
		return
	}
	w.app.updateQuickPause(qp)
	w.app.updateSmartPause(smart)
	if w.autoSaveLbl != nil {
		w.setLabel(w.autoSaveLbl, "✓ 实时生效")
	}
}

func helperSpeedIndex(ms int) int {
	best, bestDiff := 2, 1<<30
	for i, v := range helperSpeedMS {
		d := v - ms
		if d < 0 {
			d = -d
		}
		if d < bestDiff {
			best, bestDiff = i, d
		}
	}
	return best
}

func (w *mainWindow) onHelperChanged() {
	if w.loading {
		return
	}
	hs := w.app.settings.Helpers
	if w.helperHotkey != nil {
		hs.Hotkey = strings.TrimSpace(w.helperHotkey.Text())
	}
	if w.helperSpeed != nil {
		if idx := w.helperSpeed.CurrentIndex(); idx >= 0 && idx < len(helperSpeedMS) {
			hs.DelayMS = helperSpeedMS[idx]
		}
	}
	if w.reforgeTarget != nil {
		if idx := w.reforgeTarget.CurrentIndex(); idx >= 0 && idx < len(reforgeTargetValues) {
			hs.ReforgeTarget = reforgeTargetValues[idx]
		}
	}
	if w.helperDev != nil {
		hs.DevMode = w.helperDev.Checked()
	}
	hs.Enabled = map[string]bool{}
	for _, e := range w.helperChecks {
		if e.check != nil && e.check.Checked() {
			hs.Enabled[e.id] = true
		}
	}
	w.app.settings.Helpers = hs

	if err := store.SaveSettings(w.app.dir, *w.app.settings); err != nil {
		w.fatal(err)
		return
	}
	if w.autoSaveLbl != nil {
		w.setLabel(w.autoSaveLbl, "✓ 实时生效")
	}
}

func (w *mainWindow) setToggleKey(token string) {
	w.cancelCapture()
	w.raw.Toggle = token
	w.setButtonText(w.toggleKeyBtn, keyNameOfToken(token))
	w.refreshStatus()
	w.autoSave()
}

func (w *mainWindow) setSlotKey(t, s int, token string) {
	w.cancelCapture()
	w.profileSlots[t][s].Key = token
	w.setButtonText(w.slotKeyBtns[t][s], keyNameOfToken(token))
	w.autoSave()
}

func (w *mainWindow) resetSlotKey(t, s int) {
	w.cancelCapture()
	slot := &w.profileSlots[t][s]
	slot.Key = slot.DefaultKey
	w.setButtonText(w.slotKeyBtns[t][s], keyNameOfToken(slot.Key))
	w.autoSave()
}

func (w *mainWindow) clearSwitchKey(t int) {
	w.cancelCapture()
	w.profileHKs[t] = ""
	w.setButtonText(w.switchKeyBtns[t], "无")
	w.autoSave()
}

func (w *mainWindow) saveToState(silent bool) error {
	w.flushAllTabs()

	profs := make([]config.RawProfile, maxProfiles)
	for i := 0; i < maxProfiles; i++ {
		name := w.profileNames[i]
		if name == "" {
			name = fmt.Sprintf("配置%d", i+1)
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
	w.flushAllTabs()

	profs := make([]config.RawProfile, maxProfiles)
	for i := 0; i < maxProfiles; i++ {
		name := w.profileNames[i]
		if name == "" {
			name = fmt.Sprintf("配置%d", i+1)
		}
		profs[i] = slotsToProfile(w.profileSlots[i], name, w.profileHKs[i], w.profileModes[i])
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

func (w *mainWindow) fatal(err error) {
	if w != nil && w.app != nil && w.app.log != nil {
		w.app.log.Printf("[窗口] 界面操作失败：%v", err)
	}
	if w != nil && w.mw != nil {
		walk.MsgBox(w.mw, appName, "界面操作失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
	}
}

func (w *mainWindow) setText(le *walk.LineEdit, s string) {
	if le == nil || le.Text() == s {
		return
	}
	_ = le.SetText(s)
}

func (w *mainWindow) setLabel(l *walk.Label, s string) {
	if l == nil || l.Text() == s {
		return
	}
	_ = l.SetText(s)
}

func (w *mainWindow) setButtonText(b *walk.PushButton, s string) {
	if b == nil || b.Text() == s {
		return
	}
	_ = b.SetText(s)
}

func (w *mainWindow) setCombo(cb *walk.ComboBox, items []string, idx int) {
	if cb == nil {
		return
	}
	if !comboModelEqual(cb, items) {
		if err := cb.SetModel(items); err != nil {
			return
		}
	}
	if idx < 0 || idx >= len(items) {
		idx = 0
	}
	if len(items) > 0 && cb.CurrentIndex() != idx {
		_ = cb.SetCurrentIndex(idx)
	}
}

func comboModelEqual(cb *walk.ComboBox, items []string) bool {
	cur, ok := cb.Model().([]string)
	if !ok || len(cur) != len(items) {
		return false
	}
	for i := range items {
		if cur[i] != items[i] {
			return false
		}
	}
	return true
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

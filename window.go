//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/imsxx/diablo-lazy-macro/internal/config"
	"github.com/imsxx/diablo-lazy-macro/internal/games"
	"github.com/imsxx/diablo-lazy-macro/internal/keys"
	"github.com/imsxx/diablo-lazy-macro/internal/helper"
	"github.com/imsxx/diablo-lazy-macro/internal/store"

	"github.com/lxn/walk"
	decl "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

const (
	maxProfiles = 4
	numSlots    = 6

	// 窗口为三段式纵向布局：顶部全局控制条、中部方案配置（6 列槽位参数）、底部状态栏。
	// 宽度由槽位表的列宽与顶部控件行共同决定。
	fixedWinWidth  = 890
	fixedWinHeight = 560
)

// 槽位表列宽：集中定义，便于与 fixedWinWidth 联动调整。
const (
	colSlotName   = 62
	colKey        = 68
	colStrategy   = 88
	colInterval   = 62
	colDelay      = 56
	colRandom     = 44
	colRowSpacing = 6
)

// 延迟的界面取值范围：与协议校验保持一致，避免界面允许但保存时被拒。
const maxDelayUI = 30000

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

// 按键触发模式：下标 0 = 并发（默认），1 = 循环。
var modeNames = []string{
	"并发按键",
	"循环按键",
}

type skillSlotData struct {
	Name       string
	DefaultKey string
	Key        string
	Strategy   int // 0:禁用, 1:按住不放, 2:连点, 3:保持Buff
	IntervalMS int

	// 延迟调参，对应分享码协议 v2 的 d / rd
	DelayMS     int  // 延迟毫秒：正数延后、负数提前（仅连点）
	RandomDelay bool // 延迟随机（仅连点）
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
	gameCombo        *walk.ComboBox
	toggleKeyBtn     *walk.PushButton
	startModeCombo   *walk.ComboBox
	statusLabel      *walk.Label
	targetProcLbl    *walk.Label
	activeProfLbl    *walk.Label
	forceStandCheck  *walk.CheckBox
	forceStandKeyBtn *walk.PushButton

	// 左栏原生 TabWidget (4套独立方案页)
	tabWidget *walk.TabWidget

	// 4个配置 × 6个核心技能槽位的行控件
	slotKeyBtns       [maxProfiles][numSlots]*walk.PushButton
	slotStrategyComs  [maxProfiles][numSlots]*walk.ComboBox
	slotIntervalEdits [maxProfiles][numSlots]*walk.LineEdit
	slotDelayEdits    [maxProfiles][numSlots]*walk.LineEdit
	slotRandomChecks  [maxProfiles][numSlots]*walk.CheckBox

	// 4个配置对应的设置项
	profileNameEdits [maxProfiles]*walk.LineEdit
	switchKeyBtns    [maxProfiles]*walk.PushButton
	modeComs         [maxProfiles]*walk.ComboBox
	profileModes     [maxProfiles]string

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
		// 不启用 DoubleBuffering：walk 的实现是给窗口加 WS_EX_COMPOSITED 扩展样式，
		// 对以原生控件为主的窗口会造成整窗重绘跳变（打开与切换时肉眼可见的闪动）。
		Layout:          decl.VBox{Margins: decl.Margins{Left: 10, Top: 8, Right: 10, Bottom: 6}, Spacing: 6},
		OnKeyDown:       w.handleGlobalKeyDown,
		Children: []decl.Widget{
			// ================= 顶部：全局控制条 =================
			// 游戏切换、宏启停、状态与主要动作作用于「整个工具」而不是某个方案，
			// 因此横跨窗口顶部；方案相关的设置留在下方的主体区域。
			decl.GroupBox{
				Title:  "全局控制",
				Layout: decl.VBox{Margins: decl.Margins{Left: 10, Top: 6, Right: 10, Bottom: 6}, Spacing: 6},
				Children: []decl.Widget{
					decl.Composite{
						Layout: decl.HBox{MarginsZero: true, Spacing: 8},
						Children: []decl.Widget{
							decl.Label{Text: "当前游戏：", MinSize: decl.Size{Width: 66}},
							decl.ComboBox{
								AssignTo:              &w.gameCombo,
								MinSize:               decl.Size{Width: 150},
								MaxSize:               decl.Size{Width: 150},
								OnCurrentIndexChanged: w.onGameChanged,
							},
							decl.Label{
								AssignTo: &w.statusLabel,
								Text:     "🟢 宏已就绪（前台自动生效）",
								Font:     decl.Font{Bold: true},
							},
							decl.HSpacer{},
							decl.Label{Text: "★ 启动快捷键：", Font: decl.Font{Bold: true, PointSize: 9}},
							decl.PushButton{
								AssignTo:    &w.toggleKeyBtn,
								Text:        "X1",
								Font:        decl.Font{Bold: true, PointSize: 10},
								MinSize:     decl.Size{Width: 84, Height: 30},
								MaxSize:     decl.Size{Width: 84, Height: 30},
								OnClicked:   func() { w.startCapture(-1, 100) },
								OnKeyDown:   w.handleGlobalKeyDown,
								ToolTipText: "左键点击捕获按键；右键菜单可快速绑定鼠标侧键(X1/X2)或中键\n默认 X1（鼠标侧键）：三款暗黑游戏的默认键位都不占用鼠标额外按键",
								ContextMenuItems: []decl.MenuItem{
									decl.Action{Text: "🖱️ 设为 鼠标侧键1 (X1)", OnTriggered: func() { w.setToggleKey("X1") }},
									decl.Action{Text: "🖱️ 设为 鼠标侧键2 (X2)", OnTriggered: func() { w.setToggleKey("X2") }},
									decl.Action{Text: "🖱️ 设为 鼠标中键 (MMB)", OnTriggered: func() { w.setToggleKey("MMB") }},
									decl.Separator{},
									decl.Action{Text: "⌨️ 设为 F2", OnTriggered: func() { w.setToggleKey("F2") }},
									decl.Action{Text: "⌨️ 设为 ` (波浪号)", OnTriggered: func() { w.setToggleKey("`") }},
								},
							},
							decl.Label{Text: "启动方式："},
							decl.ComboBox{
								AssignTo:              &w.startModeCombo,
								Model:                 startModeNames,
								MinSize:               decl.Size{Width: 150},
								MaxSize:               decl.Size{Width: 150},
								OnCurrentIndexChanged: w.onStartModeChanged,
								ToolTipText:           "懒人模式：按一下开，再按一下关\n仅按下时：按住快捷键(如侧键)触发，松开立即停止",
							},
							decl.CheckBox{
								AssignTo:         &w.forceStandCheck,
								Text:             "左键自动站立",
								OnCheckedChanged: w.onForceStandToggled,
								ToolTipText:      "连点左键技能时自动按住强制站立键，避免角色走动（暗黑3 的左键既是移动又是主要技能）\n键位需与游戏内「按键绑定」保持一致，点右侧按钮可改",
							},
							decl.PushButton{
								AssignTo:    &w.forceStandKeyBtn,
								Text:        "LSHIFT",
								MinSize:     decl.Size{Width: 78, Height: 24},
								MaxSize:     decl.Size{Width: 78, Height: 24},
								OnClicked:   func() { w.startCapture(-1, 102) },
								OnKeyDown:   w.handleGlobalKeyDown,
								ToolTipText: "强制站立键：点此按钮后按任意键录入；默认 LSHIFT（暗黑3 的默认绑定）",
								ContextMenuItems: []decl.MenuItem{
									decl.Action{Text: "⌨️ 恢复默认 (LShift)", OnTriggered: func() { w.setForceStandKey("") }},
									decl.Separator{},
									decl.Action{Text: "⌨️ 设为 LShift", OnTriggered: func() { w.setForceStandKey("LSHIFT") }},
									decl.Action{Text: "⌨️ 设为 LCtrl", OnTriggered: func() { w.setForceStandKey("LCTRL") }},
									decl.Action{Text: "⌨️ 设为 LAlt", OnTriggered: func() { w.setForceStandKey("LALT") }},
								},
							},
						},
					},
					decl.Composite{
						Layout: decl.HBox{MarginsZero: true, Spacing: 8},
						Children: []decl.Widget{
							decl.Label{
								Text:      "⚠ 屏幕识别（保持Buff）需游戏窗口化运行",
								TextColor: walk.RGB(150, 96, 32),
								ToolTipText: "暗黑3 的「保持Buff」依赖屏幕取色。全屏独占模式下游戏不参与桌面合成，" +
									"取色会失败、守卫随之失效；请在游戏设置里改用「窗口化」或「无边框窗口化」。",
							},
							decl.HSpacer{},
							decl.PushButton{
								Text:      "💾 保存并应用配置",
								Font:      decl.Font{Bold: true, PointSize: 9},
								MinSize:   decl.Size{Height: 28},
								OnClicked: w.saveAndApply,
							},
							decl.PushButton{Text: "📋 导入分享码", MinSize: decl.Size{Height: 28}, OnClicked: func() { a.importFromClipboard() }},
							decl.PushButton{Text: "📤 复制分享码", MinSize: decl.Size{Height: 28}, OnClicked: w.exportCode},
							decl.PushButton{Text: "⚙ 更多设置", MinSize: decl.Size{Height: 28}, OnClicked: w.openExtraSettings},
							decl.PushButton{Text: "📄 运行日志", MinSize: decl.Size{Height: 28}, OnClicked: a.showLogWindow},
						},
					},
				},
			},

			// ================= 主体：4 个方案的技能配置 =================
			decl.Composite{
				Layout: decl.HBox{MarginsZero: true, Spacing: 8},
				Children: []decl.Widget{
					decl.Composite{
						Layout: decl.VBox{MarginsZero: true, Spacing: 4},
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

					// 原「右栏全局控制」已上移到窗口顶部，这里不再保留第二栏。
					decl.Composite{
						Layout:   decl.VBox{MarginsZero: true},
						MinSize:  decl.Size{Width: 0},
						MaxSize:  decl.Size{Width: 0},
						Children: []decl.Widget{





						},
					},
				},
			},

			// ================= 底部：状态栏 =================
			// 只读状态集中于此，与顶部的可操作控件分开：看状态不用去找控件，找控件也不用翻状态。
			decl.Composite{
				Layout: decl.HBox{Margins: decl.Margins{Left: 4, Top: 4, Right: 4, Bottom: 2}, Spacing: 14},
				Children: []decl.Widget{
					decl.Label{
						AssignTo: &w.activeProfLbl,
						Text:     "当前激活配置：配置 1",
						Font:     decl.Font{Bold: true, PointSize: 9},
					},
					decl.Label{
						AssignTo: &w.targetProcLbl,
						Text:     "🎮 目标进程：Diablo IV.exe",
					},
					decl.HSpacer{},
					decl.Label{Text: "Win32 SendInput · 零驱动零注入"},
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
		win.SWP_NOZORDER|win.SWP_FRAMECHANGED|win.SWP_NOREDRAW)

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
	a.log.Printf("[窗口] 主窗口已打开（固定尺寸 %dx%d，顶部全局 / 中部方案 / 底部状态）", fixedWinWidth, fixedWinHeight)
	return nil
}

func (w *mainWindow) createTabPage(tabIdx int) decl.TabPage {
	t := tabIdx
	return decl.TabPage{
		Title: fmt.Sprintf("配置 %d", t+1),
		// 局部双缓冲：只把该页内部的控件合成后呈现，避免切换标签时逐个控件重绘造成的闪动。
		DoubleBuffering: true,
		Layout:          decl.VBox{Margins: decl.Margins{Left: 6, Top: 6, Right: 6, Bottom: 6}, Spacing: 4},
		Children: []decl.Widget{
			// 按键宏设置 GroupBox (高密度紧凑网格，去除冗长文字，采用 ToolTip)
			decl.GroupBox{
				Title:  "按键宏设置",
				Layout: decl.VBox{Margins: decl.Margins{Left: 8, Top: 6, Right: 8, Bottom: 6}, Spacing: 4},
				Children: []decl.Widget{
					decl.Composite{
						Layout: decl.HBox{Spacing: colRowSpacing},
						Children: []decl.Widget{
							decl.Label{Text: "技能槽位", MinSize: decl.Size{Width: colSlotName}, MaxSize: decl.Size{Width: colSlotName}, Font: decl.Font{Bold: true}},
							decl.Label{Text: "快捷键", MinSize: decl.Size{Width: colKey}, MaxSize: decl.Size{Width: colKey}, Font: decl.Font{Bold: true}},
							decl.Label{Text: "释放策略", MinSize: decl.Size{Width: colStrategy}, MaxSize: decl.Size{Width: colStrategy}, Font: decl.Font{Bold: true}},
							decl.Label{Text: "间隔(ms)", MinSize: decl.Size{Width: colInterval}, MaxSize: decl.Size{Width: colInterval}, Font: decl.Font{Bold: true}, ToolTipText: "执行间隔（毫秒）"},
							decl.Label{Text: "延迟", MinSize: decl.Size{Width: colDelay}, MaxSize: decl.Size{Width: colDelay}, Font: decl.Font{Bold: true}, ToolTipText: "延迟（毫秒）：正数延后、负数提前，|值| ≤ 20 视为关闭；仅连点可用"},
							decl.Label{Text: "±随机", MinSize: decl.Size{Width: colRandom}, MaxSize: decl.Size{Width: colRandom}, Font: decl.Font{Bold: true}, ToolTipText: "勾选后每次延迟取 0～|延迟| 之间的随机值；仅连点可用"},
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
					decl.Label{Text: "按键模式：", MinSize: decl.Size{Width: 65}},
					decl.ComboBox{
						AssignTo:              &w.modeComs[t],
						Model:                 modeNames,
						MinSize:               decl.Size{Width: 115},
						MaxSize:               decl.Size{Width: 115},
						OnCurrentIndexChanged: func() { w.onProfileModeChanged(t) },
						ToolTipText:           "并发按键：各技能各自计时、到点就按，互不影响（默认）\n循环按键：按槽位顺序一轮一轮按，一轮走完从头再来（罗技/雷蛇式顺序宏）",
					},
					decl.HSpacer{},
				},
			},
		},
	}
}

func (w *mainWindow) createSlotRow(t, s int, labelText string) decl.Widget {
	return decl.Composite{
		Layout: decl.HBox{Spacing: colRowSpacing},
		Children: []decl.Widget{
			decl.Label{Text: labelText, MinSize: decl.Size{Width: colSlotName}, MaxSize: decl.Size{Width: colSlotName}},
			decl.PushButton{
				AssignTo:    &w.slotKeyBtns[t][s],
				Text:        "1",
				MinSize:     decl.Size{Width: colKey, Height: 24},
				MaxSize:     decl.Size{Width: colKey, Height: 24},
				OnClicked:   func() { w.startCapture(t, s) },
				OnKeyDown:   w.handleGlobalKeyDown,
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
				Model:                 strategyNames,
				MinSize:               decl.Size{Width: colStrategy},
				MaxSize:               decl.Size{Width: colStrategy},
				OnCurrentIndexChanged: func() { w.onSlotStrategyChanged(t, s) },
				ToolTipText:           "策略说明：\n· 禁用：不自动触发\n· 按住不放：旋风斩/扫射引导技能，战斗中持续按下\n· 连点：按指定间隔持续点击\n· 保持Buff：智能检测发光环(D3)或定时补放，未断绝不抢按",
			},
			decl.LineEdit{
				AssignTo:      &w.slotIntervalEdits[t][s],
				MinSize:       decl.Size{Width: colInterval},
				MaxSize:       decl.Size{Width: colInterval},
				OnTextChanged: func() { w.onSlotIntervalChanged(t, s) },
				ToolTipText:   "执行间隔（毫秒）：\n· 连点模式：技能连发的间隔\n· 保持Buff模式：屏幕找色采样的频率或定时续航周期",
			},
			decl.LineEdit{
				AssignTo:      &w.slotDelayEdits[t][s],
				MinSize:       decl.Size{Width: colDelay},
				MaxSize:       decl.Size{Width: colDelay},
				OnTextChanged: func() { w.onSlotTuningChanged(t, s) },
				ToolTipText:   "延迟（毫秒）：\n· 正数：本周期内延后该毫秒数再发送\n· 负数：推迟到周期末尾前该毫秒数发送\n· 绝对值 ≤ 20 或留空视为关闭",
			},
			decl.CheckBox{
				AssignTo:         &w.slotRandomChecks[t][s],
				MinSize:          decl.Size{Width: colRandom},
				MaxSize:          decl.Size{Width: colRandom},
				OnCheckedChanged: func() { w.onSlotTuningChanged(t, s) },
				ToolTipText:      "延迟随机：每次延迟取 0～|延迟| 之间的随机值",
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

	if err := w.mw.SetTitle(fmt.Sprintf("%s v%s — %s", appName, version, gameDisplayName(w.app.settings.CurrentGame))); err != nil {
		w.app.log.Printf("[窗口] 设置标题失败：%v", err)
	}

	w.setCombo(w.gameCombo, gameNames(), gameIndex(w.app.settings.CurrentGame))
	w.setCombo(w.startModeCombo, startModeNames, w.startMode)
	for t := 0; t < maxProfiles; t++ {
		w.setCombo(w.modeComs[t], modeNames, modeToIndex(w.profileModes[t]))
	}
	w.loadForceStand()

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
			w.loadSlotTuning(t, s, slot)
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
		toggleKey = config.DefaultToggleKey
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
	toggleName := config.DefaultToggleKey
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

	// 切换策略后清理与当前策略无关的参数，避免把无意义字段写进分享码。
	switch slot.Strategy {
	case strategySpam:
		if slot.IntervalMS <= 0 {
			slot.IntervalMS = 250
		}
	case strategyBuff:
		slot.DelayMS = 0
		slot.RandomDelay = false
		if slot.IntervalMS <= 0 {
			slot.IntervalMS = 300
		}
	default: // 禁用 / 按住不放
		slot.DelayMS = 0
		slot.RandomDelay = false
	}
	w.loadSlotTuning(t, s, *slot)
}

// loadSlotTuning 把单个槽位的策略与高级参数刷到对应控件，并按策略决定各列可用性。
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

// applySlotTuningEnabled 按策略决定各列是否可编辑：
// 连点 → 间隔/延迟/随机；保持Buff → 间隔；其余策略全部禁用。
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

// onSlotTuningChanged 处理延迟/随机/重复/优先级任一控件的变化，回写内存状态。
// 不回写控件，避免打断用户输入；显示规范化由 loadSlotTuning 负责。
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
				w.setCheckedSilently(c, false) // 没有延迟时，随机无意义
			} else {
				slot.RandomDelay = c.Checked()
			}
		}
	}
}

// flushSlot 把单个槽位行上的控件取值写回内存状态（保存前调用）。
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

// setCheckedSilently 修改勾选状态但不触发回写回调。
func (w *mainWindow) setCheckedSilently(cb *walk.CheckBox, on bool) {
	if cb == nil || cb.Checked() == on {
		return
	}
	prev := w.loading
	w.loading = true
	cb.SetChecked(on)
	w.loading = prev
}

// parseIntField 读取输入框整数：留空或非法返回 fallback，越界收敛到边界。
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
	}
}

// modeToIndex / indexToMode 在协议模式常量与界面下拉下标之间转换：下标 0 = 并发（默认），1 = 循环。
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
	case s == 102:
		w.setForceStandKey(token)
	}
}

// forceStandKeyName 返回当前游戏生效的强制站立键名：玩家设置优先，留空则用该游戏默认值。
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

// loadForceStand 把按键映射设置刷到界面控件。
func (w *mainWindow) loadForceStand() {
	gk := w.app.settings.GameKeyMap(w.app.settings.CurrentGame)
	w.setCheckedSilently(w.forceStandCheck, gk.ForceStandEnabled)
	key := w.forceStandKeyName()
	if key == "" {
		key = "—"
	}
	w.setButtonText(w.forceStandKeyBtn, keyNameOfToken(key))
}

// setForceStandKey 写入强制站立键（空串表示恢复该游戏默认值），并立即生效。
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

// applyForceStandSettings 持久化按键映射并让设置立即生效（无需重启宏）。
func (w *mainWindow) applyForceStandSettings(gk store.GameKeys) {
	gameID := w.app.settings.CurrentGame
	w.app.settings.SetGameKeyMap(gameID, gk)
	if err := store.SaveSettings(w.app.dir, *w.app.settings); err != nil {
		w.fatal(err)
		return
	}
	w.loadForceStand()

	// 设置变化立即作用到运行中的引擎，不必重新应用配置。
	if snap := w.app.st.snapshot(); snap.Engine != nil {
		w.app.setupForceStand(snap.Engine)
	}
	state := "关闭"
	if gk.ForceStandEnabled {
		state = "开启"
	}
	w.app.log.Printf("[设置] %s 左键自动站立：%s（键 %s）", gameDisplayName(gameID), state, w.forceStandKeyName())
}

// 快速暂停的界面下拉选项（下标 +1 即协议取值）。
var (
	quickPauseTriggerNames = []string{"双击", "单击", "压住"}
	quickPauseMouseNames   = []string{"左键", "右键", "中键", "侧键1", "侧键2"}
	quickPauseActionNames  = []string{"暂停按键宏", "暂停宏且连点左键"}
)

// helperEntry 记录一个助手开关的绑定关系。
type helperEntry struct {
	id    string
	check *walk.CheckBox
}

// 重铸目标品质的界面选项；下标与 reforgeTargetValues 一一对应。
var (
	reforgeTargetNames  = []string{"只重铸一次", "普通传奇", "远古传奇", "神圣装备", "太古传奇"}
	reforgeTargetValues = []int{0, 2, 3, 4, 5}
)

// settingsDlg 承载「更多设置」对话框的控件引用。
type settingsDlg struct {
	dlg *walk.Dialog

	quickPauseCheck    *walk.CheckBox
	quickPauseTrigger  *walk.ComboBox
	quickPauseMouseKey *walk.ComboBox
	quickPauseAction   *walk.ComboBox
	quickPauseDelay    *walk.LineEdit

	smartPauseCheck *walk.CheckBox
	soundCueCheck   *walk.CheckBox

	helpers       []helperEntry
	helperHotkey  *walk.LineEdit
	helperDelay   *walk.LineEdit
	helperDev     *walk.CheckBox
	reforgeTarget *walk.ComboBox
}

// openExtraSettings 打开「更多设置」对话框。
// 这里放的是操作习惯类设置：不属于分享码内容，改动后立即生效并落盘。
func (w *mainWindow) openExtraSettings() {
	d := &settingsDlg{}
	qp := w.app.settings.QuickPause

	dlg := decl.Dialog{
		AssignTo: &d.dlg,
		Title:    "更多设置",
		MinSize:  decl.Size{Width: 470, Height: 560},
		Layout:   decl.VBox{Margins: decl.Margins{Left: 12, Top: 10, Right: 12, Bottom: 10}, Spacing: 8},
		Children: []decl.Widget{
			decl.GroupBox{
				Title:  "快速暂停",
				Layout: decl.VBox{Margins: decl.Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}, Spacing: 8},
				Children: []decl.Widget{
					decl.CheckBox{
						AssignTo:    &d.quickPauseCheck,
						Text:        "启用快速暂停（按鼠标键临时停手，再按一下恢复）",
						Checked:     qp.Enabled,
						ToolTipText: "暂停期间不发送任何技能按键，并立即释放按住中的键",
					},
					decl.Composite{
						Layout: decl.HBox{MarginsZero: true, Spacing: 6},
						Children: []decl.Widget{
							decl.Label{Text: "鼠标"},
							decl.ComboBox{AssignTo: &d.quickPauseTrigger, Model: quickPauseTriggerNames, CurrentIndex: qp.Trigger - 1, MinSize: decl.Size{Width: 62}, MaxSize: decl.Size{Width: 62}},
							decl.ComboBox{AssignTo: &d.quickPauseMouseKey, Model: quickPauseMouseNames, CurrentIndex: qp.MouseKey - 1, MinSize: decl.Size{Width: 70}, MaxSize: decl.Size{Width: 70}},
							decl.Label{Text: "则"},
							decl.ComboBox{AssignTo: &d.quickPauseAction, Model: quickPauseActionNames, CurrentIndex: qp.Action - 1, MinSize: decl.Size{Width: 148}, MaxSize: decl.Size{Width: 148}},
							decl.HSpacer{},
						},
					},
					decl.Composite{
						Layout: decl.HBox{MarginsZero: true, Spacing: 6},
						Children: []decl.Widget{
							decl.Label{Text: "单击 / 双击模式的暂停时长："},
							decl.LineEdit{AssignTo: &d.quickPauseDelay, Text: strconv.Itoa(qp.DelayMS), MinSize: decl.Size{Width: 60}, MaxSize: decl.Size{Width: 60}},
							decl.Label{Text: "毫秒（500–5000）"},
							decl.HSpacer{},
						},
					},
				},
			},
			decl.GroupBox{
				Title:  "智能暂停",
				Layout: decl.VBox{Margins: decl.Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}, Spacing: 6},
				Children: []decl.Widget{
					decl.CheckBox{
						AssignTo:    &d.smartPauseCheck,
						Text:        "启用智能暂停（推荐）",
						Checked:     w.app.settings.SmartPause,
						ToolTipText: "按 Tab 打开地图时自动暂停宏，再按一次恢复；\n按 回车（聊天）、T（回城）、M（世界地图）时自动停止宏，避免对着界面误发技能",
					},
					decl.Label{
						Text:      "　Tab：暂停 / 恢复　　回车 · T · M：停止宏",
						TextColor: walk.RGB(110, 110, 110),
					},
				},
			},
			decl.GroupBox{
				Title:  "音效提示",
				Layout: decl.VBox{Margins: decl.Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}, Spacing: 6},
				Children: []decl.Widget{
					decl.CheckBox{
						AssignTo:    &d.soundCueCheck,
						Text:        "快捷键切换配置成功时播放提示音",
						Checked:     w.app.settings.SoundCue,
						ToolTipText: "在游戏里用切换键换方案时，不必切出画面就能靠声音确认是否切换成功",
					},
				},
			},
			decl.Composite{
				Layout: decl.HBox{MarginsZero: true, Spacing: 8},
				Children: []decl.Widget{
					decl.HSpacer{},
					decl.PushButton{Text: "确定", MinSize: decl.Size{Width: 80, Height: 28}, MaxSize: decl.Size{Width: 80, Height: 28}, OnClicked: func() { d.save(w) }},
					decl.PushButton{Text: "取消", MinSize: decl.Size{Width: 80, Height: 28}, MaxSize: decl.Size{Width: 80, Height: 28}, OnClicked: func() { d.dlg.Close(0) }},
				},
			},
		},
	}
	// 助手开关：按注册顺序动态生成，控件引用固定在切片里（切片一次分配，指针稳定）。
	allHelpers := helper.All()
	d.helpers = make([]helperEntry, len(allHelpers))
	helperChecks := make([]decl.Widget, 0, len(allHelpers))
	for i, h := range allHelpers {
		d.helpers[i] = helperEntry{id: h.ID()}
		helperChecks = append(helperChecks, decl.CheckBox{
			AssignTo: &d.helpers[i].check,
			Text:     h.Name(),
			Checked:  w.app.settings.Helpers.IsEnabled(h.ID()),
		})
	}
	targetIdx := 0
	for i, v := range reforgeTargetValues {
		if v == w.app.settings.Helpers.ReforgeTarget {
			targetIdx = i
			break
		}
	}

	extra := []decl.Widget{
		decl.GroupBox{
			Title:  "助手功能（暗黑3 专用）",
			Layout: decl.VBox{Margins: decl.Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}, Spacing: 6},
			Children: []decl.Widget{
				decl.Label{
					Text:      "⚠ 需游戏窗口化运行，且技能宏处于停止状态（助手与宏互斥）",
					TextColor: walk.RGB(150, 96, 32),
				},
				decl.Composite{
					Layout: decl.HBox{MarginsZero: true, Spacing: 6},
					Children: []decl.Widget{
						decl.Label{Text: "触发快捷键："},
						decl.LineEdit{
							AssignTo: &d.helperHotkey,
							Text:     w.app.settings.Helpers.Hotkey,
							MinSize:  decl.Size{Width: 70},
							MaxSize:  decl.Size{Width: 70},
						},
						decl.Label{Text: "动作间隔："},
						decl.LineEdit{
							AssignTo: &d.helperDelay,
							Text:     strconv.Itoa(w.app.settings.Helpers.DelayMS),
							MinSize:  decl.Size{Width: 55},
							MaxSize:  decl.Size{Width: 55},
						},
						decl.Label{Text: "毫秒（50–1000）"},
						decl.HSpacer{},
					},
				},
				decl.Composite{
					Layout:   decl.HBox{MarginsZero: true, Spacing: 12},
					Children: helperChecks,
				},
				decl.Composite{
					Layout: decl.HBox{MarginsZero: true, Spacing: 6},
					Children: []decl.Widget{
						decl.Label{Text: "重铸目标品质："},
						decl.ComboBox{
							AssignTo: &d.reforgeTarget, Model: reforgeTargetNames, CurrentIndex: targetIdx,
							MinSize: decl.Size{Width: 120}, MaxSize: decl.Size{Width: 120},
						},
						decl.CheckBox{
							AssignTo:    &d.helperDev,
							Text:        "开发者模式（详细日志）",
							Checked:     w.app.settings.Helpers.DevMode,
							ToolTipText: "记录每一步的取色值、判定结果与鼠标去向，便于复现问题时回传日志",
						},
						decl.HSpacer{},
					},
				},
			},
		},
	}
	dlg.Children = append(dlg.Children, extra...)

	if err := dlg.Create(w.mw); err != nil {
		w.fatal(err)
		return
	}
	d.dlg.Run()
}

// save 把对话框内容落盘并立即生效。
func (d *settingsDlg) save(w *mainWindow) {
	qp := store.QuickPauseSettings{
		Enabled:  d.quickPauseCheck.Checked(),
		Trigger:  d.quickPauseTrigger.CurrentIndex() + 1,
		MouseKey: d.quickPauseMouseKey.CurrentIndex() + 1,
		Action:   d.quickPauseAction.CurrentIndex() + 1,
		DelayMS:  parseIntField(d.quickPauseDelay, store.QuickPauseMinDelayMS, store.QuickPauseMaxDelayMS, store.DefaultQuickPause().DelayMS),
	}
	smart := d.smartPauseCheck.Checked()
	sound := d.soundCueCheck.Checked()
	w.app.settings.QuickPause = qp
	w.app.settings.SmartPause = smart
	w.app.settings.SoundCue = sound

	// 助手设置
	hs := w.app.settings.Helpers
	hs.Hotkey = strings.TrimSpace(d.helperHotkey.Text())
	hs.DelayMS = parseIntField(d.helperDelay, store.HelperMinDelayMS, store.HelperMaxDelayMS, store.DefaultHelperDelayMS)
	hs.DevMode = d.helperDev.Checked()
	if idx := d.reforgeTarget.CurrentIndex(); idx >= 0 && idx < len(reforgeTargetValues) {
		hs.ReforgeTarget = reforgeTargetValues[idx]
	}
	hs.Enabled = map[string]bool{}
	var enabledNames []string
	for _, e := range d.helpers {
		if e.check != nil && e.check.Checked() {
			hs.Enabled[e.id] = true
			enabledNames = append(enabledNames, e.id)
		}
	}
	w.app.settings.Helpers = hs

	if err := store.SaveSettings(w.app.dir, *w.app.settings); err != nil {
		w.fatal(err)
		return
	}
	w.app.updateQuickPause(qp)
	w.app.updateSmartPause(smart)
	w.app.updateHelpers(hs)
	w.app.log.Printf("[设置] 智能暂停：%s；音效提示：%s", onOffText(smart), onOffText(sound))
	w.app.log.Printf("[设置] 助手快捷键：%s；动作间隔：%dms；开发者模式：%s；已启用：%v",
		hs.Hotkey, hs.DelayMS, onOffText(hs.DevMode), enabledNames)
	state := "关闭"
	if qp.Enabled {
		state = "开启"
	}
	w.app.log.Printf("[设置] 快速暂停：%s（鼠标%s + %s → %s）", state,
		quickPauseTriggerNames[qp.Trigger-1], quickPauseMouseNames[qp.MouseKey-1], quickPauseActionNames[qp.Action-1])
	d.dlg.Close(0)
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

// 辅助方法
func (w *mainWindow) fatal(err error) {
	if w != nil && w.app != nil && w.app.log != nil {
		w.app.log.Printf("[窗口] 界面操作失败：%v", err)
	}
	if w != nil && w.mw != nil {
		walk.MsgBox(w.mw, appName, "界面操作失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
	}
}

// 以下 set* 辅助均为幂等：值未变化时跳过写入，避免触发无谓的控件重绘（界面闪动的直接来源之一）。

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

// setCombo 幂等设置下拉的模型与选中项：模型内容一致时不重建，避免整片下拉重绘。
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

// comboModelEqual 判断下拉当前模型是否与目标列表逐项一致。
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

func slotsToProfile(slots [numSlots]skillSlotData, name string, switchKey string, mode string) config.RawProfile {
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
		case strategySpam: // 连点：可叠加延迟（含随机）做错峰与拟人化
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

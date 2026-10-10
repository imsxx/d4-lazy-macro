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
)

const (
	maxProfiles = 4
	maxRules    = 8
)

var (
	triggerItems = []string{"宏开启期间持续", "按住指定键"}
	actionItems  = []string{"连点", "按键序列", "单点一次", "按住不放"}
	actionCodes  = []string{"tap", "seq", "once", "hold"}
)

// mainWindow 是程序主窗口：游戏与方案切换、宏开关、规则编辑、导入导出与日志入口。
// 关闭窗口只最小化到托盘，宏继续运行；所有控件仅在 UI 线程访问。
type mainWindow struct {
	app *app
	mw  *walk.MainWindow

	gameCombo    *walk.ComboBox
	profileCombo *walk.ComboBox
	statusLabel  *walk.Label
	toggleBtn    *walk.PushButton

	nameEdit        *walk.LineEdit
	authorEdit      *walk.LineEdit
	toggleEdit      *walk.LineEdit
	procEdit        *walk.LineEdit
	profileNameEdit *walk.LineEdit
	ruleList        *walk.ListBox
	triggerCombo    *walk.ComboBox
	holdKeyEdit     *walk.LineEdit
	actionCombo     *walk.ComboBox
	keysEdit        *walk.LineEdit
	intervalEdit    *walk.LineEdit
	hintLabel       *walk.Label

	raw        config.Raw
	profileIdx int
	ruleIdx    int
	loading    bool

	hiddenHintShown bool
}

func (a *app) createMainWindow() error {
	w := &mainWindow{app: a}
	err := decl.MainWindow{
		AssignTo: &w.mw,
		Title:    fmt.Sprintf("%s v%s", appName, version),
		MinSize:  decl.Size{Width: 1000, Height: 700},
		Layout:   decl.VBox{},
		Children: []decl.Widget{
			decl.Composite{
				Layout: decl.HBox{},
				Children: []decl.Widget{
					decl.Label{Text: "游戏："},
					decl.ComboBox{AssignTo: &w.gameCombo, MinSize: decl.Size{Width: 180}, OnCurrentIndexChanged: w.onGameChanged},
					decl.Label{AssignTo: &w.statusLabel, Text: "状态：未配置", MinSize: decl.Size{Width: 220}},
					decl.PushButton{AssignTo: &w.toggleBtn, Text: "启用宏", OnClicked: w.onToggleClicked},
					decl.HSpacer{},
					decl.Label{Text: "方案："},
					decl.ComboBox{AssignTo: &w.profileCombo, MinSize: decl.Size{Width: 180}, OnCurrentIndexChanged: w.onProfileChanged},
				},
			},
			decl.Composite{
				Layout: decl.HBox{},
				Children: []decl.Widget{
					decl.Label{Text: "配置名："},
					decl.LineEdit{AssignTo: &w.nameEdit, MinSize: decl.Size{Width: 210}},
					decl.Label{Text: "作者："},
					decl.LineEdit{AssignTo: &w.authorEdit, MinSize: decl.Size{Width: 140}},
					decl.Label{Text: "启停键："},
					decl.LineEdit{AssignTo: &w.toggleEdit, MinSize: decl.Size{Width: 70}},
					decl.Label{Text: "目标进程："},
					decl.LineEdit{AssignTo: &w.procEdit, MinSize: decl.Size{Width: 190}},
				},
			},
			decl.Composite{
				Layout: decl.HBox{},
				Children: []decl.Widget{
					decl.Label{Text: "方案名："},
					decl.LineEdit{AssignTo: &w.profileNameEdit, MinSize: decl.Size{Width: 190}},
					decl.PushButton{Text: "添加方案", OnClicked: w.addProfile},
					decl.PushButton{Text: "删除方案", OnClicked: w.deleteProfile},
					decl.Label{Text: "按键写法：0-9 / A-Z / F1-F12 / TAB / ESC / SPACE / LMB / RMB / MMB / X1 / X2 / 0xNN，序列用英文逗号分隔"},
				},
			},
			decl.Composite{
				Layout: decl.HBox{},
				Children: []decl.Widget{
					decl.ListBox{AssignTo: &w.ruleList, MinSize: decl.Size{Width: 480, Height: 320}, OnCurrentIndexChanged: w.onRuleChanged},
					decl.Composite{
						Layout: decl.VBox{},
						Children: []decl.Widget{
							decl.Composite{Layout: decl.HBox{}, Children: []decl.Widget{
								decl.Label{Text: "触发方式："},
								decl.ComboBox{AssignTo: &w.triggerCombo, MinSize: decl.Size{Width: 170}, OnCurrentIndexChanged: w.onTriggerChanged},
								decl.Label{Text: "按住键："},
								decl.LineEdit{AssignTo: &w.holdKeyEdit, MinSize: decl.Size{Width: 90}},
							}},
							decl.Composite{Layout: decl.HBox{}, Children: []decl.Widget{
								decl.Label{Text: "动作："},
								decl.ComboBox{AssignTo: &w.actionCombo, MinSize: decl.Size{Width: 170}, OnCurrentIndexChanged: w.onActionChanged},
							}},
							decl.Composite{Layout: decl.HBox{}, Children: []decl.Widget{
								decl.Label{Text: "按键："},
								decl.LineEdit{AssignTo: &w.keysEdit, MinSize: decl.Size{Width: 220}},
							}},
							decl.Composite{Layout: decl.HBox{}, Children: []decl.Widget{
								decl.Label{Text: "间隔(ms)："},
								decl.LineEdit{AssignTo: &w.intervalEdit, MinSize: decl.Size{Width: 110}},
							}},
							decl.Composite{Layout: decl.HBox{}, Children: []decl.Widget{
								decl.PushButton{Text: "添加规则", OnClicked: w.addRule},
								decl.PushButton{Text: "删除规则", OnClicked: w.deleteRule},
							}},
							decl.Label{AssignTo: &w.hintLabel, Text: ""},
							decl.VSpacer{},
						},
					},
				},
			},
			decl.Composite{
				Layout: decl.HBox{},
				Children: []decl.Widget{
					decl.PushButton{Text: "保存并应用", OnClicked: w.saveAndApply},
					decl.PushButton{Text: "从剪贴板导入分享码", OnClicked: func() { a.importFromClipboard() }},
					decl.PushButton{Text: "复制分享码", OnClicked: w.exportCode},
					decl.HSpacer{},
					decl.PushButton{Text: "查看日志", OnClicked: a.showLogWindow},
					decl.PushButton{Text: "打开数据目录", OnClicked: a.openDataDir},
					decl.PushButton{Text: "退出", OnClicked: a.quitApp},
				},
			},
		},
	}.Create()
	if err != nil {
		return err
	}

	w.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		*canceled = true
		w.mw.Hide()
		if !w.hiddenHintShown {
			w.hiddenHintShown = true
			if err := a.ni.ShowInfo(appName, "已最小化到托盘，宏会继续运行。\n双击托盘图标可以再次打开本窗口。"); err != nil {
				a.log.Printf("[托盘] 气泡提示失败：%v", err)
			}
		}
	})

	a.win = w
	w.loadFromState()
	w.mw.Show()
	a.log.Printf("[窗口] 主窗口已打开")
	return nil
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

func (w *mainWindow) fatal(err error) {
	w.app.log.Printf("[窗口] 界面操作失败：%v", err)
	walk.MsgBox(w.mw, appName, "界面操作失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
}

func (w *mainWindow) setText(le *walk.LineEdit, s string) {
	if err := le.SetText(s); err != nil {
		w.fatal(err)
	}
}

func (w *mainWindow) setLabel(l *walk.Label, s string) {
	if err := l.SetText(s); err != nil {
		w.fatal(err)
	}
}

func (w *mainWindow) setButtonText(b *walk.PushButton, s string) {
	if err := b.SetText(s); err != nil {
		w.fatal(err)
	}
}

func (w *mainWindow) setCombo(cb *walk.ComboBox, items []string, idx int) {
	if err := cb.SetModel(items); err != nil {
		w.fatal(err)
		return
	}
	if idx < 0 || idx >= len(items) {
		idx = 0
	}
	if len(items) == 0 {
		return
	}
	if err := cb.SetCurrentIndex(idx); err != nil {
		w.fatal(err)
	}
}

func (w *mainWindow) profile() *config.RawProfile {
	return &w.raw.Profiles[w.profileIdx]
}

// loadFromState 用运行期状态重建整个窗口内容（外部动作后调用）。
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
	if len(w.raw.Profiles) == 0 {
		w.raw.Profiles = []config.RawProfile{{Rules: []config.RawRule{{Trigger: "t", Action: "tap:1", IntervalMS: 300}}}}
	}
	w.profileIdx = 0
	if snap.Engine != nil {
		w.profileIdx = snap.Engine.ProfileIndex()
	}
	if w.profileIdx < 0 || w.profileIdx >= len(w.raw.Profiles) {
		w.profileIdx = 0
	}
	w.ruleIdx = 0

	if err := w.mw.SetTitle(fmt.Sprintf("%s v%s — %s", appName, version, gameDisplayName(w.app.settings.CurrentGame))); err != nil {
		w.fatal(err)
	}
	w.setCombo(w.gameCombo, gameNames(), gameIndex(w.app.settings.CurrentGame))
	w.setText(w.nameEdit, w.raw.Name)
	w.setText(w.authorEdit, w.raw.Author)
	w.setText(w.toggleEdit, w.raw.Toggle)
	w.setText(w.procEdit, w.app.targetProcessOf(w.app.settings.CurrentGame))
	w.setCombo(w.triggerCombo, triggerItems, 0)
	w.setCombo(w.actionCombo, actionItems, 0)
	w.refreshProfiles()
	w.refreshStatus()
}

// refreshStatus 只刷新与宏运行状态相关的控件（热键切换等高频路径）。
func (w *mainWindow) refreshStatus() {
	if w.mw == nil {
		return
	}
	snap := w.app.st.snapshot()
	switch {
	case snap.Engine == nil:
		w.setLabel(w.statusLabel, "状态：未配置")
		w.setButtonText(w.toggleBtn, "启用宏（先保存配置）")
	case snap.Engine.Enabled():
		w.setLabel(w.statusLabel, "状态：已开启（"+keys.Label(snap.ToggleVK)+" 停止）")
		w.setButtonText(w.toggleBtn, "停用宏")
	default:
		w.setLabel(w.statusLabel, "状态：已关闭（"+keys.Label(snap.ToggleVK)+" 启用）")
		w.setButtonText(w.toggleBtn, "启用宏")
	}
}

func (w *mainWindow) refreshProfiles() {
	names := make([]string, 0, len(w.raw.Profiles))
	for i, p := range w.raw.Profiles {
		n := p.Name
		if n == "" {
			n = fmt.Sprintf("方案 %d", i+1)
		}
		names = append(names, fmt.Sprintf("%d · %s", i+1, n))
	}
	if w.profileIdx < 0 || w.profileIdx >= len(w.raw.Profiles) {
		w.profileIdx = 0
	}
	if err := w.profileCombo.SetModel(names); err != nil {
		w.fatal(err)
		return
	}
	if err := w.profileCombo.SetCurrentIndex(w.profileIdx); err != nil {
		w.fatal(err)
		return
	}
	w.setText(w.profileNameEdit, w.profile().Name)
	w.refreshRules()
}

func (w *mainWindow) refreshRules() {
	rules := w.profile().Rules
	if len(rules) == 0 {
		w.ruleIdx = -1
		if err := w.ruleList.SetModel([]string{}); err != nil {
			w.fatal(err)
		}
		w.clearRuleFields()
		return
	}
	items := make([]string, 0, len(rules))
	for i, r := range rules {
		items = append(items, fmt.Sprintf("%d. %s", i+1, describeRawRule(r)))
	}
	if w.ruleIdx < 0 || w.ruleIdx >= len(rules) {
		w.ruleIdx = 0
	}
	if err := w.ruleList.SetModel(items); err != nil {
		w.fatal(err)
		return
	}
	if err := w.ruleList.SetCurrentIndex(w.ruleIdx); err != nil {
		w.fatal(err)
		return
	}
	w.loadRuleFields()
}

func (w *mainWindow) clearRuleFields() {
	w.loading = true
	defer func() { w.loading = false }()
	w.setCombo(w.triggerCombo, triggerItems, 0)
	w.setText(w.holdKeyEdit, "")
	w.setCombo(w.actionCombo, actionItems, 0)
	w.setText(w.keysEdit, "")
	w.setText(w.intervalEdit, "")
	w.setLabel(w.hintLabel, "（当前方案没有规则，点「添加规则」新建）")
}

func (w *mainWindow) loadRuleFields() {
	w.loading = true
	defer func() { w.loading = false }()
	rules := w.profile().Rules
	if len(rules) == 0 || w.ruleIdx < 0 || w.ruleIdx >= len(rules) {
		return
	}
	r := rules[w.ruleIdx]
	if strings.HasPrefix(r.Trigger, "h:") {
		w.setCombo(w.triggerCombo, triggerItems, 1)
		w.setText(w.holdKeyEdit, strings.TrimPrefix(r.Trigger, "h:"))
	} else {
		w.setCombo(w.triggerCombo, triggerItems, 0)
		w.setText(w.holdKeyEdit, "")
	}
	kind, arg, ok := strings.Cut(r.Action, ":")
	if !ok {
		kind, arg = actionCodes[0], ""
	}
	idx := 0
	for i, c := range actionCodes {
		if c == kind {
			idx = i
		}
	}
	w.setCombo(w.actionCombo, actionItems, idx)
	w.setText(w.keysEdit, arg)
	if r.IntervalMS > 0 {
		w.setText(w.intervalEdit, strconv.Itoa(r.IntervalMS))
	} else {
		w.setText(w.intervalEdit, "")
	}
	w.updateHint()
}

func (w *mainWindow) updateHint() {
	switch w.actionCombo.CurrentIndex() {
	case 1:
		w.setLabel(w.hintLabel, "序列：按键用英文逗号分隔（如 2,3），按间隔循环点击；间隔必填（20–600000）")
	case 2:
		w.setLabel(w.hintLabel, "单点：每次触发开始时点击一次；间隔留空")
	case 3:
		w.setLabel(w.hintLabel, "按住不放：触发期间保持按下，触发结束自动释放；间隔留空")
	default:
		w.setLabel(w.hintLabel, "连点：按间隔重复点击「按键」；间隔必填（20–600000）")
	}
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
	// 切换前先落盘当前编辑内容（仅当该游戏已有配置）；不合法则阻止切换
	if w.app.st.snapshot().Raw != nil {
		if err := w.saveToState(true); err != nil {
			walk.MsgBox(w.mw, appName,
				"当前配置不合法，未保存到「"+gameDisplayName(w.app.settings.CurrentGame)+"」：\n\n"+err.Error()+"\n\n请先修正，再切换游戏。",
				walk.MsgBoxOK|walk.MsgBoxIconError)
			w.loadFromState()
			return
		}
	}
	w.app.switchGame(id)
}

func (w *mainWindow) onToggleClicked() {
	w.app.toggleFromUI()
	w.refreshStatus()
}

func (w *mainWindow) onProfileChanged() {
	if w.loading {
		return
	}
	w.flushProfileName()
	w.flushRule()
	idx := w.profileCombo.CurrentIndex()
	if idx < 0 || idx >= len(w.raw.Profiles) {
		return
	}
	if idx == w.profileIdx {
		w.setText(w.profileNameEdit, w.profile().Name)
		return
	}
	w.profileIdx = idx
	w.ruleIdx = 0
	w.app.switchProfile(idx)
}

func (w *mainWindow) onRuleChanged() {
	if w.loading {
		return
	}
	w.flushRule()
	idx := w.ruleList.CurrentIndex()
	if idx < 0 {
		return
	}
	w.ruleIdx = idx
	w.loadRuleFields()
}

func (w *mainWindow) onTriggerChanged() {
	if !w.loading {
		w.updateHint()
	}
}

func (w *mainWindow) onActionChanged() {
	if !w.loading {
		w.updateHint()
	}
}

func (w *mainWindow) flushProfileName() {
	if w.profileIdx < 0 || w.profileIdx >= len(w.raw.Profiles) {
		return
	}
	w.raw.Profiles[w.profileIdx].Name = strings.TrimSpace(w.profileNameEdit.Text())
}

func (w *mainWindow) flushRule() {
	if w.profileIdx < 0 || w.profileIdx >= len(w.raw.Profiles) {
		return
	}
	rules := w.profile().Rules
	if w.ruleIdx < 0 || w.ruleIdx >= len(rules) {
		return
	}
	r := &rules[w.ruleIdx]
	if w.triggerCombo.CurrentIndex() == 1 {
		r.Trigger = "h:" + strings.TrimSpace(w.holdKeyEdit.Text())
	} else {
		r.Trigger = "t"
	}
	idx := w.actionCombo.CurrentIndex()
	if idx < 0 || idx >= len(actionCodes) {
		idx = 0
	}
	r.Action = actionCodes[idx] + ":" + strings.TrimSpace(w.keysEdit.Text())
	r.IntervalMS = parseInterval(w.intervalEdit.Text())
}

// flushAll 把界面上的全部编辑内容写回内存配置。
func (w *mainWindow) flushAll() {
	w.flushProfileName()
	w.flushRule()
	w.raw.Version = config.Version
	w.raw.Name = strings.TrimSpace(w.nameEdit.Text())
	w.raw.Author = strings.TrimSpace(w.authorEdit.Text())
	w.raw.Toggle = strings.TrimSpace(w.toggleEdit.Text())
}

func parseInterval(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

func (w *mainWindow) addRule() {
	w.flushRule()
	p := w.profile()
	if len(p.Rules) >= maxRules {
		walk.MsgBox(w.mw, appName, fmt.Sprintf("每个方案最多 %d 条规则", maxRules), walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	p.Rules = append(p.Rules, config.RawRule{Trigger: "t", Action: "tap:1", IntervalMS: 300})
	w.ruleIdx = len(p.Rules) - 1
	w.refreshRules()
}

func (w *mainWindow) deleteRule() {
	w.flushRule()
	p := w.profile()
	if len(p.Rules) <= 1 {
		walk.MsgBox(w.mw, appName, "每个方案至少要保留一条规则", walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	if w.ruleIdx < 0 || w.ruleIdx >= len(p.Rules) {
		return
	}
	p.Rules = append(p.Rules[:w.ruleIdx], p.Rules[w.ruleIdx+1:]...)
	if w.ruleIdx >= len(p.Rules) {
		w.ruleIdx = len(p.Rules) - 1
	}
	w.refreshRules()
}

func (w *mainWindow) addProfile() {
	w.flushProfileName()
	w.flushRule()
	if len(w.raw.Profiles) >= maxProfiles {
		walk.MsgBox(w.mw, appName, fmt.Sprintf("最多 %d 个方案", maxProfiles), walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	w.raw.Profiles = append(w.raw.Profiles, config.RawProfile{Rules: []config.RawRule{{Trigger: "t", Action: "tap:1", IntervalMS: 300}}})
	w.profileIdx = len(w.raw.Profiles) - 1
	w.ruleIdx = 0
	w.refreshProfiles()
}

func (w *mainWindow) deleteProfile() {
	w.flushProfileName()
	w.flushRule()
	if len(w.raw.Profiles) <= 1 {
		walk.MsgBox(w.mw, appName, "至少保留一个方案", walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	w.raw.Profiles = append(w.raw.Profiles[:w.profileIdx], w.raw.Profiles[w.profileIdx+1:]...)
	if w.profileIdx >= len(w.raw.Profiles) {
		w.profileIdx = len(w.raw.Profiles) - 1
	}
	w.ruleIdx = 0
	w.refreshProfiles()
}

// saveToState 校验并保存当前编辑内容；silent 为真时不弹成功后的注意提示。
func (w *mainWindow) saveToState(silent bool) error {
	w.flushAll()
	raw := w.raw
	parsed, err := config.Resolve(raw)
	if err != nil {
		return err
	}
	if err := w.app.setTargetProcess(w.app.settings.CurrentGame, strings.TrimSpace(w.procEdit.Text())); err != nil {
		return err
	}
	if err := w.app.applyConfig(&raw, parsed, w.profileIdx); err != nil {
		return err
	}
	if !silent {
		if warns := config.Warnings(parsed); len(warns) > 0 {
			var sb strings.Builder
			sb.WriteString("配置已保存，但有以下注意事项：\n\n")
			for _, msg := range warns {
				sb.WriteString("· " + msg + "\n")
			}
			walk.MsgBox(w.mw, appName, sb.String(), walk.MsgBoxOK|walk.MsgBoxIconWarning)
		}
	}
	return nil
}

func (w *mainWindow) saveAndApply() {
	if err := w.saveToState(false); err != nil {
		walk.MsgBox(w.mw, appName, "配置不合法，未保存：\n\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	w.app.log.Printf("[窗口] 已保存并应用：%s（%s）", w.raw.Name, gameDisplayName(w.app.settings.CurrentGame))
}

func (w *mainWindow) exportCode() {
	w.flushAll()
	code, err := config.Encode(w.raw)
	if err != nil {
		walk.MsgBox(w.mw, appName, "配置不合法，无法导出：\n\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if err := walk.Clipboard().SetText(code); err != nil {
		walk.MsgBox(w.mw, appName, "写入剪贴板失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	w.app.log.Printf("[窗口] 已导出分享码（%d 字符）", len(code))
	walk.MsgBox(w.mw, appName, "分享码已复制到剪贴板（"+strconv.Itoa(len(code))+" 字符）。", walk.MsgBoxOK|walk.MsgBoxIconInformation)
}

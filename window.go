//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/imsxx/diablo-lazy-macro/internal/config"
	"github.com/imsxx/diablo-lazy-macro/internal/games"

	"github.com/lxn/walk"
	decl "github.com/lxn/walk/declarative"
)

const (
	maxProfiles  = 4
	maxRules     = 8
	maxSeqKeys   = 4 // 界面提供 4 个序列下拉；协议上限 6，取更严的一侧
	maxNameRunes = 24
)

var (
	triggerItems = []string{"宏开启期间持续", "按住指定键"}
	actionItems  = []string{"连点", "按键序列", "单点一次", "按住不放"}
	actionCodes  = []string{"tap", "seq", "once", "hold"}
	actionSeqIdx = 1 // actionItems / actionCodes 中「按键序列」的下标
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

	nameEdit    *walk.LineEdit
	authorEdit  *walk.LineEdit
	procEdit    *walk.LineEdit
	toggleCombo *walk.ComboBox
	toggleItems []keyChoice

	profileNameEdit *walk.LineEdit
	switchCombo     *walk.ComboBox
	switchItems     []keyChoice
	profileInfo     *walk.Label

	ruleList     *walk.ListBox
	triggerCombo *walk.ComboBox
	holdKeyCombo *walk.ComboBox
	holdKeyItems []keyChoice
	actionCombo  *walk.ComboBox
	keyLabels    [maxSeqKeys]*walk.Label
	keyCombos    [maxSeqKeys]*walk.ComboBox
	keyItems     [maxSeqKeys][]keyChoice
	keysRow34    *walk.Composite
	intervalEdit *walk.LineEdit
	hintLabel    *walk.Label

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
		Icon:     a.icon,
		Size:     decl.Size{Width: 940, Height: 640}, // 显式给初始尺寸：不设时 walk 会撑到整个桌面
		MinSize:  decl.Size{Width: 900, Height: 600},
		Layout:   decl.VBox{},
		Children: []decl.Widget{
			// 第一行：游戏、运行状态、总开关、方案切换
			decl.Composite{
				Layout: decl.HBox{},
				Children: []decl.Widget{
					decl.Label{Text: "游戏："},
					decl.ComboBox{AssignTo: &w.gameCombo, MinSize: decl.Size{Width: 170}, OnCurrentIndexChanged: w.onGameChanged},
					decl.Label{AssignTo: &w.statusLabel, Text: "状态：未配置", MinSize: decl.Size{Width: 230}},
					decl.PushButton{AssignTo: &w.toggleBtn, Text: "启用宏", OnClicked: w.onToggleClicked},
					decl.HSpacer{},
					decl.Label{Text: "方案："},
					decl.ComboBox{AssignTo: &w.profileCombo, MinSize: decl.Size{Width: 190}, OnCurrentIndexChanged: w.onProfileChanged},
				},
			},
			// 第二行：配置名、作者、启停键、目标进程
			decl.Composite{
				Layout: decl.HBox{},
				Children: []decl.Widget{
					decl.Label{Text: "配置名："},
					decl.LineEdit{AssignTo: &w.nameEdit, MinSize: decl.Size{Width: 170}},
					decl.Label{Text: "作者："},
					decl.LineEdit{AssignTo: &w.authorEdit, MinSize: decl.Size{Width: 110}},
					decl.Label{Text: "启停键："},
					decl.ComboBox{AssignTo: &w.toggleCombo, MinSize: decl.Size{Width: 110}},
					decl.Label{Text: "目标进程："},
					decl.LineEdit{AssignTo: &w.procEdit, MinSize: decl.Size{Width: 170}},
				},
			},
			// 第三行：方案名、方案切换键、方案管理
			decl.Composite{
				Layout: decl.HBox{},
				Children: []decl.Widget{
					decl.Label{Text: "方案名："},
					decl.LineEdit{AssignTo: &w.profileNameEdit, MinSize: decl.Size{Width: 170}},
					decl.Label{Text: "切换键："},
					decl.ComboBox{AssignTo: &w.switchCombo, MinSize: decl.Size{Width: 110}},
					decl.PushButton{Text: "新建方案", OnClicked: w.addProfile},
					decl.PushButton{Text: "复制方案", OnClicked: w.copyProfile},
					decl.PushButton{Text: "删除方案", OnClicked: w.deleteProfile},
					decl.Label{AssignTo: &w.profileInfo, Text: ""},
				},
			},
			// 第四行：规则列表 + 规则编辑
			decl.Composite{
				Layout: decl.HBox{},
				Children: []decl.Widget{
					decl.ListBox{AssignTo: &w.ruleList, MinSize: decl.Size{Width: 430, Height: 300}, OnCurrentIndexChanged: w.onRuleChanged},
					decl.Composite{
						Layout: decl.VBox{},
						Children: []decl.Widget{
							decl.Composite{Layout: decl.HBox{}, Children: []decl.Widget{
								decl.Label{Text: "触发方式："},
								decl.ComboBox{AssignTo: &w.triggerCombo, MinSize: decl.Size{Width: 150}, OnCurrentIndexChanged: w.onTriggerChanged},
								decl.Label{Text: "按住键："},
								decl.ComboBox{AssignTo: &w.holdKeyCombo, MinSize: decl.Size{Width: 120}, OnCurrentIndexChanged: w.onTriggerChanged},
							}},
							decl.Composite{Layout: decl.HBox{}, Children: []decl.Widget{
								decl.Label{Text: "动作："},
								decl.ComboBox{AssignTo: &w.actionCombo, MinSize: decl.Size{Width: 150}, OnCurrentIndexChanged: w.onActionChanged},
								decl.Label{Text: "间隔(ms)："},
								decl.LineEdit{AssignTo: &w.intervalEdit, MinSize: decl.Size{Width: 100}},
							}},
							decl.Composite{Layout: decl.HBox{}, Children: []decl.Widget{
								decl.Label{AssignTo: &w.keyLabels[0], Text: "按键："},
								decl.ComboBox{AssignTo: &w.keyCombos[0], MinSize: decl.Size{Width: 110}},
								decl.Label{AssignTo: &w.keyLabels[1], Text: "序列2："},
								decl.ComboBox{AssignTo: &w.keyCombos[1], MinSize: decl.Size{Width: 110}},
							}},
							decl.Composite{AssignTo: &w.keysRow34, Layout: decl.HBox{}, Children: []decl.Widget{
								decl.Label{AssignTo: &w.keyLabels[2], Text: "序列3："},
								decl.ComboBox{AssignTo: &w.keyCombos[2], MinSize: decl.Size{Width: 110}},
								decl.Label{AssignTo: &w.keyLabels[3], Text: "序列4："},
								decl.ComboBox{AssignTo: &w.keyCombos[3], MinSize: decl.Size{Width: 110}},
							}},
							decl.Composite{Layout: decl.HBox{}, Children: []decl.Widget{
								decl.PushButton{Text: "添加规则", OnClicked: w.addRule},
								decl.PushButton{Text: "删除规则", OnClicked: w.deleteRule},
								decl.HSpacer{},
							}},
							decl.Label{AssignTo: &w.hintLabel, Text: ""},
							decl.VSpacer{},
						},
					},
				},
			},
			// 第五行：保存与分享、日志与退出
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

func defaultProfile() config.RawProfile {
	return config.RawProfile{Rules: []config.RawRule{{Trigger: "t", Action: "tap:1", IntervalMS: 300}}}
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
		w.raw.Profiles = []config.RawProfile{defaultProfile()}
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
	w.setText(w.procEdit, w.app.targetProcessOf(w.app.settings.CurrentGame))
	toggle := strings.TrimSpace(w.raw.Toggle)
	if toggle == "" {
		toggle = "F1"
	}
	w.toggleItems = w.setChoiceCombo(w.toggleCombo, keyboardKeyChoices(), toggle)
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
		w.setLabel(w.statusLabel, "状态：已开启（"+keyName(snap.ToggleVK)+" 停止）")
		w.setButtonText(w.toggleBtn, "停用宏")
	default:
		w.setLabel(w.statusLabel, "状态：已关闭（"+keyName(snap.ToggleVK)+" 启用）")
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
	w.setCombo(w.profileCombo, names, w.profileIdx)
	w.setLabel(w.profileInfo, fmt.Sprintf("共 %d/%d 个方案", len(w.raw.Profiles), maxProfiles))
	w.setText(w.profileNameEdit, w.profile().Name)
	w.switchItems = w.setChoiceCombo(w.switchCombo, withNone("（无）", keyboardKeyChoices()), w.profile().HK)
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
	w.holdKeyItems = w.setChoiceCombo(w.holdKeyCombo, skillKeyChoices(), "")
	w.setCombo(w.actionCombo, actionItems, 0)
	for i := 0; i < maxSeqKeys; i++ {
		base := skillKeyChoices()
		if i > 0 {
			base = withNone("（不使用）", base)
		}
		w.keyItems[i] = w.setChoiceCombo(w.keyCombos[i], base, "")
	}
	w.setText(w.intervalEdit, "")
	w.updateRuleFieldsState()
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
		w.holdKeyItems = w.setChoiceCombo(w.holdKeyCombo, skillKeyChoices(), strings.TrimPrefix(r.Trigger, "h:"))
	} else {
		w.setCombo(w.triggerCombo, triggerItems, 0)
		w.holdKeyItems = w.setChoiceCombo(w.holdKeyCombo, skillKeyChoices(), "")
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

	var tokens [maxSeqKeys]string
	if kind == actionCodes[actionSeqIdx] {
		parts := strings.Split(arg, ",")
		for i := 0; i < maxSeqKeys && i < len(parts); i++ {
			tokens[i] = strings.TrimSpace(parts[i])
		}
	} else {
		tokens[0] = strings.TrimSpace(arg)
	}
	for i := 0; i < maxSeqKeys; i++ {
		base := skillKeyChoices()
		if i > 0 {
			base = withNone("（不使用）", base)
		}
		w.keyItems[i] = w.setChoiceCombo(w.keyCombos[i], base, tokens[i])
	}
	if r.IntervalMS > 0 {
		w.setText(w.intervalEdit, strconv.Itoa(r.IntervalMS))
	} else {
		w.setText(w.intervalEdit, "")
	}
	w.updateRuleFieldsState()
}

// setRuleFieldsEnabled 在「当前方案没有规则」时把规则字段整体置灰。
func (w *mainWindow) setRuleFieldsEnabled(on bool) {
	w.triggerCombo.SetEnabled(on)
	w.holdKeyCombo.SetEnabled(on)
	w.actionCombo.SetEnabled(on)
	w.intervalEdit.SetEnabled(on)
	for i := 0; i < maxSeqKeys; i++ {
		w.keyCombos[i].SetEnabled(on)
		w.keyLabels[i].SetEnabled(on)
	}
}

// updateRuleFieldsState 按当前动作与触发方式联动字段的可见性、可用性与提示文案。
func (w *mainWindow) updateRuleFieldsState() {
	rules := w.profile().Rules
	hasRule := w.ruleIdx >= 0 && w.ruleIdx < len(rules)
	w.setRuleFieldsEnabled(hasRule)
	if !hasRule {
		w.setLabel(w.hintLabel, "（当前方案没有规则，点「添加规则」新建）")
		w.keysRow34.SetVisible(false)
		w.keyLabels[1].SetVisible(false)
		w.keyCombos[1].SetVisible(false)
		return
	}

	seq := w.actionCombo.CurrentIndex() == actionSeqIdx
	kind := w.actionCombo.CurrentIndex()

	w.setLabel(w.keyLabels[0], "按键：")
	if seq {
		w.setLabel(w.keyLabels[0], "序列1：")
	}
	w.keyLabels[1].SetVisible(seq)
	w.keyCombos[1].SetVisible(seq)
	w.keysRow34.SetVisible(seq)

	w.holdKeyCombo.SetEnabled(w.triggerCombo.CurrentIndex() == 1)
	w.intervalEdit.SetEnabled(seq || kind == 0)

	switch kind {
	case actionSeqIdx:
		w.setLabel(w.hintLabel, "序列：按间隔依次点击选中的键（最多 4 个，留空表示该位不参与）；间隔必填 20–600000")
	case 2:
		w.setLabel(w.hintLabel, "单点：每次触发开始时点击一次；间隔不必填")
	case 3:
		w.setLabel(w.hintLabel, "按住不放：触发期间保持按下，触发结束自动释放；间隔不必填")
	default:
		w.setLabel(w.hintLabel, "连点：按间隔重复点击；间隔必填 20–600000")
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
	idx := w.profileCombo.CurrentIndex()
	if idx < 0 || idx >= len(w.raw.Profiles) {
		return
	}
	if idx == w.profileIdx {
		w.setText(w.profileNameEdit, w.profile().Name)
		return
	}
	// 切换方案前先落盘（含方案名、切换键与规则），不合法则留在原方案
	w.flushProfile()
	w.flushRule()
	if err := w.saveToState(true); err != nil {
		walk.MsgBox(w.mw, appName, "当前方案不合法，未保存也无法切换：\n\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		w.loadFromState()
		return
	}
	w.profileIdx = idx
	w.ruleIdx = 0
	w.app.switchProfile(idx)
	w.loadFromState()
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
		w.updateRuleFieldsState()
	}
}

func (w *mainWindow) onActionChanged() {
	if !w.loading {
		w.updateRuleFieldsState()
	}
}

func (w *mainWindow) flushProfile() {
	if w.profileIdx < 0 || w.profileIdx >= len(w.raw.Profiles) {
		return
	}
	w.raw.Profiles[w.profileIdx].Name = strings.TrimSpace(w.profileNameEdit.Text())
	w.raw.Profiles[w.profileIdx].HK = comboTokenOf(w.switchCombo, w.switchItems)
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
		r.Trigger = "h:" + comboTokenOf(w.holdKeyCombo, w.holdKeyItems)
	} else {
		r.Trigger = "t"
	}
	idx := w.actionCombo.CurrentIndex()
	if idx < 0 || idx >= len(actionCodes) {
		idx = 0
	}
	kind := actionCodes[idx]
	arg := comboTokenOf(w.keyCombos[0], w.keyItems[0])
	if kind == actionCodes[actionSeqIdx] {
		parts := make([]string, 0, maxSeqKeys)
		for i := 0; i < maxSeqKeys; i++ {
			if tk := comboTokenOf(w.keyCombos[i], w.keyItems[i]); tk != "" {
				parts = append(parts, tk)
			}
		}
		arg = strings.Join(parts, ",")
	}
	r.Action = kind + ":" + arg
	r.IntervalMS = parseInterval(w.intervalEdit.Text())
}

// flushAll 把界面上的全部编辑内容写回内存配置。
func (w *mainWindow) flushAll() {
	w.flushProfile()
	w.flushRule()
	w.raw.Version = config.Version
	w.raw.Name = strings.TrimSpace(w.nameEdit.Text())
	w.raw.Author = strings.TrimSpace(w.authorEdit.Text())
	w.raw.Toggle = comboTokenOf(w.toggleCombo, w.toggleItems)
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
	w.flushProfile()
	w.flushRule()
	if len(w.raw.Profiles) >= maxProfiles {
		walk.MsgBox(w.mw, appName, fmt.Sprintf("最多 %d 个方案", maxProfiles), walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	p := defaultProfile()
	p.Name = fmt.Sprintf("方案 %d", len(w.raw.Profiles)+1)
	w.raw.Profiles = append(w.raw.Profiles, p)
	w.profileIdx = len(w.raw.Profiles) - 1
	w.ruleIdx = 0
	w.refreshProfiles()
}

// copyProfile 复制当前方案：规则深拷贝；切换键不随复制走，避免同键冲突。
func (w *mainWindow) copyProfile() {
	w.flushProfile()
	w.flushRule()
	if len(w.raw.Profiles) >= maxProfiles {
		walk.MsgBox(w.mw, appName, fmt.Sprintf("最多 %d 个方案", maxProfiles), walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	src := w.profile()
	dst := config.RawProfile{Name: profileCopyName(src.Name), Rules: append([]config.RawRule{}, src.Rules...)}
	w.raw.Profiles = append(w.raw.Profiles, dst)
	w.profileIdx = len(w.raw.Profiles) - 1
	w.ruleIdx = 0
	w.refreshProfiles()
	walk.MsgBox(w.mw, appName, "已复制为「"+dst.Name+"」。\n切换键不随复制走，需要的话请在新方案里单独设置。",
		walk.MsgBoxOK|walk.MsgBoxIconInformation)
}

func (w *mainWindow) deleteProfile() {
	w.flushProfile()
	w.flushRule()
	if len(w.raw.Profiles) <= 1 {
		walk.MsgBox(w.mw, appName, "至少保留一个方案", walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	name := w.profile().Name
	if name == "" {
		name = fmt.Sprintf("方案 %d", w.profileIdx+1)
	}
	if walk.MsgBox(w.mw, appName, "删除「"+name+"」？\n（点「保存并应用」后才写入磁盘）",
		walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}
	w.raw.Profiles = append(w.raw.Profiles[:w.profileIdx], w.raw.Profiles[w.profileIdx+1:]...)
	if w.profileIdx >= len(w.raw.Profiles) {
		w.profileIdx = len(w.raw.Profiles) - 1
	}
	w.ruleIdx = 0
	w.refreshProfiles()
}

// profileCopyName 生成「名字-副本」形式的方案名，并按协议上限（24 字符）截断。
func profileCopyName(src string) string {
	base := strings.TrimSpace(src)
	if base == "" {
		base = "方案"
	}
	name := []rune(base + "-副本")
	if len(name) > maxNameRunes {
		name = name[:maxNameRunes]
	}
	return string(name)
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

//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/imsxx/diablo-lazy-macro/internal/config"

	"github.com/lxn/walk"
	decl "github.com/lxn/walk/declarative"
)

const (
	editorMaxProfiles = 4
	editorMaxRules    = 8
)

var (
	triggerItems = []string{"宏开启期间持续", "按住指定键"}
	actionItems  = []string{"连点", "按键序列", "单点一次", "按住不放"}
	actionCodes  = []string{"tap", "seq", "once", "hold"}
)

// editorState 是编辑器窗口的界面状态；所有字段仅在 UI 线程访问。
type editorState struct {
	app        *app
	dlg        *walk.Dialog
	raw        config.Raw
	profileIdx int
	ruleIdx    int
	loading    bool // 刷新控件时抑制事件回调

	nameEdit     *walk.LineEdit
	authorEdit   *walk.LineEdit
	toggleEdit   *walk.LineEdit
	procEdit     *walk.LineEdit
	profileCombo *walk.ComboBox
	profileName  *walk.LineEdit
	ruleList     *walk.ListBox
	triggerCombo *walk.ComboBox
	holdKeyEdit  *walk.LineEdit
	actionCombo  *walk.ComboBox
	keysEdit     *walk.LineEdit
	intervalEdit *walk.LineEdit
	hintLabel    *walk.Label
}

func (a *app) showEditor(raw config.Raw, profileIdx int, targetProcess string) error {
	if a.editorDlg != nil {
		walk.MsgBox(a.mw, appName, "编辑器窗口已经打开了。", walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return nil
	}
	e := &editorState{app: a, raw: raw, profileIdx: profileIdx}

	err := decl.Dialog{
		AssignTo: &e.dlg,
		Title:    fmt.Sprintf("%s v%s · 编辑配置（%s）", appName, version, gameDisplayName(a.settings.CurrentGame)),
		MinSize:  decl.Size{Width: 940, Height: 620},
		Layout:   decl.VBox{},
		Children: []decl.Widget{
			decl.Composite{
				Layout: decl.HBox{},
				Children: []decl.Widget{
					decl.Label{Text: "配置名："},
					decl.LineEdit{AssignTo: &e.nameEdit, MinSize: decl.Size{Width: 220}},
					decl.Label{Text: "作者："},
					decl.LineEdit{AssignTo: &e.authorEdit, MinSize: decl.Size{Width: 150}},
					decl.Label{Text: "启停键："},
					decl.LineEdit{AssignTo: &e.toggleEdit, MinSize: decl.Size{Width: 80}},
					decl.Label{Text: "目标进程："},
					decl.LineEdit{AssignTo: &e.procEdit, MinSize: decl.Size{Width: 200}},
				},
			},
			decl.Composite{
				Layout: decl.HBox{},
				Children: []decl.Widget{
					decl.Label{Text: "方案："},
					decl.ComboBox{AssignTo: &e.profileCombo, MinSize: decl.Size{Width: 200}, OnCurrentIndexChanged: e.onProfileChanged},
					decl.Label{Text: "方案名："},
					decl.LineEdit{AssignTo: &e.profileName, MinSize: decl.Size{Width: 180}},
					decl.PushButton{Text: "添加方案", OnClicked: e.addProfile},
					decl.PushButton{Text: "删除方案", OnClicked: e.deleteProfile},
				},
			},
			decl.Composite{
				Layout: decl.HBox{},
				Children: []decl.Widget{
					decl.ListBox{AssignTo: &e.ruleList, MinSize: decl.Size{Width: 430, Height: 330}, OnCurrentIndexChanged: e.onRuleChanged},
					decl.Composite{
						Layout: decl.VBox{},
						Children: []decl.Widget{
							decl.Composite{Layout: decl.HBox{}, Children: []decl.Widget{
								decl.Label{Text: "触发方式："},
								decl.ComboBox{AssignTo: &e.triggerCombo, MinSize: decl.Size{Width: 170}, OnCurrentIndexChanged: e.onTriggerChanged},
								decl.Label{Text: "按住键："},
								decl.LineEdit{AssignTo: &e.holdKeyEdit, MinSize: decl.Size{Width: 90}},
							}},
							decl.Composite{Layout: decl.HBox{}, Children: []decl.Widget{
								decl.Label{Text: "动作："},
								decl.ComboBox{AssignTo: &e.actionCombo, MinSize: decl.Size{Width: 170}, OnCurrentIndexChanged: e.onActionChanged},
							}},
							decl.Composite{Layout: decl.HBox{}, Children: []decl.Widget{
								decl.Label{Text: "按键："},
								decl.LineEdit{AssignTo: &e.keysEdit, MinSize: decl.Size{Width: 220}},
							}},
							decl.Composite{Layout: decl.HBox{}, Children: []decl.Widget{
								decl.Label{Text: "间隔(ms)："},
								decl.LineEdit{AssignTo: &e.intervalEdit, MinSize: decl.Size{Width: 110}},
							}},
							decl.Composite{Layout: decl.HBox{}, Children: []decl.Widget{
								decl.PushButton{Text: "添加规则", OnClicked: e.addRule},
								decl.PushButton{Text: "删除规则", OnClicked: e.deleteRule},
							}},
							decl.Label{AssignTo: &e.hintLabel, Text: ""},
							decl.VSpacer{},
						},
					},
				},
			},
			decl.Label{Text: "按键写法：0-9 / A-Z / F1-F12 / TAB / ESC / SPACE / LMB / RMB / MMB / X1 / X2 / 0xNN（虚拟键码十六进制）；序列用英文逗号分隔"},
			decl.Composite{
				Layout: decl.HBox{},
				Children: []decl.Widget{
					decl.PushButton{Text: "导出分享码", OnClicked: e.exportCode},
					decl.PushButton{Text: "保存并应用", OnClicked: e.saveAndApply},
					decl.HSpacer{},
					decl.PushButton{Text: "取消", OnClicked: func() { e.dlg.Cancel() }},
				},
			},
		},
	}.Create(a.mw)
	if err != nil {
		return err
	}
	a.editorDlg = e.dlg
	e.dlg.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		*canceled = false
		a.editorDlg = nil
	})

	e.loading = true
	e.setText(e.nameEdit, e.raw.Name)
	e.setText(e.authorEdit, e.raw.Author)
	e.setText(e.toggleEdit, e.raw.Toggle)
	e.setText(e.procEdit, targetProcess)
	e.setCombo(e.triggerCombo, triggerItems, 0)
	e.setCombo(e.actionCombo, actionItems, 0)
	if len(e.raw.Profiles) == 0 {
		e.raw.Profiles = []config.RawProfile{{}}
	}
	if e.profileIdx < 0 || e.profileIdx >= len(e.raw.Profiles) {
		e.profileIdx = 0
	}
	e.refreshProfiles()
	e.loading = false
	e.dlg.Show()
	a.log.Printf("[编辑器] 打开（%s，%d 个方案）", gameDisplayName(a.settings.CurrentGame), len(e.raw.Profiles))
	return nil
}

func (e *editorState) fatal(err error) {
	e.app.log.Printf("[编辑器] 界面操作失败：%v", err)
	walk.MsgBox(e.dlg, appName, "界面操作失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
}

func (e *editorState) setText(le *walk.LineEdit, s string) {
	if err := le.SetText(s); err != nil {
		e.fatal(err)
	}
}

func (e *editorState) setLabel(l *walk.Label, s string) {
	if err := l.SetText(s); err != nil {
		e.fatal(err)
	}
}

func (e *editorState) setCombo(cb *walk.ComboBox, items []string, idx int) {
	if err := cb.SetModel(items); err != nil {
		e.fatal(err)
		return
	}
	if idx < 0 || idx >= len(items) {
		idx = 0
	}
	if err := cb.SetCurrentIndex(idx); err != nil {
		e.fatal(err)
	}
}

func (e *editorState) profile() *config.RawProfile {
	return &e.raw.Profiles[e.profileIdx]
}

func (e *editorState) refreshProfiles() {
	names := make([]string, 0, len(e.raw.Profiles))
	for i, p := range e.raw.Profiles {
		n := p.Name
		if n == "" {
			n = fmt.Sprintf("方案 %d", i+1)
		}
		names = append(names, fmt.Sprintf("%d · %s", i+1, n))
	}
	if e.profileIdx < 0 || e.profileIdx >= len(e.raw.Profiles) {
		e.profileIdx = 0
	}
	if err := e.profileCombo.SetModel(names); err != nil {
		e.fatal(err)
		return
	}
	if err := e.profileCombo.SetCurrentIndex(e.profileIdx); err != nil {
		e.fatal(err)
		return
	}
	e.setText(e.profileName, e.profile().Name)
	e.refreshRules()
}

func (e *editorState) refreshRules() {
	rules := e.profile().Rules
	if len(rules) == 0 {
		e.ruleIdx = -1
		if err := e.ruleList.SetModel([]string{}); err != nil {
			e.fatal(err)
		}
		e.clearRuleFields()
		return
	}
	items := make([]string, 0, len(rules))
	for i, r := range rules {
		items = append(items, fmt.Sprintf("%d. %s", i+1, describeRawRule(r)))
	}
	if e.ruleIdx < 0 || e.ruleIdx >= len(rules) {
		e.ruleIdx = 0
	}
	if err := e.ruleList.SetModel(items); err != nil {
		e.fatal(err)
		return
	}
	if err := e.ruleList.SetCurrentIndex(e.ruleIdx); err != nil {
		e.fatal(err)
		return
	}
	e.loadRuleFields()
}

func (e *editorState) clearRuleFields() {
	e.loading = true
	defer func() { e.loading = false }()
	e.setCombo(e.triggerCombo, triggerItems, 0)
	e.setText(e.holdKeyEdit, "")
	e.setCombo(e.actionCombo, actionItems, 0)
	e.setText(e.keysEdit, "")
	e.setText(e.intervalEdit, "")
	e.setLabel(e.hintLabel, "（当前方案没有规则，点「添加规则」新建）")
}

func (e *editorState) loadRuleFields() {
	e.loading = true
	defer func() { e.loading = false }()
	rules := e.profile().Rules
	if len(rules) == 0 || e.ruleIdx < 0 || e.ruleIdx >= len(rules) {
		return
	}
	r := rules[e.ruleIdx]
	if strings.HasPrefix(r.Trigger, "h:") {
		e.setCombo(e.triggerCombo, triggerItems, 1)
		e.setText(e.holdKeyEdit, strings.TrimPrefix(r.Trigger, "h:"))
	} else {
		e.setCombo(e.triggerCombo, triggerItems, 0)
		e.setText(e.holdKeyEdit, "")
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
	e.setCombo(e.actionCombo, actionItems, idx)
	e.setText(e.keysEdit, arg)
	if r.IntervalMS > 0 {
		e.setText(e.intervalEdit, strconv.Itoa(r.IntervalMS))
	} else {
		e.setText(e.intervalEdit, "")
	}
	e.updateHint()
}

func (e *editorState) updateHint() {
	switch e.actionCombo.CurrentIndex() {
	case 1:
		e.setLabel(e.hintLabel, "序列：按键用英文逗号分隔（如 2,3），按间隔循环点击；间隔必填（20–600000）")
	case 2:
		e.setLabel(e.hintLabel, "单点：每次触发开始时点击一次；间隔留空")
	case 3:
		e.setLabel(e.hintLabel, "按住不放：触发期间保持按下，触发结束自动释放；间隔留空")
	default:
		e.setLabel(e.hintLabel, "连点：按间隔重复点击「按键」；间隔必填（20–600000）")
	}
}

func (e *editorState) onProfileChanged() {
	if e.loading {
		return
	}
	e.flushProfileName()
	e.flushRule()
	idx := e.profileCombo.CurrentIndex()
	if idx < 0 || idx >= len(e.raw.Profiles) {
		return
	}
	e.profileIdx = idx
	e.ruleIdx = 0
	e.setText(e.profileName, e.profile().Name)
	e.refreshRules()
}

func (e *editorState) onRuleChanged() {
	if e.loading {
		return
	}
	e.flushRule()
	idx := e.ruleList.CurrentIndex()
	if idx < 0 {
		return
	}
	e.ruleIdx = idx
	e.loadRuleFields()
}

func (e *editorState) onTriggerChanged() {
	if !e.loading {
		e.updateHint()
	}
}

func (e *editorState) onActionChanged() {
	if !e.loading {
		e.updateHint()
	}
}

func (e *editorState) flushProfileName() {
	if e.profileIdx < 0 || e.profileIdx >= len(e.raw.Profiles) {
		return
	}
	e.raw.Profiles[e.profileIdx].Name = strings.TrimSpace(e.profileName.Text())
}

func (e *editorState) flushRule() {
	if e.profileIdx < 0 || e.profileIdx >= len(e.raw.Profiles) {
		return
	}
	rules := e.profile().Rules
	if e.ruleIdx < 0 || e.ruleIdx >= len(rules) {
		return
	}
	r := &rules[e.ruleIdx]
	if e.triggerCombo.CurrentIndex() == 1 {
		r.Trigger = "h:" + strings.TrimSpace(e.holdKeyEdit.Text())
	} else {
		r.Trigger = "t"
	}
	idx := e.actionCombo.CurrentIndex()
	if idx < 0 || idx >= len(actionCodes) {
		idx = 0
	}
	r.Action = actionCodes[idx] + ":" + strings.TrimSpace(e.keysEdit.Text())
	r.IntervalMS = parseInterval(e.intervalEdit.Text())
}

func parseInterval(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

func (e *editorState) addRule() {
	e.flushRule()
	p := e.profile()
	if len(p.Rules) >= editorMaxRules {
		walk.MsgBox(e.dlg, appName, fmt.Sprintf("每个方案最多 %d 条规则", editorMaxRules), walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	p.Rules = append(p.Rules, config.RawRule{Trigger: "t", Action: "tap:1", IntervalMS: 300})
	e.ruleIdx = len(p.Rules) - 1
	e.refreshRules()
}

func (e *editorState) deleteRule() {
	e.flushRule()
	p := e.profile()
	if len(p.Rules) <= 1 {
		walk.MsgBox(e.dlg, appName, "每个方案至少要保留一条规则", walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	if e.ruleIdx < 0 || e.ruleIdx >= len(p.Rules) {
		return
	}
	p.Rules = append(p.Rules[:e.ruleIdx], p.Rules[e.ruleIdx+1:]...)
	if e.ruleIdx >= len(p.Rules) {
		e.ruleIdx = len(p.Rules) - 1
	}
	e.refreshRules()
}

func (e *editorState) addProfile() {
	e.flushProfileName()
	e.flushRule()
	if len(e.raw.Profiles) >= editorMaxProfiles {
		walk.MsgBox(e.dlg, appName, fmt.Sprintf("最多 %d 个方案", editorMaxProfiles), walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	e.raw.Profiles = append(e.raw.Profiles, config.RawProfile{Rules: []config.RawRule{{Trigger: "t", Action: "tap:1", IntervalMS: 300}}})
	e.profileIdx = len(e.raw.Profiles) - 1
	e.ruleIdx = 0
	e.refreshProfiles()
}

func (e *editorState) deleteProfile() {
	e.flushProfileName()
	e.flushRule()
	if len(e.raw.Profiles) <= 1 {
		walk.MsgBox(e.dlg, appName, "至少保留一个方案", walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return
	}
	e.raw.Profiles = append(e.raw.Profiles[:e.profileIdx], e.raw.Profiles[e.profileIdx+1:]...)
	if e.profileIdx >= len(e.raw.Profiles) {
		e.profileIdx = len(e.raw.Profiles) - 1
	}
	e.ruleIdx = 0
	e.refreshProfiles()
}

func (e *editorState) buildRaw() config.Raw {
	e.flushProfileName()
	e.flushRule()
	raw := e.raw
	raw.Version = config.Version
	raw.Name = strings.TrimSpace(e.nameEdit.Text())
	raw.Author = strings.TrimSpace(e.authorEdit.Text())
	raw.Toggle = strings.TrimSpace(e.toggleEdit.Text())
	return raw
}

func (e *editorState) exportCode() {
	raw := e.buildRaw()
	code, err := config.Encode(raw)
	if err != nil {
		walk.MsgBox(e.dlg, appName, "配置不合法，无法导出：\n\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if err := walk.Clipboard().SetText(code); err != nil {
		walk.MsgBox(e.dlg, appName, "写入剪贴板失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	e.app.log.Printf("[编辑器] 已导出分享码（%d 字符）", len(code))
	walk.MsgBox(e.dlg, appName, "分享码已复制到剪贴板（"+strconv.Itoa(len(code))+" 字符）。", walk.MsgBoxOK|walk.MsgBoxIconInformation)
}

func (e *editorState) saveAndApply() {
	raw := e.buildRaw()
	parsed, err := config.Resolve(raw)
	if err != nil {
		walk.MsgBox(e.dlg, appName, "配置不合法，未保存：\n\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if err := e.app.setTargetProcess(e.app.settings.CurrentGame, strings.TrimSpace(e.procEdit.Text())); err != nil {
		walk.MsgBox(e.dlg, appName, "目标进程设置保存失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if err := e.app.applyConfig(&raw, parsed, e.profileIdx); err != nil {
		walk.MsgBox(e.dlg, appName, "保存失败：\n"+err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		return
	}
	if warns := config.Warnings(parsed); len(warns) > 0 {
		var sb strings.Builder
		sb.WriteString("配置已保存，但有以下注意事项：\n\n")
		for _, w := range warns {
			sb.WriteString("· " + w + "\n")
		}
		walk.MsgBox(e.dlg, appName, sb.String(), walk.MsgBoxOK|walk.MsgBoxIconWarning)
	}
	e.dlg.Accept()
}

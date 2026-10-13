//go:build windows

package main

import (
	"fmt"
	"strings"

	"github.com/imsxx/diablo-lazy-macro/internal/keys"

	"github.com/lxn/walk"
)

// keyChoice 是键位下拉里的一项：中文显示名 + 协议键名（internal/keys 白名单）。
type keyChoice struct{ label, token string }

// skillKeyChoices 返回「技能 / 动作按键」下拉的候选项，按暗黑系列的常用度排序。
func skillKeyChoices() []keyChoice {
	items := []keyChoice{
		{"鼠标左键", "LMB"},
		{"鼠标右键", "RMB"},
		{"鼠标中键", "MMB"},
		{"侧键1", "X1"},
		{"侧键2", "X2"},
	}
	for _, d := range "1234567890" {
		items = append(items, keyChoice{string(d), string(d)})
	}
	items = append(items, functionChoices()...)
	for _, c := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		items = append(items, keyChoice{string(c), string(c)})
	}
	return items
}

// keyboardKeyChoices 返回「启停键 / 方案切换键」下拉的候选项：协议规定这类键不支持鼠标键。
func keyboardKeyChoices() []keyChoice {
	items := append([]keyChoice{}, functionChoices()...)
	for _, d := range "1234567890" {
		items = append(items, keyChoice{string(d), string(d)})
	}
	for _, c := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		items = append(items, keyChoice{string(c), string(c)})
	}
	return items
}

func functionChoices() []keyChoice {
	items := make([]keyChoice, 0, 15)
	for i := 1; i <= 12; i++ {
		items = append(items, keyChoice{fmt.Sprintf("F%d", i), fmt.Sprintf("F%d", i)})
	}
	return append(items, keyChoice{"TAB", "TAB"}, keyChoice{"SPACE", "SPACE"}, keyChoice{"ESC", "ESC"})
}

// withNone 在候选项最前面插入一个空项（token 为空串），用于可留空的键位。
func withNone(label string, base []keyChoice) []keyChoice {
	return append([]keyChoice{{label, ""}}, base...)
}

// keyName 把虚拟键码转成界面上的中文名；未覆盖的键沿用协议键名。
func keyName(vk uint16) string {
	switch vk {
	case keys.VKLeftMouse:
		return "鼠标左键"
	case keys.VKRightMouse:
		return "鼠标右键"
	case keys.VKMiddleMouse:
		return "鼠标中键"
	case keys.VKXButton1:
		return "侧键1"
	case keys.VKXButton2:
		return "侧键2"
	}
	return keys.Label(vk)
}

// keyNameOfToken 把协议键名转成中文名；无法解析时原样返回（宽容显示，不阻断界面）。
func keyNameOfToken(token string) string {
	if strings.TrimSpace(token) == "" {
		return "（未选择）"
	}
	vk, err := keys.Parse(token)
	if err != nil {
		return token
	}
	return keyName(vk)
}

// keySequenceNames 把 "2,3" 这样的序列转成中文名串。
func keySequenceNames(arg string) string {
	names := make([]string, 0, 6)
	for _, p := range strings.Split(arg, ",") {
		if strings.TrimSpace(p) == "" {
			continue
		}
		names = append(names, keyNameOfToken(p))
	}
	if len(names) == 0 {
		return "（空序列）"
	}
	return strings.Join(names, " → ")
}

// setChoiceCombo 用候选项重建下拉并选中 token。token 不在候选中时**追加**一项，
// 保证导入的分享码在界面上原样可见、不会被悄悄改写。返回本次实际使用的候选项。
func (w *mainWindow) setChoiceCombo(cb *walk.ComboBox, base []keyChoice, token string) []keyChoice {
	items := append([]keyChoice{}, base...)
	token = strings.TrimSpace(token)
	if token != "" && indexOfToken(items, token) < 0 {
		items = append(items, keyChoice{keyNameOfToken(token), token})
	}
	labels := make([]string, 0, len(items))
	for _, it := range items {
		labels = append(labels, it.label)
	}
	if err := cb.SetModel(labels); err != nil {
		w.fatal(err)
		return items
	}
	idx := indexOfToken(items, token)
	if idx < 0 {
		idx = 0
	}
	if err := cb.SetCurrentIndex(idx); err != nil {
		w.fatal(err)
	}
	return items
}

func indexOfToken(items []keyChoice, token string) int {
	for i, it := range items {
		if it.token == token {
			return i
		}
	}
	return -1
}

// comboTokenOf 返回下拉当前项对应的协议键名；越界或空项返回空串。
func comboTokenOf(cb *walk.ComboBox, items []keyChoice) string {
	idx := cb.CurrentIndex()
	if idx < 0 || idx >= len(items) {
		return ""
	}
	return items[idx].token
}

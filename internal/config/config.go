// Package config 实现分享码协议 v1 的解析、编码与校验（契约文档：docs/share-code.md）。
// 解析必须 Fail Fast：任何不合法输入整段拒绝，禁止部分应用或静默降级。
package config

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/imsxx/diablo-lazy-macro/internal/games"
	"github.com/imsxx/diablo-lazy-macro/internal/keys"
)

const (
	// Prefix 是分享码前缀，字母大小写不敏感；其后为 base64url 编码的紧凑 JSON。
	Prefix = "DLM1:"
	// Version 是当前协议版本。
	Version = 1

	maxCodeChars       = 4096
	maxDecodedBytes    = 3000
	maxNameRunes       = 24
	maxProfiles        = 4
	maxRulesPerProfile = 8
	maxSequenceKeys    = 6
	minIntervalMS      = 20
	maxIntervalMS      = 600000
)

// ActionKind 是规则动作类型。
type ActionKind string

const (
	ActionTap  ActionKind = "tap"  // 连点：按间隔重复点击
	ActionOnce ActionKind = "once" // 单点：每次触发开始时点击一次
	ActionHold ActionKind = "hold" // 按住不放：触发期间保持按下
	ActionSeq  ActionKind = "seq"  // 序列：按间隔循环点击多个键
)

// Raw 与分享码 JSON 一一对应，仅用于编解码。
type Raw struct {
	Version  int          `json:"v"`
	Game     string       `json:"g,omitempty"`
	Name     string       `json:"n,omitempty"`
	Author   string       `json:"a,omitempty"`
	Toggle   string       `json:"tg,omitempty"`
	Profiles []RawProfile `json:"p"`
}

// RawProfile 是一套可切换的规则集合的 JSON 形态。
type RawProfile struct {
	Name  string    `json:"n,omitempty"`
	HK    string    `json:"hk,omitempty"` // 方案切换键（仅键盘键，可留空）
	Rules []RawRule `json:"r"`
}

// RawRule 是单条规则的 JSON 形态。
type RawRule struct {
	Trigger    string `json:"tg"`
	Action     string `json:"a"`
	IntervalMS int    `json:"i,omitempty"`
}

// Parsed 是校验并解析后的可执行形式。
type Parsed struct {
	Game     string
	Name     string
	Author   string
	ToggleVK uint16
	Profiles []ParsedProfile
}

// ParsedProfile 是解析后的方案；SwitchVK == 0 表示未设置方案切换键。
type ParsedProfile struct {
	Name     string
	SwitchVK uint16
	Rules    []ParsedRule
}

// ParsedRule 是解析后的规则；HoldVK == 0 表示宏开启期间持续触发，否则为按住触发键。
type ParsedRule struct {
	HoldVK     uint16
	Kind       ActionKind
	Keys       []uint16
	IntervalMS int
}

// Decode 解析分享码，同时返回原始形态（用于持久化与再导出）与可执行形式。
// 整段不合法即返回错误（Fail Fast）。
func Decode(code string) (*Raw, *Parsed, error) {
	raw, err := decode(code)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := resolve(raw)
	if err != nil {
		return nil, nil, err
	}
	return raw, parsed, nil
}

// Parse 仅需可执行形式时的便捷入口。
func Parse(code string) (*Parsed, error) {
	_, parsed, err := Decode(code)
	return parsed, err
}

// Resolve 校验并解析原始形态（用于从本地存档恢复）。
func Resolve(raw Raw) (*Parsed, error) {
	return resolve(&raw)
}

// Encode 校验并编码分享码（用于导出与分享）。
func Encode(r Raw) (string, error) {
	if _, err := resolve(&r); err != nil {
		return "", err
	}
	b, err := json.Marshal(&r)
	if err != nil {
		return "", fmt.Errorf("编码 JSON 失败：%v", err)
	}
	return Prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func decode(code string) (*Raw, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, fmt.Errorf("分享码为空")
	}
	if len(code) > maxCodeChars {
		return nil, fmt.Errorf("分享码过长（%d 字符，上限 %d）", len(code), maxCodeChars)
	}
	idx := strings.IndexByte(code, ':')
	if idx < 0 || !strings.EqualFold(code[:idx], strings.TrimSuffix(Prefix, ":")) {
		return nil, fmt.Errorf("分享码必须以 %s 开头", Prefix)
	}
	payload := stripSpace(code[idx+1:])
	if payload == "" {
		return nil, fmt.Errorf("分享码正文为空")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(payload, "="))
	if err != nil {
		return nil, fmt.Errorf("分享码正文不是合法的 Base64：%v", err)
	}
	if len(data) > maxDecodedBytes {
		return nil, fmt.Errorf("分享码解码后过大（%d 字节，上限 %d）", len(data), maxDecodedBytes)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("分享码解码结果不是合法 UTF-8 文本")
	}
	var raw Raw
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("分享码 JSON 解析失败：%v", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("分享码 JSON 末尾存在多余内容")
	}
	return &raw, nil
}

func stripSpace(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r':
			return -1
		}
		return r
	}, s)
}

func resolve(raw *Raw) (*Parsed, error) {
	if raw.Version != Version {
		return nil, fmt.Errorf("分享码版本为 %d，本工具只支持版本 %d（请升级工具）", raw.Version, Version)
	}
	if raw.Game != "" {
		if _, ok := games.Lookup(raw.Game); !ok {
			return nil, fmt.Errorf("游戏标识 %q 无效（支持 %s）", raw.Game, gameIDList())
		}
	}
	if err := checkName("配置名", raw.Name); err != nil {
		return nil, err
	}
	if err := checkName("作者署名", raw.Author); err != nil {
		return nil, err
	}

	toggleName := raw.Toggle
	if strings.TrimSpace(toggleName) == "" {
		toggleName = "F1"
	}
	toggleVK, err := keys.Parse(toggleName)
	if err != nil {
		return nil, fmt.Errorf("启停键无效：%v", err)
	}
	if keys.IsMouse(toggleVK) {
		return nil, fmt.Errorf("启停键不支持鼠标键（%s）", keys.Label(toggleVK))
	}

	if len(raw.Profiles) < 1 || len(raw.Profiles) > maxProfiles {
		return nil, fmt.Errorf("方案数量必须在 1–%d 之间，实际 %d", maxProfiles, len(raw.Profiles))
	}
	parsed := &Parsed{Game: raw.Game, Name: raw.Name, Author: raw.Author, ToggleVK: toggleVK}
	for pi, rp := range raw.Profiles {
		if err := checkName(fmt.Sprintf("方案 #%d 名称", pi+1), rp.Name); err != nil {
			return nil, err
		}
		if len(rp.Rules) < 1 || len(rp.Rules) > maxRulesPerProfile {
			return nil, fmt.Errorf("方案 #%d 的规则数量必须在 1–%d 之间，实际 %d", pi+1, maxRulesPerProfile, len(rp.Rules))
		}
		pp := ParsedProfile{Name: rp.Name}
		if hk := strings.TrimSpace(rp.HK); hk != "" {
			vk, err := keys.Parse(hk)
			if err != nil {
				return nil, fmt.Errorf("方案 #%d 的切换键无效：%v", pi+1, err)
			}
			if keys.IsMouse(vk) {
				return nil, fmt.Errorf("方案 #%d 的切换键不支持鼠标键（%s），请改用键盘键", pi+1, keys.Label(vk))
			}
			pp.SwitchVK = vk
		}
		for ri, rr := range rp.Rules {
			rule, err := resolveRule(pi+1, ri+1, rr)
			if err != nil {
				return nil, err
			}
			pp.Rules = append(pp.Rules, *rule)
		}
		parsed.Profiles = append(parsed.Profiles, pp)
	}
	return parsed, nil
}

func resolveRule(profileNo, ruleNo int, r RawRule) (*ParsedRule, error) {
	where := fmt.Sprintf("方案 #%d 规则 #%d", profileNo, ruleNo)
	rule := &ParsedRule{IntervalMS: r.IntervalMS}

	trigger := strings.TrimSpace(r.Trigger)
	switch {
	case strings.EqualFold(trigger, "t"):
		// HoldVK 保持零值：宏开启期间持续触发
	case strings.HasPrefix(strings.ToLower(trigger), "h:"):
		keyName := strings.TrimSpace(trigger[2:])
		if keyName == "" {
			return nil, fmt.Errorf("%s 的触发方式是「按住指定键」，但没有选择触发键", where)
		}
		vk, err := keys.Parse(keyName)
		if err != nil {
			return nil, fmt.Errorf("%s 的触发键无效：%v", where, err)
		}
		rule.HoldVK = vk
	default:
		return nil, fmt.Errorf("%s 的触发方式 %q 无效（应为 t 或 h:<键名>）", where, r.Trigger)
	}

	kindRaw, arg, ok := strings.Cut(r.Action, ":")
	if !ok {
		return nil, fmt.Errorf("%s 的动作 %q 无效（应为 tap:<键> / once:<键> / hold:<键> / seq:<键,键...>）", where, r.Action)
	}
	kind := ActionKind(strings.ToLower(strings.TrimSpace(kindRaw)))
	switch kind {
	case ActionTap, ActionOnce, ActionHold:
		if strings.TrimSpace(arg) == "" {
			return nil, fmt.Errorf("%s 的动作 %s 没有选择按键", where, kind)
		}
		vk, err := keys.Parse(arg)
		if err != nil {
			return nil, fmt.Errorf("%s 的动作键无效：%v", where, err)
		}
		rule.Kind = kind
		rule.Keys = []uint16{vk}
	case ActionSeq:
		parts := strings.Split(arg, ",")
		if len(parts) < 1 || len(parts) > maxSequenceKeys {
			return nil, fmt.Errorf("%s 的序列长度必须在 1–%d 之间，实际 %d", where, maxSequenceKeys, len(parts))
		}
		for i, p := range parts {
			if strings.TrimSpace(p) == "" {
				return nil, fmt.Errorf("%s 的序列第 %d 个按键为空", where, i+1)
			}
			vk, err := keys.Parse(p)
			if err != nil {
				return nil, fmt.Errorf("%s 的序列键无效：%v", where, err)
			}
			rule.Keys = append(rule.Keys, vk)
		}
		rule.Kind = ActionSeq
	default:
		return nil, fmt.Errorf("%s 的动作类型 %q 无效", where, kindRaw)
	}

	switch rule.Kind {
	case ActionTap, ActionSeq:
		if r.IntervalMS < minIntervalMS || r.IntervalMS > maxIntervalMS {
			return nil, fmt.Errorf("%s 的间隔必须在 %d–%d 毫秒之间，实际 %d", where, minIntervalMS, maxIntervalMS, r.IntervalMS)
		}
	default:
		if r.IntervalMS != 0 {
			return nil, fmt.Errorf("%s 的动作 %s 不接受间隔字段（i）", where, rule.Kind)
		}
	}
	return rule, nil
}

func checkName(field, name string) error {
	if utf8.RuneCountInString(name) > maxNameRunes {
		return fmt.Errorf("%s 超过 %d 个字符上限", field, maxNameRunes)
	}
	return nil
}

func gameIDList() string {
	ids := make([]string, 0, 4)
	for _, g := range games.All() {
		ids = append(ids, g.ID)
	}
	return strings.Join(ids, "/")
}

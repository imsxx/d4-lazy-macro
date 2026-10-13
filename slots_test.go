//go:build windows

package main

import (
	"testing"

	"github.com/imsxx/diablo-lazy-macro/internal/config"
)

func TestDefaultConfigHasFourProfiles(t *testing.T) {
	cfg := defaultConfig()
	if len(cfg.Profiles) != 4 {
		t.Fatalf("预期 4 个默认方案，实际 %d", len(cfg.Profiles))
	}
	parsed, err := config.Resolve(cfg)
	if err != nil {
		t.Fatalf("默认配置校验失败：%v", err)
	}
	if len(parsed.Profiles) != 4 {
		t.Fatalf("解析后方案数量不为 4：%d", len(parsed.Profiles))
	}
}

func TestSlotConversionStrategies(t *testing.T) {
	slots := defaultSlots()
	// Slot 0: 连点 150ms
	slots[0].Strategy = 2
	slots[0].IntervalMS = 150

	// Slot 1: 按住不放
	slots[1].Strategy = 1

	// Slot 2: 保持Buff
	slots[2].Strategy = 3
	slots[2].IntervalMS = 300

	// Slot 3: 禁用
	slots[3].Strategy = 0

	prof := slotsToProfile(slots, "测试方案", "F9", config.ModeConcurrent)
	if prof.Name != "测试方案" || prof.HK != "F9" {
		t.Fatalf("方案元数据不符：%+v", prof)
	}

	converted := profileToSlots(prof)

	if converted[0].Strategy != 2 || converted[0].IntervalMS != 150 {
		t.Errorf("Slot 0 连点转换不符：Strategy=%d, Interval=%d", converted[0].Strategy, converted[0].IntervalMS)
	}
	if converted[1].Strategy != 1 {
		t.Errorf("Slot 1 按住不放转换不符：Strategy=%d", converted[1].Strategy)
	}
	if converted[2].Strategy != 3 {
		t.Errorf("Slot 2 保持Buff转换不符：Strategy=%d", converted[2].Strategy)
	}
	if converted[3].Strategy != 0 {
		t.Errorf("Slot 3 禁用转换不符：Strategy=%d", converted[3].Strategy)
	}
}

func TestSlotExplanation(t *testing.T) {
	sHold := skillSlotData{Key: "1", Strategy: 1}
	expHold := slotExplanation(sHold)
	if expHold != "持续保持按下「1」（旋风斩/扫射/引导类技能专长）" {
		t.Errorf("按住不放说明不符：%s", expHold)
	}

	sBuff := skillSlotData{Key: "2", Strategy: 3}
	expBuff := slotExplanation(sBuff)
	if expBuff != "智能屏幕找色保持「2」Buff（发光环未结束绝不按键）" {
		t.Errorf("保持Buff说明不符：%s", expBuff)
	}

	sSpam := skillSlotData{Key: "3", Strategy: 2, IntervalMS: 1}
	expSpam := slotExplanation(sSpam)
	if expSpam != "每 1ms 持续连点「3」" {
		t.Errorf("连点 1ms 说明不符：%s", expSpam)
	}
}

func TestSlotStrategySurvivesRoundTrip(t *testing.T) {
	// 策略必须显式写入配置：否则「保持Buff」在引擎侧会丢失屏幕守卫语义。
	slots := defaultSlots()
	slots[0].Strategy = strategyBuff
	slots[0].IntervalMS = 300

	prof := slotsToProfile(slots, "守卫往返", "", config.ModeConcurrent)
	raw := config.Raw{Version: config.Version, Profiles: []config.RawProfile{prof}}
	parsed, err := config.Resolve(raw)
	if err != nil {
		t.Fatalf("策略往返配置校验失败：%v", err)
	}
	if !parsed.Profiles[0].Rules[0].Buff {
		t.Fatalf("保持Buff 策略未落到协议 Buff 标记：%+v", parsed.Profiles[0].Rules[0])
	}
	back := profileToSlots(prof)
	if back[0].Strategy != strategyBuff {
		t.Fatalf("往返后策略丢失：got %d，want %d", back[0].Strategy, strategyBuff)
	}
}

func TestSlotDelaySurvivesRoundTrip(t *testing.T) {
	// 连点的延迟与延迟随机必须完整写入配置并能读回。
	slots := defaultSlots()
	slots[0].Strategy = strategySpam
	slots[0].IntervalMS = 150
	slots[0].DelayMS = -40
	slots[0].RandomDelay = true

	prof := slotsToProfile(slots, "延迟往返", "", config.ModeConcurrent)
	raw := config.Raw{Version: config.Version, Profiles: []config.RawProfile{prof}}
	parsed, err := config.Resolve(raw)
	if err != nil {
		t.Fatalf("延迟配置应当合法：%v", err)
	}
	r := parsed.Profiles[0].Rules[0]
	if r.DelayMS != -40 || !r.RandomDelay || r.Strategy != config.StrategySpam {
		t.Fatalf("延迟写入协议有误：%+v", r)
	}

	got := profileToSlots(prof)[0]
	if got.DelayMS != -40 || !got.RandomDelay {
		t.Fatalf("延迟往返后丢失：%+v", got)
	}
}

func TestSlotBuffDropsDelay(t *testing.T) {
	// 延迟只对连点有意义：保持Buff 必须把它裁掉，否则协议会拒绝保存。
	slots := defaultSlots()
	slots[1].Strategy = strategyBuff
	slots[1].IntervalMS = 300
	slots[1].DelayMS = 100
	slots[1].RandomDelay = true

	prof := slotsToProfile(slots, "buff 干净字段", "", config.ModeConcurrent)
	raw := config.Raw{Version: config.Version, Profiles: []config.RawProfile{prof}}
	parsed, err := config.Resolve(raw)
	if err != nil {
		t.Fatalf("保持Buff 配置应当合法（延迟须被裁剪）：%v", err)
	}
	r := parsed.Profiles[0].Rules[0]
	if !r.Buff || r.DelayMS != 0 || r.RandomDelay {
		t.Fatalf("保持Buff 不应携带延迟：%+v", r)
	}
}

func TestSlotRandomDelayRequiresDelay(t *testing.T) {
	// 没有延迟时不应写出延迟随机（协议要求 rd 与 d 成对）。
	slots := defaultSlots()
	slots[0].Strategy = strategySpam
	slots[0].IntervalMS = 150
	slots[0].DelayMS = 0
	slots[0].RandomDelay = true

	prof := slotsToProfile(slots, "无延迟随机", "", config.ModeConcurrent)
	raw := config.Raw{Version: config.Version, Profiles: []config.RawProfile{prof}}
	if _, err := config.Resolve(raw); err != nil {
		t.Fatalf("无延迟时不应写出 rd：%v", err)
	}
}

func TestSlotStrategyInferredFromLegacyConfig(t *testing.T) {
	// v1 旧码不含策略字段：按动作类型与间隔推断（tap + 300ms 视为保持Buff）。
	prof := config.RawProfile{Rules: []config.RawRule{
		{Trigger: "t", Action: "hold:RMB"},
		{Trigger: "t", Action: "tap:2", IntervalMS: 300},
		{Trigger: "t", Action: "tap:1", IntervalMS: 100},
	}}
	slots := profileToSlots(prof)
	if slots[5].Strategy != strategyHold {
		t.Errorf("按住不放推断错误：%d", slots[5].Strategy)
	}
	if slots[1].Strategy != strategyBuff {
		t.Errorf("保持Buff 推断错误：%d", slots[1].Strategy)
	}
	if slots[0].Strategy != strategySpam {
		t.Errorf("连点推断错误：%d", slots[0].Strategy)
	}
}

func TestProfileModeRoundTrip(t *testing.T) {
	slots := defaultSlots()
	slots[0].Strategy = strategySpam
	slots[0].IntervalMS = 150

	prof := slotsToProfile(slots, "循环模式", "", config.ModeSequential)
	raw := config.Raw{Version: config.Version, Profiles: []config.RawProfile{prof}}
	parsed, err := config.Resolve(raw)
	if err != nil {
		t.Fatalf("循环模式配置应当合法：%v", err)
	}
	if parsed.Profiles[0].Mode != config.ModeSequential {
		t.Fatalf("循环模式未写入协议：%+v", parsed.Profiles[0])
	}
}

func TestProfileModeDefaultsToConcurrent(t *testing.T) {
	// 缺省（空字符串）归一化为并发：旧配置的既有行为不变。
	prof := config.RawProfile{Rules: []config.RawRule{{Trigger: "t", Action: "tap:1", IntervalMS: 250}}}
	parsed, err := config.Resolve(config.Raw{Version: config.Version, Profiles: []config.RawProfile{prof}})
	if err != nil {
		t.Fatalf("缺省模式应当合法：%v", err)
	}
	if parsed.Profiles[0].Mode != config.ModeConcurrent {
		t.Fatalf("缺省模式应为并发，实际 %q", parsed.Profiles[0].Mode)
	}
}

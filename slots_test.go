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

	prof := slotsToProfile(slots, "测试方案", "F9")
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

	prof := slotsToProfile(slots, "守卫往返", "")
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

func TestSlotTuningSurvivesRoundTrip(t *testing.T) {
	// 连点的高级参数（延迟/随机/重复）必须完整写入配置并能读回。
	slots := defaultSlots()
	slots[0].Strategy = strategySpam
	slots[0].IntervalMS = 150
	slots[0].DelayMS = -40
	slots[0].RandomDelay = true
	slots[0].Repeat = 3
	slots[0].RepeatInterval = 60
	slots[0].Priority = 5 // 连点策略不该写入优先级

	prof := slotsToProfile(slots, "调参往返", "")
	raw := config.Raw{Version: config.Version, Profiles: []config.RawProfile{prof}}
	parsed, err := config.Resolve(raw)
	if err != nil {
		t.Fatalf("调参配置应当合法：%v", err)
	}
	r := parsed.Profiles[0].Rules[0]
	if r.DelayMS != -40 || !r.RandomDelay || r.Repeat != 3 || r.RepeatInterval != 60 {
		t.Fatalf("连点调参写入协议有误：%+v", r)
	}
	if r.Priority != 0 {
		t.Fatalf("连点策略不应写入优先级：%+v", r)
	}

	got := profileToSlots(prof)[0]
	if got.DelayMS != -40 || !got.RandomDelay || got.Repeat != 3 || got.RepeatInterval != 60 {
		t.Fatalf("调参往返后丢失：%+v", got)
	}
}

func TestSlotBuffPriorityCropsSpamFields(t *testing.T) {
	// 保持Buff 只带优先级；连点专属参数必须被裁剪，否则协议会拒绝保存。
	slots := defaultSlots()
	slots[1].Strategy = strategyBuff
	slots[1].IntervalMS = 300
	slots[1].Priority = 4
	slots[1].DelayMS = 100
	slots[1].RandomDelay = true
	slots[1].Repeat = 5
	slots[1].RepeatInterval = 80

	prof := slotsToProfile(slots, "buff 往返", "")
	raw := config.Raw{Version: config.Version, Profiles: []config.RawProfile{prof}}
	parsed, err := config.Resolve(raw)
	if err != nil {
		t.Fatalf("保持Buff 配置应当合法（连点专属参数须被裁剪）：%v", err)
	}
	r := parsed.Profiles[0].Rules[0]
	if !r.Buff || r.Priority != 4 {
		t.Fatalf("保持Buff 优先级写入有误：%+v", r)
	}
	if r.DelayMS != 0 || r.RandomDelay || r.Repeat != 0 || r.RepeatInterval != 0 {
		t.Fatalf("保持Buff 不应携带连点专属参数：%+v", r)
	}
	if got := profileToSlots(prof)[1].Priority; got != 4 {
		t.Fatalf("优先级往返丢失：%d", got)
	}
}

func TestSlotRepeatBelowTwoIsNotWritten(t *testing.T) {
	// 重复次数为 1（或留空）表示不重复：不应写出 rp，否则协议会拒绝。
	slots := defaultSlots()
	slots[0].Strategy = strategySpam
	slots[0].IntervalMS = 120
	slots[0].Repeat = 1
	slots[0].RepeatInterval = 60

	prof := slotsToProfile(slots, "单次连点", "")
	raw := config.Raw{Version: config.Version, Profiles: []config.RawProfile{prof}}
	parsed, err := config.Resolve(raw)
	if err != nil {
		t.Fatalf("重复次数不足 2 时不应写出 rp：%v", err)
	}
	if parsed.Profiles[0].Rules[0].Repeat != 0 {
		t.Fatalf("重复次数应为 0：%+v", parsed.Profiles[0].Rules[0])
	}
}

func TestSlotRandomDelayRequiresDelay(t *testing.T) {
	// 没有延迟时不应写出延迟随机（协议要求 rd 与 d 成对）。
	slots := defaultSlots()
	slots[0].Strategy = strategySpam
	slots[0].IntervalMS = 150
	slots[0].DelayMS = 0
	slots[0].RandomDelay = true

	prof := slotsToProfile(slots, "无延迟随机", "")
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

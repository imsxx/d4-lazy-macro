package config

import "testing"

func TestWarningsDetectsSelfTrigger(t *testing.T) {
	p := &Parsed{
		ToggleVK: 0x70, // F1
		Profiles: []ParsedProfile{{Rules: []ParsedRule{
			{HoldVK: 0x05, Kind: ActionTap, Keys: []uint16{0x31}, IntervalMS: 100}, // 按住 X1 连点 1，没问题
			{Kind: ActionHold, Keys: []uint16{0x05}},                               // 动作按住 X1，与触发键重合
			{Kind: ActionTap, Keys: []uint16{0x70}, IntervalMS: 500},               // 动作按 F1，与启停键重合
			{Kind: ActionTap, Keys: []uint16{0x31}, IntervalMS: 500},               // 键 1 重复作为动作，但一次提示即可
		}}},
	}
	warnings := Warnings(p)
	if len(warnings) != 2 {
		t.Fatalf("期望 2 条警告，实际 %d 条：%v", len(warnings), warnings)
	}
}

func TestWarningsCleanConfig(t *testing.T) {
	p := &Parsed{
		ToggleVK: 0x70,
		Profiles: []ParsedProfile{{Rules: []ParsedRule{
			{HoldVK: 0x05, Kind: ActionTap, Keys: []uint16{0x31}, IntervalMS: 100},
			{Kind: ActionOnce, Keys: []uint16{0x32}},
		}}},
	}
	if warnings := Warnings(p); len(warnings) != 0 {
		t.Fatalf("干净配置不应有警告，实际：%v", warnings)
	}
}

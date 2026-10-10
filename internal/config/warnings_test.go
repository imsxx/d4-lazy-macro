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

func TestWarningsDetectsSwitchKeyConflicts(t *testing.T) {
	p := &Parsed{
		ToggleVK: 0x70, // F1
		Profiles: []ParsedProfile{
			{Name: "速刷", SwitchVK: 0x71, Rules: []ParsedRule{{Kind: ActionTap, Keys: []uint16{0x31}, IntervalMS: 100}}},
			{Name: "攻坚", SwitchVK: 0x71, Rules: []ParsedRule{{Kind: ActionOnce, Keys: []uint16{0x32}}}},
			{Name: "冲层", SwitchVK: 0x70, Rules: []ParsedRule{{Kind: ActionOnce, Keys: []uint16{0x33}}}},
		},
	}
	warnings := Warnings(p)
	if len(warnings) != 2 {
		t.Fatalf("期望 2 条警告（切换键重复、切换键与启停键重合），实际 %d 条：%v", len(warnings), warnings)
	}
}

func TestWarningsDetectsSwitchKeyAsActionKey(t *testing.T) {
	p := &Parsed{
		Profiles: []ParsedProfile{{Name: "速刷", SwitchVK: 0x77, Rules: []ParsedRule{
			{Kind: ActionTap, Keys: []uint16{0x77}, IntervalMS: 100},
		}}},
	}
	if warnings := Warnings(p); len(warnings) != 1 {
		t.Fatalf("期望 1 条警告（切换键同时是动作键），实际 %d 条：%v", len(warnings), warnings)
	}
}

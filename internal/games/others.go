package games

// 暗黑破坏神4 与 暗黑破坏神2：重制版 的适配器。
// 目前两者均无屏幕 Buff 守卫（BuffActive 恒为 false），其余能力与暗黑3一致；
// 后续如需接入各自的屏幕识别或助手功能，在此实现，不污染通用层。

type d4Adapter struct{}

func (d4Adapter) ID() string                                { return "d4" }
func (d4Adapter) Name() string                              { return "暗黑破坏神4" }
func (d4Adapter) DefaultProcess() string                    { return "Diablo IV.exe" }
func (d4Adapter) Slots() []SlotDef                          { return standardSlots() }
func (d4Adapter) BuffActive(uintptr, int, PixelSource) bool { return false }

type d2rAdapter struct{}

func (d2rAdapter) ID() string                                { return "d2r" }
func (d2rAdapter) Name() string                              { return "暗黑破坏神2：重制版" }
func (d2rAdapter) DefaultProcess() string                    { return "D2R.exe" }
func (d2rAdapter) Slots() []SlotDef                          { return standardSlots() }
func (d2rAdapter) BuffActive(uintptr, int, PixelSource) bool { return false }

// standardSlots 是暗黑系列通用的 6 技能槽默认定义。
func standardSlots() []SlotDef {
	return []SlotDef{
		{Name: "技能一：", DefaultKey: "1"},
		{Name: "技能二：", DefaultKey: "2"},
		{Name: "技能三：", DefaultKey: "3"},
		{Name: "技能四：", DefaultKey: "4"},
		{Name: "左键技能：", DefaultKey: "LMB"},
		{Name: "右键技能：", DefaultKey: "RMB"},
	}
}

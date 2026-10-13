package helper

import (
	"fmt"

	"github.com/imsxx/diablo-lazy-macro/internal/games"
	"github.com/imsxx/diablo-lazy-macro/internal/keys"
)

func init() { Register(D3KanaiUpgradeConvert{}) }

// d3BagSlotCount 是背包格子总数（10 列 x 6 行）。
const d3BagSlotCount = 60

// D3KanaiUpgradeConvert 是卡奈魔盒的「升级稀有装备 / 转化材料」助手
//（对应 D3keyHelper 的 oneButtonUpgradeConvertHelper）：把背包里的装备逐个送进魔盒执行一次转化。
//
// 与脚本的差异：脚本另起线程扫描哪些格子有装备（scanInventorySpaceGDIP）以跳过空格。
// 这里先按「遍历全部 60 格」实现——对空格执行同样动作在游戏里是无操作，功能上等价，
// 只是更慢；格子扫描留作后续加速优化（也顺带避免了那套多点聚合取色的移植成本）。
type D3KanaiUpgradeConvert struct{}

func (D3KanaiUpgradeConvert) ID() string { return "d3.kanai.upgrade_convert" }
func (D3KanaiUpgradeConvert) Name() string {
	return "卡奈魔盒 · 升级/转化"
}

func (D3KanaiUpgradeConvert) Run(env *Env) error {
	state := games.D3KanaiCubeOpen(env.Lang, env.Sampler())
	if state != games.D3KanaiUpgrade && state != games.D3KanaiConvert {
		return fmt.Errorf("不在卡奈魔盒的升级或材料转化界面：请先打开魔盒并切到对应页面")
	}
	buttons, ok := games.D3KanaiButtonXY(env.WinH)
	if !ok {
		return fmt.Errorf("无法计算魔盒按钮坐标")
	}

	origX, origY, err := env.MouseRel()
	if err != nil {
		return err
	}
	defer func() { _ = env.MoveTo(origX, origY) }()

	for id := 1; id <= d3BagSlotCount; id++ {
		if env.aborted() {
			return AbortError{Helper: "升级/转化"}
		}
		slot, ok := games.D3InventorySlotXY(id, games.D3GridBag, env.WinW, env.WinH)
		if !ok {
			break
		}
		// 右键把该格装备送进魔盒
		if err := env.MoveTo(slot.CenterX, slot.CenterY); err != nil {
			return err
		}
		env.SleepHalf()
		if err := env.Mouse.Tap(keys.VKRightMouse); err != nil {
			return err
		}
		env.Sleep()

		// 添加材料 → 转化 → 清空魔盒（对应脚本的 k[2] → k[1] → k[4] → k[3]）
		for _, btn := range []games.D3Point{
			buttons.InsertItem, buttons.Transmute, buttons.NextPage, buttons.PrevPage,
		} {
			if err := env.ClickAt(btn.X, btn.Y, keys.VKLeftMouse); err != nil {
				return err
			}
			env.Sleep()
		}
		env.debugf("已处理第 %d 格", id)
	}
	env.logf("升级/转化完成：已遍历背包 %d 格", d3BagSlotCount)
	return nil
}

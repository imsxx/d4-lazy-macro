package helper

import (
	"fmt"
	"time"

	"github.com/imsxx/diablo-lazy-macro/internal/games"
	"github.com/imsxx/diablo-lazy-macro/internal/keys"
)

func init() { Register(D3Reforge{}) }

// 重铸后的装备品质档位（数值与 D3keyHelper 的判定结果一致，越大约好）。
const (
	reforgeQualityLegendary = 2 // 普通传奇
	reforgeQualityAncient   = 3 // 远古传奇
	reforgeQualitySacred    = 4 // 神圣装备
	reforgeQualityPrimal    = 5 // 太古传奇
)

// d3MaxReforge 是单次运行的重铸次数上限：即使品质判定误判也不会无限重铸下去。
const d3MaxReforge = 30

// D3Reforge 是暗黑3 的「卡奈魔盒 · 重铸」助手。
//
// 对应 D3keyHelper 的 oneButtonReforgeHelper：把鼠标所指装备放进魔盒 → 放材料 → 点转化
//（这一套动作即一次重铸），然后可选地判断重铸结果的品质，未达到目标就再来一次。
//
// 目标品质由 Target 指定：0 表示只重铸一次。
type D3Reforge struct {
	// Target 是期望达到的品质档位，0 表示只重铸一次。
	Target int
}

func (D3Reforge) ID() string   { return "d3.kanai.reforge" }
func (D3Reforge) Name() string { return "卡奈魔盒 · 重铸" }

func (r D3Reforge) Run(env *Env) error {
	at := env.Sampler()
	if games.D3KanaiCubeOpen(env.Lang, at) != games.D3KanaiReforge {
		return fmt.Errorf("不在卡奈魔盒的重铸界面：请先打开魔盒并切到「重铸」页")
	}
	buttons, ok := games.D3KanaiButtonXY(env.WinH)
	if !ok {
		return fmt.Errorf("无法计算魔盒按钮坐标")
	}
	slot1, ok := games.D3InventorySlotXY(1, games.D3GridKanai, env.WinW, env.WinH)
	if !ok {
		return fmt.Errorf("无法计算魔盒格子坐标")
	}
	slot2, ok := games.D3InventorySlotXY(2, games.D3GridKanai, env.WinW, env.WinH)
	if !ok {
		return fmt.Errorf("无法计算魔盒格子坐标")
	}

	// 记下玩家鼠标位置：整个流程会反复挪动光标，结束时必须还回去。
	origX, origY, err := env.MouseRel()
	if err != nil {
		return err
	}
	if zone := games.D3MouseZoneAt(origX, origY, env.WinW, env.WinH); zone != games.D3ZoneBag {
		return fmt.Errorf("请把鼠标移到要重铸的装备上再执行（当前鼠标不在背包区域）")
	}
	restore := func() { _ = env.MoveTo(origX, origY) }
	defer restore()

	limit := d3MaxReforge
	if r.Target <= 0 {
		limit = 1
	}
	for i := 0; i < limit; i++ {
		if env.aborted() {
			return AbortError{Helper: "重铸"}
		}

		// 一次重铸：放入装备 → 放入材料 → 转化 → 翻页复位配方选择
		if err := env.Mouse.Tap(keys.VKRightMouse); err != nil {
			return err
		}
		env.SleepQuarter()
		if err := env.ClickAt(buttons.InsertItem.X, buttons.InsertItem.Y, keys.VKLeftMouse); err != nil {
			return err
		}
		env.SleepQuarter()
		if err := env.ClickAt(buttons.Transmute.X, buttons.Transmute.Y, keys.VKLeftMouse); err != nil {
			return err
		}
		env.SleepQuarter()
		if err := env.ClickAt(buttons.PrevPage.X, buttons.PrevPage.Y, keys.VKLeftMouse); err != nil {
			return err
		}
		env.SleepQuarter()
		if err := env.ClickAt(buttons.NextPage.X, buttons.NextPage.Y, keys.VKLeftMouse); err != nil {
			return err
		}
		if r.Target <= 0 {
			break
		}

		// 条件重铸：把装备重新放回魔盒第 1 格，等动画停稳后判读品质。
		if err := env.MoveTo(origX, origY); err != nil {
			return err
		}
		if err := env.Mouse.Tap(keys.VKRightMouse); err != nil {
			return err
		}
		if err := env.MoveTo(slot1.CenterX, slot1.CenterY); err != nil {
			return err
		}
		env.SleepHalf()
		color, ok := env.stableBorderColor(at, float64(slot2.OuterX)+1, float64(slot2.CenterY))
		if !ok {
			return fmt.Errorf("无法读取重铸后的装备品质")
		}
		quality := reforgeQuality(color)
		env.debugf("第 %d 次重铸：品质=%d（目标 %d）", i+1, quality, r.Target)
		if quality >= r.Target {
			env.logf("重铸完成：第 %d 次达到品质 %d", i+1, quality)
			return nil
		}
	}
	env.logf("重铸结束：已达到次数上限或只重铸一次")
	return nil
}

// stableBorderColor 反复取 3x1 区域的最大值，直到颜色不再变化（动画播完）或超时。
// 对应脚本里「取色直到 c_t 与前一次相同」的等待。
func (e *Env) stableBorderColor(at games.D3ColorAt, baseX, baseY float64) (games.D3Color, bool) {
	prev := games.D3Color{R: -255, G: -255, B: -255}
	deadline := e.Delay
	if deadline <= 0 {
		deadline = 100 * time.Millisecond
	}
	for elapsed := time.Duration(0); elapsed <= deadline; elapsed += 20 * time.Millisecond {
		c, ok := maxColor3(at, baseX, baseY)
		if !ok {
			return games.D3Color{}, false
		}
		if c == prev {
			return c, true
		}
		prev = c
		time.Sleep(20 * time.Millisecond)
	}
	return prev, true
}

// maxColor3 取 (baseX..baseX+2, baseY) 三个点各通道的最大值，
// 对应脚本的 getPixelsRGB(..., 3, 1, "Max")。
func maxColor3(at games.D3ColorAt, baseX, baseY float64) (games.D3Color, bool) {
	var out games.D3Color
	for i := 0; i < 3; i++ {
		c, ok := at(baseX+float64(i), baseY, games.D3AnchorLeft)
		if !ok {
			return games.D3Color{}, false
		}
		out.R, out.G, out.B = maxOf(out.R, c.R), maxOf(out.G, c.G), maxOf(out.B, c.B)
	}
	return out, true
}

// reforgeQuality 依据装备图标边框的采样颜色判断品质（判据照搬脚本）。
func reforgeQuality(c games.D3Color) int {
	spread := maxOf(absOf(c.R-c.G), maxOf(absOf(c.R-c.B), absOf(c.B-c.G)))
	switch {
	case (c.R >= 70 || c.B <= 20) && spread > 20 && c.R+c.G+c.B < 460:
		if c.G < 35 {
			return reforgeQualityPrimal // 太古
		}
		return reforgeQualityAncient // 远古
	case c.B > 100 && c.B > c.G && c.G > c.R:
		return reforgeQualitySacred // 神圣
	default:
		return reforgeQualityLegendary // 普通传奇
	}
}

func maxOf(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func absOf(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

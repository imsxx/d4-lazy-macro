package helper

import (
	"fmt"

	"github.com/imsxx/diablo-lazy-macro/internal/games"
	"github.com/imsxx/diablo-lazy-macro/internal/keys"
)

func init() { Register(D3QuickSalvage{}) }

// D3QuickSalvage 是暗黑3 的「快速分解」：对鼠标当前指向的装备点一次左键，
// 若游戏弹出确认框再按回车确认（对应 D3keyHelper 的 quickSalvageHelper）。
//
// 鼠标位置就是它的选择器：指哪件分哪件。为避免误点，运行前会校验
// ①确实处于铁匠铺分解页 ②鼠标位于背包区域，任一不满足就直接报错退出、不发送任何输入。
type D3QuickSalvage struct{}

func (D3QuickSalvage) ID() string   { return "d3.salvage.quick" }
func (D3QuickSalvage) Name() string { return "快速分解" }

func (D3QuickSalvage) Run(env *Env) error {
	at := env.Sampler()
	state, _ := games.D3SalvagePageOpen(at)
	if state != games.D3SalvageReady {
		return fmt.Errorf("不在铁匠铺的分解页：请先与铁匠对话并切到「分解」页")
	}

	relX, relY, err := env.MouseRel()
	if err != nil {
		return err
	}
	if zone := games.D3MouseZoneAt(relX, relY, env.WinW, env.WinH); zone != games.D3ZoneBag {
		return fmt.Errorf("请把鼠标移到背包内的装备上再执行（当前鼠标不在背包区域）")
	}
	env.debugf("快速分解：鼠标位于客户区 (%d,%d)，判定区域=背包", relX, relY)

	if err := env.Mouse.Tap(keys.VKLeftMouse); err != nil {
		return err
	}
	env.Sleep()

	// 分解高价值装备时游戏会弹确认框，此时回车确认。
	if games.D3DialogOnScreen(at) {
		env.debugf("检测到确认框，按回车确认")
		if err := env.Mouse.Tap(keys.VKEnter); err != nil {
			return err
		}
	}
	env.logf("已分解鼠标指向的装备")
	return nil
}

package helper

import (
	"fmt"
	"math"

	"github.com/imsxx/diablo-lazy-macro/internal/games"
	"github.com/imsxx/diablo-lazy-macro/internal/keys"
)

func init() {
	Register(D3Gamble{})
	Register(D3Loot{})
}

// 默认次数（与 D3keyHelper 的默认值一致）。
const (
	DefaultGambleTimes = 15
	DefaultLootTimes   = 20
)

// D3Gamble 是赌博助手：在商人处连按右键买装备（对应 D3keyHelper 的 gambleHelper）。
// Times <= 0 时使用默认次数。
type D3Gamble struct{ Times int }

func (D3Gamble) ID() string   { return "d3.gamble" }
func (D3Gamble) Name() string { return "赌博（连按右键）" }

func (g D3Gamble) Run(env *Env) error {
	if !games.D3GambleOpen(env.Sampler()) {
		return fmt.Errorf("不在赌博页面：请先与商人对话并打开赌博界面")
	}
	times := g.Times
	if times <= 0 {
		times = DefaultGambleTimes
	}
	for i := 0; i < times; i++ {
		if env.aborted() {
			return AbortError{Helper: "赌博"}
		}
		if err := env.Mouse.Tap(keys.VKRightMouse); err != nil {
			return err
		}
		env.SleepQuarter()
	}
	env.logf("赌博完成：右键 %d 次", times)
	return nil
}

// D3Loot 是一键拾取（对应 D3keyHelper 的 lootHelper）：
// 鼠标停在人物周围时连点左键（拾取脚下的一堆物品），否则只点一次。
// Times <= 0 时使用默认次数。
type D3Loot struct{ Times int }

func (D3Loot) ID() string   { return "d3.loot" }
func (D3Loot) Name() string { return "一键拾取（连点左键）" }

func (l D3Loot) Run(env *Env) error {
	relX, relY, err := env.MouseRel()
	if err != nil {
		return err
	}
	if !nearCharacter(env, relX, relY) {
		// 鼠标不在人物周围：只点一次，避免在别处乱点。
		return env.Mouse.Tap(keys.VKLeftMouse)
	}

	times := l.Times
	if times <= 0 {
		times = DefaultLootTimes
	}
	for i := 0; i < times; i++ {
		if env.aborted() {
			return AbortError{Helper: "拾取"}
		}
		if err := env.Mouse.Tap(keys.VKLeftMouse); err != nil {
			return err
		}
		env.SleepHalf()
	}
	env.logf("拾取完成：连点左键 %d 次", times)
	return nil
}

// nearCharacter 判断鼠标是否停在人物周围（以客户区中心为基准的范围）。
// 基准半径 600x500 是 3440x1440 下的值，按高度比例换算到当前分辨率。
func nearCharacter(env *Env, relX, relY int32) bool {
	scale := float64(env.WinH) / 1440.0
	if scale <= 0 {
		return false
	}
	return math.Abs(float64(relX)-float64(env.WinW)/2) < 600/scale &&
		math.Abs(float64(relY)-float64(env.WinH)/2) < 500/scale
}

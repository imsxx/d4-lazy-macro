package helper

import (
	"testing"
	"time"

	"github.com/imsxx/diablo-lazy-macro/internal/games"
	"github.com/imsxx/diablo-lazy-macro/internal/keys"
)

// kanaiReforgeColors 是一组能让界面判定为「卡奈魔盒 · 重铸页」的颜色。
func kanaiReforgeColors() map[[2]int]games.D3Color {
	return map[[2]int]games.D3Color{
		{353, 85}:  {R: 20, G: 20, B: 20},
		{278, 147}: {R: 150, G: 10, B: 10},
		{330, 140}: {R: 30, G: 60, B: 62},
		{788, 428}: {R: 50, G: 100, B: 240},
		{810, 429}: {R: 50, G: 100, B: 240},
	}
}

func TestReforgeRequiresReforgePage(t *testing.T) {
	env, m := newEnv(nil, 2800, 800)
	if err := (D3Reforge{}).Run(env); err == nil {
		t.Fatal("不在重铸界面时应报错")
	}
	if len(m.taps) != 0 || len(m.moves) != 0 {
		t.Fatalf("不应发送任何输入，实际 taps=%v moves=%v", m.taps, m.moves)
	}
}

func TestReforgeRequiresMouseInBag(t *testing.T) {
	env, m := newEnv(kanaiReforgeColors(), 1000, 800)
	if err := (D3Reforge{}).Run(env); err == nil {
		t.Fatal("鼠标不在背包区时应报错")
	}
	if len(m.taps) != 0 {
		t.Fatalf("不应点击，实际 %v", m.taps)
	}
}

func TestReforgeSinglePassRestoresMouse(t *testing.T) {
	env, m := newEnv(kanaiReforgeColors(), 2800, 800)
	env.Delay = time.Millisecond
	if err := (D3Reforge{Target: 0}).Run(env); err != nil {
		t.Fatalf("重铸失败：%v", err)
	}
	// 一次重铸 = 右键放入装备 + 依次点击「放入材料 / 转化 / 上一页 / 下一页」
	if len(m.taps) != 5 || m.taps[0] != keys.VKRightMouse {
		t.Fatalf("输入序列不符：%v", m.taps)
	}
	for _, vk := range m.taps[1:] {
		if vk != keys.VKLeftMouse {
			t.Fatalf("除首次右键外都应是左键点击，实际 %v", m.taps)
		}
	}
	// 收尾必须把光标还给玩家
	if len(m.moves) == 0 || m.moves[len(m.moves)-1] != [2]int32{2800, 800} {
		t.Errorf("收尾未还原鼠标位置：%v", m.moves)
	}
}

func TestReforgeQualityClassification(t *testing.T) {
	// 品质判据照搬脚本，这里锁住分支边界，避免日后手滑改错。
	cases := []struct {
		name string
		c    games.D3Color
		want int
	}{
		{"太古（G 很低）", games.D3Color{R: 100, G: 20, B: 10}, reforgeQualityPrimal},
		{"远古（同为低饱和但 G 较高）", games.D3Color{R: 100, G: 60, B: 20}, reforgeQualityAncient},
		{"神圣（蓝占优）", games.D3Color{R: 20, G: 60, B: 150}, reforgeQualitySacred},
		{"普通传奇（灰色）", games.D3Color{R: 200, G: 200, B: 200}, reforgeQualityLegendary},
	}
	for _, c := range cases {
		if got := reforgeQuality(c.c); got != c.want {
			t.Errorf("%s：got %d，want %d", c.name, got, c.want)
		}
	}
}

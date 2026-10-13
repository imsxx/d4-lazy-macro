package helper

import (
	"testing"
	"time"

	"github.com/imsxx/diablo-lazy-macro/internal/games"
	"github.com/imsxx/diablo-lazy-macro/internal/keys"
)

// gamblePageColors 是一组能让界面判定为「赌博页」的颜色。
func gamblePageColors() map[[2]int]games.D3Color {
	return map[[2]int]games.D3Color{
		{320, 96}:  {R: 100, G: 50, B: 150},
		{351, 100}: {R: 200, G: 200, B: 10},
		{194, 67}:  {R: 0, G: 0, B: 0},
		{147, 94}:  {R: 0, G: 0, B: 0},
	}
}

func TestGambleRequiresGamblePage(t *testing.T) {
	env, m := newEnv(nil, 2800, 800)
	if err := (D3Gamble{}).Run(env); err == nil {
		t.Fatal("不在赌博页时应报错")
	}
	if len(m.taps) != 0 {
		t.Fatalf("不应点击，实际 %v", m.taps)
	}
}

func TestGambleRightClicks(t *testing.T) {
	env, m := newEnv(gamblePageColors(), 300, 800)
	env.Delay = time.Millisecond
	if err := (D3Gamble{Times: 3}).Run(env); err != nil {
		t.Fatalf("赌博失败：%v", err)
	}
	if len(m.taps) != 3 {
		t.Fatalf("应右键 3 次，实际 %v", m.taps)
	}
	for _, vk := range m.taps {
		if vk != keys.VKRightMouse {
			t.Errorf("赌博应使用右键，实际 %v", m.taps)
			break
		}
	}
}

func TestLootClicksOnceWhenMouseAwayFromCharacter(t *testing.T) {
	// 鼠标远离人物中心时只点一次，避免在界面别处乱点。
	env, m := newEnv(nil, 200, 200)
	if err := (D3Loot{Times: 5}).Run(env); err != nil {
		t.Fatalf("拾取失败：%v", err)
	}
	if len(m.taps) != 1 {
		t.Fatalf("鼠标不在人物周围应只点一次，实际 %v", m.taps)
	}
}

func TestLootBurstsNearCharacter(t *testing.T) {
	// 鼠标停在人物周围（客户区中心）时连点。
	env, m := newEnv(nil, 1720, 720)
	env.Delay = time.Millisecond
	if err := (D3Loot{Times: 4}).Run(env); err != nil {
		t.Fatalf("拾取失败：%v", err)
	}
	if len(m.taps) != 4 {
		t.Fatalf("应连点 4 次，实际 %v", m.taps)
	}
	for _, vk := range m.taps {
		if vk != keys.VKLeftMouse {
			t.Errorf("拾取应使用左键，实际 %v", m.taps)
			break
		}
	}
}

func TestGambleAndLootRegistered(t *testing.T) {
	want := map[string]bool{"d3.gamble": false, "d3.loot": false}
	for _, h := range All() {
		if _, ok := want[h.ID()]; ok {
			want[h.ID()] = true
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("助手 %s 未注册", id)
		}
	}
}

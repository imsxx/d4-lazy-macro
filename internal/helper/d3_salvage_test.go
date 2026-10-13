package helper

import (
	"strings"
	"testing"
	"time"

	"github.com/imsxx/diablo-lazy-macro/internal/games"
	"github.com/imsxx/diablo-lazy-macro/internal/keys"
)

type fakeMouse struct {
	x, y  int32
	moves [][2]int32
	taps  []uint16
}

func (m *fakeMouse) Position() (int32, int32, error) { return m.x, m.y, nil }
func (m *fakeMouse) Move(x, y int32) error {
	m.moves = append(m.moves, [2]int32{x, y})
	m.x, m.y = x, y
	return nil
}
func (m *fakeMouse) Tap(vk uint16) error {
	m.taps = append(m.taps, vk)
	return nil
}

// fakePixel 按基准坐标返回预设颜色。测试统一使用参考分辨率（3440x1440）与原点 (0,0)，
// 此时三种横向锚点换算后都等于基准值，因此可以直接用基准坐标查表。
type fakePixel struct {
	colors map[[2]int]games.D3Color
}

func (p fakePixel) ClientRect(uintptr) (int32, int32, int32, int32, bool) {
	return 3440, 1440, 0, 0, true
}

func (p fakePixel) Pixel(x, y int32) (uint8, uint8, uint8, bool) {
	c, ok := p.colors[[2]int{int(x), int(y)}]
	if !ok {
		return 0, 0, 0, false
	}
	return uint8(c.R), uint8(c.G), uint8(c.B), true
}

// salvageReadyColors 是一组能让界面判定为「铁匠铺分解页」的颜色。
func salvageReadyColors() map[[2]int]games.D3Color {
	return map[[2]int]games.D3Color{
		{339, 80}:   {R: 50, G: 100, B: 200},
		{388, 86}:   {R: 30, G: 80, B: 150},
		{351, 107}:  {R: 200, G: 200, B: 10},
		{673, 1040}: {R: 200, G: 5, B: 5},
		{203, 337}:  {R: 10, G: 10, B: 10},
		{335, 371}:  {R: 200, G: 200, B: 200},
		{424, 371}:  {R: 10, G: 50, B: 200},
		{514, 371}:  {R: 200, G: 100, B: 10},
	}
}

func newEnv(colors map[[2]int]games.D3Color, mouseX, mouseY int32) (*Env, *fakeMouse) {
	m := &fakeMouse{x: mouseX, y: mouseY}
	return &Env{
		WinW: 3440, WinH: 1440,
		Pixel: fakePixel{colors: colors},
		Mouse: m,
		Delay: time.Millisecond, // 单测不需要真等待
	}, m
}

func TestQuickSalvageRequiresSalvagePage(t *testing.T) {
	// 不在分解页时必须直接报错，且**不发送任何输入**——否则会把点击落到游戏里别的地方。
	env, m := newEnv(nil, 2800, 800)
	err := (D3QuickSalvage{}).Run(env)
	if err == nil {
		t.Fatal("不在分解页时应报错")
	}
	if len(m.taps) != 0 || len(m.moves) != 0 {
		t.Fatalf("不应发送任何输入，实际 taps=%v moves=%v", m.taps, m.moves)
	}
	if !strings.Contains(err.Error(), "分解页") {
		t.Errorf("错误信息应提示分解页，实际：%v", err)
	}
}

func TestQuickSalvageRequiresMouseInBag(t *testing.T) {
	// 界面正确但鼠标不在背包区：同样必须停手。
	env, m := newEnv(salvageReadyColors(), 1000, 800) // 左侧空白，既非背包也非储物箱
	err := (D3QuickSalvage{}).Run(env)
	if err == nil {
		t.Fatal("鼠标不在背包区时应报错")
	}
	if len(m.taps) != 0 {
		t.Fatalf("不应点击，实际 taps=%v", m.taps)
	}
}

func TestQuickSalvageClicksAndConfirmsDialog(t *testing.T) {
	// 正常路径：点一次左键；出现确认框时再按回车。
	colors := salvageReadyColors()
	colors[[2]int{1655, 500}] = games.D3Color{R: 100, G: 10, B: 2}
	colors[[2]int{1800, 500}] = games.D3Color{R: 100, G: 10, B: 2}
	env, m := newEnv(colors, 2800, 800)
	if err := (D3QuickSalvage{}).Run(env); err != nil {
		t.Fatalf("分解失败：%v", err)
	}
	if len(m.taps) != 2 || m.taps[0] != keys.VKLeftMouse || m.taps[1] != keys.VKEnter {
		t.Errorf("应先点左键再回车确认，实际 taps=%v", m.taps)
	}
}

func TestQuickSalvageWithoutDialog(t *testing.T) {
	// 没有确认框时只点一次左键，不应多发回车。
	env, m := newEnv(salvageReadyColors(), 2800, 800)
	if err := (D3QuickSalvage{}).Run(env); err != nil {
		t.Fatalf("分解失败：%v", err)
	}
	if len(m.taps) != 1 || m.taps[0] != keys.VKLeftMouse {
		t.Errorf("应只点一次左键，实际 taps=%v", m.taps)
	}
}

func TestQuickSalvageRegistered(t *testing.T) {
	for _, h := range All() {
		if h.ID() == "d3.salvage.quick" {
			if h.Name() == "" {
				t.Error("助手应有显示名")
			}
			return
		}
	}
	t.Error("快速分解助手未注册")
}

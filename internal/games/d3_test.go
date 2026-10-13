package games

import "testing"

// fakePixel 是 PixelSource 的测试替身。
type fakePixel struct {
	w, h, ox, oy int32
	rectOK       bool
	pixels       map[[2]int32][3]uint8
}

func (f fakePixel) ClientRect(uintptr) (int32, int32, int32, int32, bool) {
	return f.w, f.h, f.ox, f.oy, f.rectOK
}

func (f fakePixel) Pixel(x, y int32) (uint8, uint8, uint8, bool) {
	if c, ok := f.pixels[[2]int32{x, y}]; ok {
		return c[0], c[1], c[2], true
	}
	return 0, 0, 0, false
}

func TestD3BuffSamplePointReferenceResolution(t *testing.T) {
	// 3440x1440、原点 (0,0)：scale = 1，槽位 0 的采样点应为
	// X = 1288 + 0.05*63 = 1291.15 -> 1291；Y = 1328。
	sx, sy, ok := d3BuffSamplePoint(0, 3440, 1440, 0, 0, d3BuffSamplePercent)
	if !ok {
		t.Fatal("参考分辨率下应能算出采样点")
	}
	if sx != 1291 || sy != 1328 {
		t.Fatalf("参考分辨率采样点不符：got (%d,%d)，want (1291,1328)", sx, sy)
	}
}

func TestD3BuffSamplePointScalesWithHeight(t *testing.T) {
	// 高度翻倍（1440 -> 2880）：Y 翻倍，X 偏移量随 scale 放大。
	sx, sy, ok := d3BuffSamplePoint(0, 6880, 2880, 0, 0, d3BuffSamplePercent)
	if !ok {
		t.Fatal("缩放后应能算出采样点")
	}
	if sy != 2656 {
		t.Errorf("Y 应随高度等比缩放：got %d，want 2656", sy)
	}
	// relX = 6880/2 - (1720-1288-3.15)*2 = 3440 - 857.7 = 2582.3 -> 2582
	if sx != 2582 {
		t.Errorf("X 缩放不符：got %d，want 2582", sx)
	}
}

func TestD3BuffSamplePointAppliesWindowOrigin(t *testing.T) {
	// 窗口位于副屏时，采样点必须叠加客户端原点。
	sx, sy, ok := d3BuffSamplePoint(0, 3440, 1440, 100, -50, d3BuffSamplePercent)
	if !ok {
		t.Fatal("应能算出采样点")
	}
	if sx != 1391 || sy != 1278 {
		t.Fatalf("原点偏移未生效：got (%d,%d)，want (1391,1278)", sx, sy)
	}
}

func TestD3BuffSamplePointRejectsBadSlot(t *testing.T) {
	if _, _, ok := d3BuffSamplePoint(-1, 3440, 1440, 0, 0, d3BuffSamplePercent); ok {
		t.Error("负数槽位应被拒绝")
	}
	if _, _, ok := d3BuffSamplePoint(d3SlotCount, 3440, 1440, 0, 0, d3BuffSamplePercent); ok {
		t.Error("越界槽位应被拒绝")
	}
}

func TestD3BuffActiveThreshold(t *testing.T) {
	// 采样点 (1291,1328)：绿色分量 >= 95 视为 Buff 仍在生效。
	cases := []struct {
		name string
		g    uint8
		want bool
	}{
		{"发光环未结束", 120, true},
		{"恰好达到阈值", 95, true},
		{"阈值下方一点", 94, false},
		{"Buff 已断", 10, false},
	}
	for _, c := range cases {
		px := fakePixel{
			w: 3440, h: 1440, rectOK: true,
			pixels: map[[2]int32][3]uint8{{1291, 1328}: {0, c.g, 0}},
		}
		if got := (d3Adapter{}).BuffActive(0, 0, px); got != c.want {
			t.Errorf("%s：BuffActive = %v，want %v（G=%d）", c.name, got, c.want, c.g)
		}
	}
}

func TestD3BuffActiveFailSafe(t *testing.T) {
	if (d3Adapter{}).BuffActive(0, 0, nil) {
		t.Error("像素源为空时应返回 false")
	}
	badRect := fakePixel{rectOK: false}
	if (d3Adapter{}).BuffActive(0, 0, badRect) {
		t.Error("窗口几何不可用时应返回 false")
	}
	noPixel := fakePixel{w: 3440, h: 1440, rectOK: true}
	if (d3Adapter{}).BuffActive(0, 0, noPixel) {
		t.Error("取色失败时应返回 false")
	}
}

func TestOtherGamesHaveNoBuffGuard(t *testing.T) {
	px := fakePixel{
		w: 3440, h: 1440, rectOK: true,
		pixels: map[[2]int32][3]uint8{{1291, 1328}: {0, 255, 0}},
	}
	for _, id := range []string{"d4", "d2r"} {
		a, ok := For(id)
		if !ok {
			t.Fatalf("适配器 %s 未注册", id)
		}
		if a.BuffActive(0, 0, px) {
			t.Errorf("%s 暂无 Buff 守卫，应恒返回 false", id)
		}
	}
}

func TestAdapterRegistryConsistency(t *testing.T) {
	if len(All()) != 3 {
		t.Fatalf("预期注册 3 款游戏，实际 %d", len(All()))
	}
	if Default().ID != "d4" {
		t.Errorf("默认游戏应为 d4，实际 %s", Default().ID)
	}
	for _, g := range All() {
		a, ok := For(g.ID)
		if !ok {
			t.Fatalf("All() 中的 %s 无法用 For 取得", g.ID)
		}
		if a.Name() != g.Name || a.DefaultProcess() != g.DefaultProcess {
			t.Errorf("%s 的适配器与 Game 视图不一致", g.ID)
		}
		if len(a.Slots()) != 6 {
			t.Errorf("%s 的槽位数量应为 6，实际 %d", g.ID, len(a.Slots()))
		}
	}
	if _, ok := For("d2"); ok {
		t.Error("未支持的游戏不应返回适配器")
	}
}

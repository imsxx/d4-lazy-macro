package games

import "testing"

func TestD3InventorySlotXYReferenceResolution(t *testing.T) {
	// 3440x1440（scale=1）下的期望值直接来自 D3keyHelper 的坐标表，
	// 用于确认移植时没有算错索引或方向（背包从右边缘倒推、魔盒从左正向）。
	cases := []struct {
		name string
		id   int
		zone D3Zone
		want D3SlotPoint
	}{
		{
			// CenterX = 3440 - (3440-2753-32) = 2785；CenterY = 747+31.5 = 778.5 -> 779
			name: "背包第 1 格（左上角）", id: 1, zone: D3ZoneBag,
			want: D3SlotPoint{CenterX: 2785, CenterY: 779, OuterX: 2753, OuterY: 747},
		},
		{
			name: "背包第 10 格（右上角）", id: 10, zone: D3ZoneBag,
			want: D3SlotPoint{CenterX: 3389, CenterY: 779, OuterX: 3357, OuterY: 747},
		},
		{
			// 第 11 格换行：第 2 行第 1 列
			name: "背包第 11 格（第二行首）", id: 11, zone: D3ZoneBag,
			want: D3SlotPoint{CenterX: 2785, CenterY: 845, OuterX: 2753, OuterY: 813},
		},
		{
			// 魔盒在左侧：CenterX = 242+32 = 274
			name: "魔盒第 1 格", id: 1, zone: D3ZoneKanai,
			want: D3SlotPoint{CenterX: 274, CenterY: 535, OuterX: 242, OuterY: 503},
		},
		{
			name: "魔盒第 4 格（第二行首）", id: 4, zone: D3ZoneKanai,
			want: D3SlotPoint{CenterX: 274, CenterY: 611, OuterX: 242, OuterY: 579},
		},
	}
	for _, c := range cases {
		got, ok := D3InventorySlotXY(c.id, c.zone, 3440, 1440)
		if !ok {
			t.Fatalf("%s：计算失败", c.name)
		}
		if got != c.want {
			t.Errorf("%s：got %+v，want %+v", c.name, got, c.want)
		}
	}
}

func TestD3InventorySlotXYScalesWithHeight(t *testing.T) {
	// 实机客户区 1817x1025（scale = 1025/1440 ≈ 0.711806）
	got, ok := D3InventorySlotXY(1, D3ZoneBag, 1817, 1025)
	if !ok {
		t.Fatal("计算失败")
	}
	// CenterX = 1817 - 655*0.711806 = 1350.77 -> 1351；CenterY = 778.5*0.711806 = 554.14 -> 554
	if got.CenterX != 1351 || got.CenterY != 554 {
		t.Errorf("缩放结果不符：got (%d,%d)，want (1351,554)", got.CenterX, got.CenterY)
	}
}

func TestD3InventorySlotXYRejectsOutOfRange(t *testing.T) {
	// 背包 10x6=60 格、魔盒 3x3=9 格：越界必须拒绝，否则会点到界面之外。
	for _, c := range []struct {
		name string
		id   int
		zone D3Zone
		w, h int32
	}{
		{"背包第 61 格", 61, D3ZoneBag, 3440, 1440},
		{"魔盒第 10 格", 10, D3ZoneKanai, 3440, 1440},
		{"编号 0", 0, D3ZoneBag, 3440, 1440},
		{"未知区域", 1, D3Zone(9), 3440, 1440},
		{"宽度为 0", 1, D3ZoneBag, 0, 1440},
		{"高度为 0", 1, D3ZoneBag, 3440, 0},
	} {
		if _, ok := D3InventorySlotXY(c.id, c.zone, c.w, c.h); ok {
			t.Errorf("%s 应被拒绝", c.name)
		}
	}
}

func TestD3SalvageButtonXY(t *testing.T) {
	// 基准分辨率下应等于基准值本身。
	if x, y, ok := D3SalvageButtonXY(D3TierLegendary, D3AnchorCenter, 1440); !ok || x != 221 || y != 388 {
		t.Errorf("中心锚点不符：got (%d,%d) ok=%v，want (221,388)", x, y, ok)
	}
	if x, y, ok := D3SalvageButtonXY(D3TierRare, D3AnchorEdge, 1440); !ok || x != 514 || y != 371 {
		t.Errorf("边缘锚点不符：got (%d,%d) ok=%v，want (514,371)", x, y, ok)
	}
	if _, _, ok := D3SalvageButtonXY(D3SalvageTier(9), D3AnchorCenter, 1440); ok {
		t.Error("越界品质应被拒绝")
	}
	if _, _, ok := D3SalvageButtonXY(D3TierWhite, D3SalvageAnchor(9), 1440); ok {
		t.Error("未知锚点应被拒绝")
	}
	if _, _, ok := D3SalvageButtonXY(D3TierWhite, D3AnchorCenter, 0); ok {
		t.Error("窗口高度为 0 应被拒绝")
	}
}

func TestD3KanaiButtonXY(t *testing.T) {
	b, ok := D3KanaiButtonXY(1440)
	if !ok {
		t.Fatal("计算失败")
	}
	if b.Transmute != (D3Point{X: 320, Y: 1105}) {
		t.Errorf("转化按钮不符：%+v", b.Transmute)
	}
	if b.InsertItem != (D3Point{X: 955, Y: 1115}) {
		t.Errorf("放入材料按钮不符：%+v", b.InsertItem)
	}
	if b.PrevPage != (D3Point{X: 777, Y: 1117}) || b.NextPage != (D3Point{X: 1135, Y: 1117}) {
		t.Errorf("翻页按钮不符：%+v / %+v", b.PrevPage, b.NextPage)
	}
	if _, ok := D3KanaiButtonXY(0); ok {
		t.Error("窗口高度为 0 应被拒绝")
	}
}

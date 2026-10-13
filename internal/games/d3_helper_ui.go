package games

import "math"

// 本文件是暗黑3 助手功能的界面状态识别，1:1 移植自 D3keyHelper 的
// isSalvagePageOpen / isKanaiCubeOpen / isGambleOpen / isInventoryOpen /
// isStashOpen / isDialogBoXOnScreen。
//
// 与它一致：判据全部是「若干个固定点的颜色分量大小关系」，只用单点取色，不做找图找色。
// 坐标以 3440x1440 为基准，按 D3H/1440 等比缩放。

// D3Color 是 RGB 三元组，分量顺序与 D3keyHelper 的 splitRGB 一致。
type D3Color struct{ R, G, B int }

// D3AnchorX 决定横坐标如何从基准值换算到当前分辨率。
type D3AnchorX int

const (
	D3AnchorLeft   D3AnchorX = iota // 从左边缘正向：x = baseX * scale
	D3AnchorRight                   // 从右边缘倒推：x = winW - (3440-baseX) * scale（背包等右侧元素）
	D3AnchorCenter                  // 相对屏幕中线：x = winW/2 + (baseX-1720) * scale（居中对话框）
)

// D3ColorAt 按基准坐标取色，返回 false 表示取色失败。
// 判据里任何一点取不到色都应视为「界面不成立」，因此调用方必须检查返回值。
type D3ColorAt func(baseX, baseY float64, anchor D3AnchorX) (D3Color, bool)

// D3ColorSampler 把像素源适配成 D3ColorAt：负责基准坐标 → 客户区相对坐标 → 屏幕绝对坐标的换算。
func D3ColorSampler(px PixelSource, winW, winH, originX, originY int32) D3ColorAt {
	scale := float64(winH) / d3RefHeight
	return func(baseX, baseY float64, anchor D3AnchorX) (D3Color, bool) {
		var relX float64
		switch anchor {
		case D3AnchorRight:
			relX = float64(winW) - (d3RefWidth-baseX)*scale
		case D3AnchorCenter:
			relX = float64(winW)/2 + (baseX-d3RefWidth/2)*scale
		default:
			relX = baseX * scale
		}
		r, g, b, ok := px.Pixel(originX+int32(math.Round(relX)), originY+int32(math.Round(baseY*scale)))
		if !ok {
			return D3Color{}, false
		}
		return D3Color{R: int(r), G: int(g), B: int(b)}, true
	}
}

// D3ClientLang 区分客户端语言：非英文客户端的部分界面元素整体上移，
// D3keyHelper 用窗口标题是否恰为 "Diablo III" 来区分。
type D3ClientLang int

const (
	D3ClientEnglish D3ClientLang = iota // 标题恰为 "Diablo III"
	D3ClientOther                       // 其它语言（含中文）
)

// 非英文客户端的 Y 轴补偿（来自 D3keyHelper）。
const (
	d3NonEnglishUpgradeYOffset = -22.0
	d3NonEnglishConvertYOffset = -43.0
)

// D3SalvageState 是铁匠铺界面状态。
type D3SalvageState int

const (
	D3SalvageClosed D3SalvageState = iota // 不在铁匠页
	D3SalvageSmithy                       // 在铁匠页，但未切到分解页
	D3SalvageReady                        // 在分解页，可以分解
)

// D3KanaiState 是卡奈魔盒界面状态，取值与 D3keyHelper 的返回值一一对应。
type D3KanaiState int

const (
	D3KanaiClosed D3KanaiState = iota // 未打开
	D3KanaiUnknown                    // 已打开，但不在已知子页面
	D3KanaiReforge                    // 重铸
	D3KanaiUpgrade                    // 升级
	D3KanaiConvert                    // 材料转化
)

// D3SalvageButtons 是 4 个品质按钮边缘的采样颜色：用于判断哪些品质有物品可分解。
type D3SalvageButtons struct {
	Legendary D3Color
	White     D3Color
	Blue      D3Color
	Rare      D3Color
}

// D3SalvagePageOpen 判断铁匠铺界面状态；处于分解页时一并返回按钮采样颜色。
func D3SalvagePageOpen(at D3ColorAt) (D3SalvageState, *D3SalvageButtons) {
	if at == nil {
		return D3SalvageClosed, nil
	}
	c1, ok1 := at(339, 80, D3AnchorLeft)
	c2, ok2 := at(351, 107, D3AnchorLeft)
	c3, ok3 := at(388, 86, D3AnchorLeft)
	c4, ok4 := at(673, 1040, D3AnchorLeft)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return D3SalvageClosed, nil
	}
	if !(c1.B > c1.G && c1.G > c1.R && c1.B > 170 && c1.B-c1.R > 80 &&
		c3.B > c3.G && c3.G > c3.R && c3.B > 110 &&
		c2.R+c2.G > 350 &&
		c4.R > 50 && c4.G < 15 && c4.B < 15) {
		return D3SalvageClosed, nil
	}

	leg, ok1 := at(203, 337, D3AnchorLeft)
	white, ok2 := at(335, 371, D3AnchorLeft)
	blue, ok3 := at(424, 371, D3AnchorLeft)
	rare, ok4 := at(514, 371, D3AnchorLeft)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return D3SalvageSmithy, nil
	}
	if blue.B > blue.G && blue.G > blue.R && rare.B < 20 && rare.R > rare.G && rare.G > rare.B {
		return D3SalvageReady, &D3SalvageButtons{Legendary: leg, White: white, Blue: blue, Rare: rare}
	}
	return D3SalvageSmithy, nil
}

// D3KanaiCubeOpen 判断卡奈魔盒界面状态；lang 用于补偿非英文客户端的 Y 轴偏移。
func D3KanaiCubeOpen(lang D3ClientLang, at D3ColorAt) D3KanaiState {
	if at == nil {
		return D3KanaiClosed
	}
	c1, ok1 := at(353, 85, D3AnchorLeft)
	c2, ok2 := at(278, 147, D3AnchorLeft)
	c3, ok3 := at(330, 140, D3AnchorLeft)
	if !ok1 || !ok2 || !ok3 {
		return D3KanaiClosed
	}
	if !(c1.R < 50 && c1.G < 40 && c1.B < 35 &&
		c2.R > 100 && c2.G < 30 && c2.B < 30 &&
		absInt(c3.B-c3.G) <= 8 && c3.R <= 55 && c3.R < c3.G && c3.R < c3.B) {
		return D3KanaiClosed
	}

	// 重铸：两个高亮蓝点
	cc1, ok1 := at(788, 428, D3AnchorLeft)
	cc2, ok2 := at(810, 429, D3AnchorLeft)
	if ok1 && ok2 && cc1.B > 230 && cc2.B > 230 &&
		cc1.B > cc1.G && cc2.B > cc2.G && cc1.G > cc1.R && cc2.G > cc2.R {
		return D3KanaiReforge
	}

	// 升级：非英文客户端整体上移
	upY := 0.0
	if lang == D3ClientOther {
		upY = d3NonEnglishUpgradeYOffset
	}
	u1, ok1 := at(799, 406+upY, D3AnchorLeft)
	u2, ok2 := at(795, 592+upY, D3AnchorLeft)
	if ok1 && ok2 && u1.R+u1.G+u1.B > 550 && u1.R > u1.B && u2.R+u2.G > 400 && u2.R > u2.B {
		return D3KanaiUpgrade
	}

	// 材料转化：非英文客户端上移更多
	cvY := 0.0
	if lang == D3ClientOther {
		cvY = d3NonEnglishConvertYOffset
	}
	v1, ok1 := at(799, 365+cvY, D3AnchorLeft)
	if ok1 && v1.R+v1.G+v1.B > 600 && v1.R > v1.G && v1.G > v1.B && v1.B > 110 && v1.B < 200 {
		return D3KanaiConvert
	}
	return D3KanaiUnknown
}

// D3GambleOpen 判断是否处于赌博（商人）页面。
func D3GambleOpen(at D3ColorAt) bool {
	if at == nil {
		return false
	}
	c1, ok1 := at(320, 96, D3AnchorLeft)
	c2, ok2 := at(351, 100, D3AnchorLeft)
	c4, ok4 := at(194, 67, D3AnchorLeft)
	c5, ok5 := at(147, 94, D3AnchorLeft)
	if !ok1 || !ok2 || !ok4 || !ok5 {
		return false
	}
	return c1.B > c1.R && c1.R > c1.G && c1.B > 130 &&
		c2.R+c2.G > 330 &&
		c4.R+c4.G+c4.B+c5.R+c5.G+c5.B < 10
}

// D3InventoryOpen 判断是否处于物品栏（背包）页面。背包在屏幕右侧，取色点从右边缘倒推。
func D3InventoryOpen(at D3ColorAt) bool {
	if at == nil {
		return false
	}
	c1, ok1 := at(3086, 108, D3AnchorRight)
	c2, ok2 := at(3010, 147, D3AnchorRight)
	c3, ok3 := at(3425, 142, D3AnchorRight)
	c4, ok4 := at(3117, 84, D3AnchorRight)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return false
	}
	return c1.R+c1.G > 240 &&
		c2.R > 115 && c2.G < 30 && c2.B < 30 &&
		absInt(c3.R-c3.G) <= 10 && c3.B < 40 &&
		c4.B > c4.G+60 && c4.G > c4.R
}

// D3StashOpen 判断是否处于储物箱页面。
func D3StashOpen(at D3ColorAt) bool {
	if at == nil {
		return false
	}
	c1, ok1 := at(282, 147, D3AnchorLeft)
	c2, ok2 := at(382, 77, D3AnchorLeft)
	c3, ok3 := at(299, 82, D3AnchorLeft)
	if !ok1 || !ok2 || !ok3 {
		return false
	}
	return c1.R > 100 && c1.R > c1.G+80 && absInt(c1.G-c1.B) < 10 &&
		c2.G > c2.B && c2.B > c2.R && c2.G-c2.R > 80 &&
		c3.R > c3.G && c3.G > c3.B && c3.B < 40
}

// D3DialogOnScreen 判断屏幕中央是否出现对话框（确认框等）。
// 出现时助手必须停手，否则会误点确认按钮。
func D3DialogOnScreen(at D3ColorAt) bool {
	if at == nil {
		return false
	}
	// 这两点相对屏幕中线定位，因此用 D3AnchorCenter。
	c1, ok1 := at(1655, 500, D3AnchorCenter)
	c2, ok2 := at(1800, 500, D3AnchorCenter)
	if !ok1 || !ok2 {
		return false
	}
	red := func(c D3Color) bool {
		return c.R > c.G && c.G > c.B && c.B < 5 && c.G < 15 && c.R > 25
	}
	return red(c1) && red(c2)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

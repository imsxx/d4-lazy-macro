package games

import "math"

// 本文件是暗黑3 助手功能的坐标知识，1:1 移植自 D3keyHelper。
//
// 坐标系与它保持一致：所有数值都是「窗口客户区相对坐标」
//（对应脚本里的 CoordMode, Pixel/Mouse, Client）。需要屏幕绝对坐标时，
// 由调用方叠加客户区左上角的屏幕位置（win32.Screen.ClientRect 的后两个返回值）。
//
// 基准分辨率同为 3440x1440，一律按 D3H/1440 等比缩放；背包位于屏幕右侧，
// 因此它的横坐标是从右边缘倒推的（与脚本相同）。

// D3Zone 标识格子所属区域。
type D3Zone int

const (
	D3ZoneBag   D3Zone = iota // 玩家背包：10 列 x 6 行
	D3ZoneKanai               // 卡奈魔盒：3 列 x 3 行
)

// D3Point 是一个客户区相对坐标点。
type D3Point struct{ X, Y int32 }

// 格子尺寸（3440x1440 基准）：Inner 是内部可用区，用于点击物品的中心。
const (
	d3SlotInnerW = 64.0
	d3SlotInnerH = 63.0
)

// 各区域的格子基准坐标（3440x1440 基准）。
var (
	d3BagColumnX   = [10]float64{2753, 2820, 2887, 2954, 3021, 3089, 3156, 3223, 3290, 3357}
	d3BagRowY      = [6]float64{747, 813, 880, 946, 1013, 1079}
	d3KanaiColumnX = [3]float64{242, 318, 394}
	d3KanaiRowY    = [3]float64{503, 579, 655}
)

// D3SlotPoint 是一个格子的坐标：Center 为格子中心（点物品用），Outer 为左上角（点空格用）。
type D3SlotPoint struct {
	CenterX, CenterY int32
	OuterX, OuterY   int32
}

// D3InventorySlotXY 计算背包/魔盒中第 id 个格子的坐标，id 从 1 开始、按行优先编号。
func D3InventorySlotXY(id int, zone D3Zone, winW, winH int32) (D3SlotPoint, bool) {
	if id < 1 || winW <= 0 || winH <= 0 {
		return D3SlotPoint{}, false
	}
	var (
		cols      int
		baseX     float64
		baseY     float64
		fromRight bool
	)
	switch zone {
	case D3ZoneBag:
		cols, fromRight = len(d3BagColumnX), true
		if id > cols*len(d3BagRowY) {
			return D3SlotPoint{}, false
		}
		baseX, baseY = d3BagColumnX[(id-1)%cols], d3BagRowY[(id-1)/cols]
	case D3ZoneKanai:
		cols = len(d3KanaiColumnX)
		if id > cols*len(d3KanaiRowY) {
			return D3SlotPoint{}, false
		}
		baseX, baseY = d3KanaiColumnX[(id-1)%cols], d3KanaiRowY[(id-1)/cols]
	default:
		return D3SlotPoint{}, false
	}

	scale := float64(winH) / d3RefHeight
	x := func(offset float64) int32 {
		if fromRight {
			return int32(math.Round(float64(winW) - (d3RefWidth-baseX-offset)*scale))
		}
		return int32(math.Round((baseX + offset) * scale))
	}
	return D3SlotPoint{
		CenterX: x(d3SlotInnerW / 2),
		CenterY: int32(math.Round((baseY + d3SlotInnerH/2) * scale)),
		OuterX:  x(0),
		OuterY:  int32(math.Round(baseY * scale)),
	}, true
}

// D3SalvageTier 是铁匠铺分解的品质档位（顺序与脚本的按钮排列一致）。
type D3SalvageTier int

const (
	D3TierLegendary D3SalvageTier = iota // 传说 / 暗金
	D3TierWhite                          // 白 / 灰
	D3TierBlue                           // 蓝（魔法）
	D3TierRare                           // 稀有 / 黄
)

// D3SalvageAnchor 决定取的是按钮中心还是边缘。
type D3SalvageAnchor int

const (
	D3AnchorCenter D3SalvageAnchor = iota // 点击用：按钮中心
	D3AnchorEdge                          // 取色用：按钮边缘（避开中心高光）
)

// 各品质按钮的基准坐标（3440x1440）。
var (
	d3SalvageCenter = [4][2]float64{{221, 388}, {335, 388}, {424, 388}, {514, 388}}
	d3SalvageEdge   = [4][2]float64{{203, 337}, {335, 371}, {424, 371}, {514, 371}}
)

// D3SalvageButtonXY 返回指定品质按钮的坐标（客户区相对）。
func D3SalvageButtonXY(tier D3SalvageTier, anchor D3SalvageAnchor, winH int32) (x, y int32, ok bool) {
	if winH <= 0 || tier < 0 || int(tier) >= len(d3SalvageCenter) {
		return 0, 0, false
	}
	var src [2]float64
	switch anchor {
	case D3AnchorCenter:
		src = d3SalvageCenter[tier]
	case D3AnchorEdge:
		src = d3SalvageEdge[tier]
	default:
		return 0, 0, false
	}
	scale := float64(winH) / d3RefHeight
	return int32(math.Round(src[0] * scale)), int32(math.Round(src[1] * scale)), true
}

// D3KanaiButtons 是卡奈魔盒界面的 4 个按钮（客户区相对坐标）。
type D3KanaiButtons struct {
	Transmute  D3Point // 转化按钮
	InsertItem D3Point // 放入材料按钮
	PrevPage   D3Point
	NextPage   D3Point
}

// 魔盒按钮基准坐标（3440x1440）。
var d3KanaiButtonBase = [4][2]float64{{320, 1105}, {955, 1115}, {777, 1117}, {1135, 1117}}

// D3KanaiButtonXY 返回卡奈魔盒四个按钮的坐标。
func D3KanaiButtonXY(winH int32) (D3KanaiButtons, bool) {
	if winH <= 0 {
		return D3KanaiButtons{}, false
	}
	scale := float64(winH) / d3RefHeight
	at := func(i int) D3Point {
		return D3Point{
			X: int32(math.Round(d3KanaiButtonBase[i][0] * scale)),
			Y: int32(math.Round(d3KanaiButtonBase[i][1] * scale)),
		}
	}
	return D3KanaiButtons{
		Transmute: at(0), InsertItem: at(1), PrevPage: at(2), NextPage: at(3),
	}, true
}

package games

import "testing"

// stubAt 按基准坐标（取整）返回预设颜色；未预设的点视为取色失败。
// 这样界面判据可以在没有任何屏幕的情况下被完整测试。
func stubAt(colors map[[2]int]D3Color) D3ColorAt {
	return func(baseX, baseY float64, _ D3AnchorX) (D3Color, bool) {
		c, ok := colors[[2]int{int(baseX), int(baseY)}]
		return c, ok
	}
}

func copyColors(src map[[2]int]D3Color) map[[2]int]D3Color {
	out := make(map[[2]int]D3Color, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func TestD3SalvagePageStates(t *testing.T) {
	if s, b := D3SalvagePageOpen(stubAt(nil)); s != D3SalvageClosed || b != nil {
		t.Errorf("取不到色时应为 Closed，实际 %v", s)
	}

	// 铁匠页 + 分解页的 4 个前置判据点
	base := map[[2]int]D3Color{
		{339, 80}:   {R: 50, G: 100, B: 200}, // B>G>R、B>170、B-R>80
		{388, 86}:   {R: 30, G: 80, B: 150},  // B>G>R、B>110
		{351, 107}:  {R: 200, G: 200, B: 10}, // R+G>350
		{673, 1040}: {R: 200, G: 5, B: 5},    // R>50、G/B 极低
		{203, 337}:  {R: 10, G: 10, B: 10},
		{335, 371}:  {R: 200, G: 200, B: 200},
		{424, 371}:  {R: 10, G: 50, B: 200}, // 蓝按钮：B>G>R
	}

	// 稀有按钮不满足（B 不够低）→ 停在铁匠页
	smithy := copyColors(base)
	smithy[[2]int{514, 371}] = D3Color{R: 10, G: 10, B: 100}
	if s, _ := D3SalvagePageOpen(stubAt(smithy)); s != D3SalvageSmithy {
		t.Errorf("分解页未就绪时应为 Smithy，实际 %v", s)
	}

	// 稀有按钮满足 → 可分解，并带出按钮颜色
	ready := copyColors(base)
	ready[[2]int{514, 371}] = D3Color{R: 200, G: 100, B: 10}
	state, btns := D3SalvagePageOpen(stubAt(ready))
	if state != D3SalvageReady || btns == nil {
		t.Fatalf("应为 Ready 且返回按钮颜色，实际 %v", state)
	}
	if btns.Blue != (D3Color{R: 10, G: 50, B: 200}) {
		t.Errorf("蓝色按钮采样颜色不符：%+v", btns.Blue)
	}
}

func TestD3KanaiCubeStates(t *testing.T) {
	if s := D3KanaiCubeOpen(D3ClientEnglish, stubAt(nil)); s != D3KanaiClosed {
		t.Errorf("取不到色时应为 Closed，实际 %v", s)
	}

	// 魔盒已打开的共同判据点
	common := map[[2]int]D3Color{
		{353, 85}:  {R: 20, G: 20, B: 20},
		{278, 147}: {R: 150, G: 10, B: 10},
		{330, 140}: {R: 30, G: 60, B: 62},
	}

	reforge := copyColors(common)
	reforge[[2]int{788, 428}] = D3Color{R: 50, G: 100, B: 240}
	reforge[[2]int{810, 429}] = D3Color{R: 50, G: 100, B: 240}
	if s := D3KanaiCubeOpen(D3ClientEnglish, stubAt(reforge)); s != D3KanaiReforge {
		t.Errorf("应为 Reforge，实际 %v", s)
	}

	// 英文客户端的升级界面坐标
	upg := copyColors(common)
	upg[[2]int{799, 406}] = D3Color{R: 220, G: 200, B: 180}
	upg[[2]int{795, 592}] = D3Color{R: 220, G: 200, B: 10}
	if s := D3KanaiCubeOpen(D3ClientEnglish, stubAt(upg)); s != D3KanaiUpgrade {
		t.Errorf("应为 Upgrade，实际 %v", s)
	}

	// 同一组颜色放到非英文坐标（上移 22）上：英文客户端认不出，其它语言认得出。
	// 这条用例守的是「中文客户端 UI 整体上移」这个容易漏掉的差异。
	shifted := copyColors(common)
	shifted[[2]int{799, 406 - 22}] = D3Color{R: 220, G: 200, B: 180}
	shifted[[2]int{795, 592 - 22}] = D3Color{R: 220, G: 200, B: 10}
	if s := D3KanaiCubeOpen(D3ClientEnglish, stubAt(shifted)); s != D3KanaiUnknown {
		t.Errorf("英文客户端不应在偏移坐标上认出升级，实际 %v", s)
	}
	if s := D3KanaiCubeOpen(D3ClientOther, stubAt(shifted)); s != D3KanaiUpgrade {
		t.Errorf("非英文客户端应在偏移坐标上认出升级，实际 %v", s)
	}

	// 材料转化（非英文坐标：上移 43）
	conv := copyColors(common)
	conv[[2]int{799, 365 - 43}] = D3Color{R: 250, G: 200, B: 151}
	if s := D3KanaiCubeOpen(D3ClientOther, stubAt(conv)); s != D3KanaiConvert {
		t.Errorf("应为 Convert，实际 %v", s)
	}
}

func TestD3GambleOpen(t *testing.T) {
	colors := map[[2]int]D3Color{
		{320, 96}:  {R: 100, G: 50, B: 150}, // B>R>G、B>130
		{351, 100}: {R: 200, G: 200, B: 10}, // R+G>330
		{194, 67}:  {R: 0, G: 0, B: 0},      // 这两点和为 0
		{147, 94}:  {R: 0, G: 0, B: 0},
	}
	if !D3GambleOpen(stubAt(colors)) {
		t.Error("应识别为赌博页")
	}
	if D3GambleOpen(stubAt(nil)) {
		t.Error("取不到色时不应识别为赌博页")
	}
}

func TestD3InventoryOpen(t *testing.T) {
	colors := map[[2]int]D3Color{
		{3086, 108}: {R: 150, G: 150, B: 10},
		{3010, 147}: {R: 200, G: 10, B: 10},
		{3425, 142}: {R: 100, G: 100, B: 10},
		{3117, 84}:  {R: 10, G: 100, B: 200},
	}
	if !D3InventoryOpen(stubAt(colors)) {
		t.Error("应识别为背包页")
	}
	if D3InventoryOpen(stubAt(nil)) {
		t.Error("取不到色时不应识别为背包页")
	}
}

func TestD3StashOpen(t *testing.T) {
	colors := map[[2]int]D3Color{
		{282, 147}: {R: 200, G: 50, B: 55},
		{382, 77}:  {R: 50, G: 200, B: 100},
		{299, 82}:  {R: 200, G: 100, B: 10},
	}
	if !D3StashOpen(stubAt(colors)) {
		t.Error("应识别为储物箱页")
	}
	if D3StashOpen(stubAt(nil)) {
		t.Error("取不到色时不应识别为储物箱页")
	}
}

func TestD3DialogOnScreen(t *testing.T) {
	colors := map[[2]int]D3Color{
		{1655, 500}: {R: 100, G: 10, B: 2},
		{1800, 500}: {R: 100, G: 10, B: 2},
	}
	if !D3DialogOnScreen(stubAt(colors)) {
		t.Error("应识别出对话框")
	}
	if D3DialogOnScreen(stubAt(nil)) {
		t.Error("取不到色时不应识别出对话框")
	}
}

// recordingPixel 记录收到的取色坐标，用于验证锚点换算。
type recordingPixel struct {
	w, h  int32
	calls [][2]int32
}

func (p *recordingPixel) ClientRect(uintptr) (int32, int32, int32, int32, bool) {
	return p.w, p.h, 0, 0, true
}

func (p *recordingPixel) Pixel(x, y int32) (uint8, uint8, uint8, bool) {
	p.calls = append(p.calls, [2]int32{x, y})
	return 0, 0, 0, true
}

func TestD3ColorSamplerAnchors(t *testing.T) {
	// 三种锚点必须各自换算正确，否则界面判据会整体偏移。
	// 取 winW=2000、winH=1440（scale=1），让三种锚点结果互不相同。
	px := &recordingPixel{w: 2000, h: 1440}
	at := D3ColorSampler(px, 2000, 1440, 0, 0)
	_, _ = at(100, 50, D3AnchorLeft)     // 100
	_, _ = at(3357, 747, D3AnchorRight)  // 2000-(3440-3357) = 1917
	_, _ = at(1655, 500, D3AnchorCenter) // 1000+(1655-1720) = 935
	want := [][2]int32{{100, 50}, {1917, 747}, {935, 500}}
	if len(px.calls) != len(want) {
		t.Fatalf("取色次数 = %d，want %d", len(px.calls), len(want))
	}
	for i, w := range want {
		if px.calls[i] != w {
			t.Errorf("第 %d 次取色坐标 = %v，want %v", i+1, px.calls[i], w)
		}
	}
}

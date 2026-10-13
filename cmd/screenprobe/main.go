//go:build windows

// Diablo Lazy Macro · 屏幕标定探针。
//
// 用途：把游戏画面截成 PNG（供人工核对，或交给标定流程），并报告窗口客户区几何与分辨率，
// 用来确认「当前显示模式能不能被抓到画面」——这是所有屏幕识别功能的前置条件。
//
// 两种截图后端：
//   - desktop：GDI BitBlt 读桌面合成画面。要求窗口可见且未被遮挡；能精确截客户区。
//   - window：PrintWindow 让窗口自己绘制。窗口被遮挡、不在前台也能抓，但部分独占渲染会黑帧。
//
// 无论哪种，都只用系统公开 API：零注入、零内存读取、零驱动。
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/imsxx/diablo-lazy-macro/internal/keys"
	"github.com/imsxx/diablo-lazy-macro/internal/win32"
	"golang.org/x/sys/windows"
)

func main() {
	process := flag.String("process", "", "目标进程名（如 \"Diablo III64.exe\"）；留空 = 截屏幕区域")
	out := flag.String("out", "screen.png", "输出 PNG 路径")
	backend := flag.String("backend", "auto", "截图后端：auto | desktop | window")
	press := flag.String("press", "", "截图前先按下的键，逗号分隔（如 1,2,3,4）；仅当目标窗口位于前台时发送")
	activate := flag.Bool("activate", false, "发送按键前临时把目标窗口切到前台、发完立刻还原（标定专用；正式程序不抢焦点）")
	settle := flag.Int("settle", 250, "按下按键后、截图前等待的毫秒数")
	delay := flag.Int("delay", 0, "开始前等待的秒数（留时间切回游戏窗口）")
	x := flag.Int("x", 0, "区域左上角屏幕坐标 X（仅 -process 留空时有效）")
	y := flag.Int("y", 0, "区域左上角屏幕坐标 Y（仅 -process 留空时有效）")
	w := flag.Int("w", 0, "区域宽度（0 = 全屏宽）")
	h := flag.Int("h", 0, "区域高度（0 = 全屏高）")
	px := flag.String("px", "", "读取客户区相对坐标的颜色，如 \"603,945\" 或 \"603,945;667,945\"")
	band := flag.String("band", "", "扫描横向带内的彩色列区间，格式 \"y0,y1\"，用于定位技能栏等 UI 元素")
	sat := flag.Int("sat", 40, "彩色判定的饱和度阈值（配合 -band）")
	crop := flag.String("crop", "", "只保留客户区的该子区域 \"x,y,w,h\"，便于放大观察 UI 细节（-px/-band 随之一并改为相对该子区域）")
	region := flag.String("region", "", "在矩形 \"x0,y0,x1,y1\" 内搜索：报告非黑包围盒与 G 通道峰值位置（用于定位 Buff 光晕）")
	burst := flag.Int("burst", 0, "保存首张后继续连拍的张数，用于观察 Buff 生效 / 消失的过程")
	interval := flag.Int("interval", 500, "连拍间隔（毫秒）")
	flag.Parse()

	if *delay > 0 {
		fmt.Printf("等待 %d 秒后开始，请切到目标画面…\n", *delay)
		time.Sleep(time.Duration(*delay) * time.Second)
	}

	fmt.Println("=== Diablo Lazy Macro · 屏幕标定探针 ===")
	screenW, screenH := win32.ScreenSize()
	fmt.Printf("主显示器分辨率：%d×%d\n", screenW, screenH)

	target, err := locate(*process, int32(*x), int32(*y), int32(*w), int32(*h), screenW, screenH)
	if err != nil {
		die("%v", err)
	}
	report(target)

	if *press != "" {
		if err := pressKeys(*press, target, *activate); err != nil {
			die("%v", err)
		}
		fmt.Printf("已按下 %s，等待 %dms 后截图…\n", *press, *settle)
		time.Sleep(time.Duration(*settle) * time.Millisecond)
	}

	img, used, err := shoot(target, *backend)
	if err != nil {
		die("截图失败：%v", err)
	}
	cropX, cropY, cropW, cropH, cropOK := parseCrop(*crop)
	if *crop != "" && !cropOK {
		die("-crop 格式错误，应为 x,y,w,h")
	}
	if cropOK {
		sub := win32.CropRGBA(img, int32(cropX), int32(cropY), int32(cropW), int32(cropH))
		if sub == nil {
			die("-crop 区域超出画面范围")
		}
		fmt.Printf("已裁剪子区域：%dx%d（客户区偏移 %d,%d；后续 -px/-band 坐标均相对本子区域）\n",
			sub.Bounds().Dx(), sub.Bounds().Dy(), cropX, cropY)
		img = sub
	}
	save(*out, img, used)
	if *px != "" {
		probePoints(img, *px)
	}
	if *region != "" {
		for _, item := range strings.Split(*region, ";") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			rx0, ry0, rx1, ry1, ok := parseCrop(item)
			if !ok {
				fmt.Printf("[搜索] %q 格式错误，应为 x0,y0,x1,y1\n", item)
				continue
			}
			scanRegion(img, rx0, ry0, rx1, ry1)
		}
	}
	if *band != "" {
		y0, y1, ok := parseBand(*band)
		if !ok {
			fmt.Println("[扫描] -band 格式错误，应为 y0,y1")
		} else {
			scanBand(img, y0, y1, *sat)
		}
	}

	// 连拍：观察 Buff 生效 / 消失这类随时间变化的过程。
	for i := 1; i <= *burst; i++ {
		time.Sleep(time.Duration(*interval) * time.Millisecond)
		bi, _, err := shoot(target, *backend)
		if err != nil {
			fmt.Printf("[连拍] 第 %d 张失败：%v\n", i, err)
			break
		}
		if cropOK {
			if sub := win32.CropRGBA(bi, int32(cropX), int32(cropY), int32(cropW), int32(cropH)); sub != nil {
				bi = sub
			}
		}
		path := burstPath(*out, i)
		if err := writePNG(path, bi); err != nil {
			fmt.Printf("[连拍] 保存 %s 失败：%v\n", path, err)
			break
		}
		fmt.Printf("[连拍 %d] %s 非黑 %.1f%%\n", i, path, win32.NonBlackRatio(bi)*100)
		if *px != "" {
			probePoints(bi, *px)
		}
	}
}

// burstPath 由首张文件名推出第 i 张连拍的文件名（out.png → out-03.png）。
func burstPath(out string, i int) string {
	ext := filepath.Ext(out)
	return fmt.Sprintf("%s-%02d%s", strings.TrimSuffix(out, ext), i, ext)
}

func parseCrop(s string) (int, int, int, int, bool) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return 0, 0, 0, 0, false
	}
	var v [4]int
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return 0, 0, 0, 0, false
		}
		v[i] = n
	}
	return v[0], v[1], v[2], v[3], true
}

// scanRegion 在矩形内搜索：报告非黑像素的包围盒，以及 G 通道峰值的位置与颜色。
// Buff 光晕的判据正是 G 通道（D3keyHelper 用 G>=95），所以峰值点就是最可靠的采样坐标。
func scanRegion(img *image.RGBA, x0, y0, x1, y1 int) {
	b := img.Bounds()
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > b.Dx() {
		x1 = b.Dx()
	}
	if y1 > b.Dy() {
		y1 = b.Dy()
	}
	if x1 <= x0 || y1 <= y0 {
		fmt.Printf("[搜索] 区域无效：(%d,%d)-(%d,%d)（画面 %dx%d）\n", x0, y0, x1, y1, b.Dx(), b.Dy())
		return
	}

	n := 0
	bx0, by0, bx1, by1 := x1, y1, x0-1, y0-1
	peakG := -1
	var pr, pg, pb, pxx, pyy int
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			rr, gg, bb := int(r>>8), int(g>>8), int(bl>>8)
			if rr > 12 || gg > 12 || bb > 12 {
				n++
				if x < bx0 {
					bx0 = x
				}
				if y < by0 {
					by0 = y
				}
				if x > bx1 {
					bx1 = x
				}
				if y > by1 {
					by1 = y
				}
			}
			if gg > peakG {
				peakG, pr, pg, pb, pxx, pyy = gg, rr, gg, bb, x, y
			}
		}
	}

	fmt.Printf("[搜索] 区域 (%d,%d)-(%d,%d)｜非黑像素 %d 个\n", x0, y0, x1, y1, n)
	if n > 0 {
		fmt.Printf("  非黑包围盒：x=%d..%d，y=%d..%d（%dx%d）\n", bx0, bx1, by0, by1, bx1-bx0+1, by1-by0+1)
	} else {
		fmt.Println("  区域内没有非黑像素")
	}
	fmt.Printf("  G 通道峰值：(%d,%d) → RGB(%d, %d, %d)｜%s\n",
		pxx, pyy, pr, pg, pb, buffVerdict(peakG))
}

// buffVerdict 按 D3keyHelper 的判据（G>=95）给出结论。
func buffVerdict(g int) string {
	if g >= 95 {
		return fmt.Sprintf("G=%d ≥ 95 → 判为 Buff 生效", g)
	}
	return fmt.Sprintf("G=%d < 95 → 判为 Buff 未生效", g)
}

func parseBand(s string) (int, int, bool) {
	parts := strings.SplitN(s, ",", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	y0, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	y1, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return y0, y1, true
}

// scanBand 在 [y0, y1) 横向带里找出「有颜色」的列，合并成连续区间后报告。
// 用于精确判定技能图标的水平位置：比肉眼看图准，也不受图像缩放影响。
func scanBand(img *image.RGBA, y0, y1, sat int) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if y0 < 0 {
		y0 = 0
	}
	if y1 > h {
		y1 = h
	}
	if y1 <= y0 {
		fmt.Printf("[扫描] 纵向范围无效：%d..%d（画面高 %d）\n", y0, y1, h)
		return
	}

	colored := make([]bool, w)
	sumR, sumG, sumB, count := make([]int, w), make([]int, w), make([]int, w), make([]int, w)
	for y := y0; y < y1; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			rr, gg, bb := int(r>>8), int(g>>8), int(bl>>8)
			mx, mn := rr, rr
			if gg > mx {
				mx = gg
			}
			if bb > mx {
				mx = bb
			}
			if gg < mn {
				mn = gg
			}
			if bb < mn {
				mn = bb
			}
			if mx >= 60 && mx-mn >= sat {
				colored[x] = true
				sumR[x] += rr
				sumG[x] += gg
				sumB[x] += bb
				count[x]++
			}
		}
	}

	fmt.Printf("[扫描] 纵向 %d..%d | 饱和度阈值 %d\n", y0, y1, sat)
	start := -1
	for x := 0; x <= w; x++ {
		on := x < w && colored[x]
		if on && start < 0 {
			start = x
			continue
		}
		if !on && start >= 0 {
			emitBand(start, x-1, sumR, sumG, sumB, count)
			start = -1
		}
	}
}

func emitBand(x0, x1 int, sumR, sumG, sumB, count []int) {
	n := x1 - x0 + 1
	if n < 2 {
		return
	}
	var r, g, b, c int
	for x := x0; x <= x1; x++ {
		r += sumR[x]
		g += sumG[x]
		b += sumB[x]
		c += count[x]
	}
	if c == 0 {
		return
	}
	fmt.Printf("  区间 x=%d..%d（宽 %d，中心 %d）| 平均色 RGB(%d, %d, %d)\n",
		x0, x1, n, (x0+x1)/2, r/c, g/c, b/c)
}

// probePoints 读取指定客户区相对坐标的颜色，用于标定时核对某个点究竟是什么颜色。
// 之所以必须从截图里读、而不是直接 GetPixel：走 PrintWindow 后端的窗口在屏幕上可能根本不可见。
func probePoints(img *image.RGBA, list string) {
	b := img.Bounds()
	for _, item := range strings.Split(list, ";") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, ",", 2)
		if len(parts) != 2 {
			fmt.Printf("[取样] %q 格式错误，应为 x,y\n", item)
			continue
		}
		x, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		y, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil {
			fmt.Printf("[取样] %q 解析失败\n", item)
			continue
		}
		if x < 0 || y < 0 || x >= b.Dx() || y >= b.Dy() {
			fmt.Printf("[取样] (%d,%d) 超出画面范围 %dx%d\n", x, y, b.Dx(), b.Dy())
			continue
		}
		r, g, bl, _ := img.At(x, y).RGBA()
		fmt.Printf("[取样] (%d,%d) → RGB(%d, %d, %d)\n", x, y, r>>8, g>>8, bl>>8)
	}
}

// target 描述本次截图的取景框。hwnd 为 0 表示纯屏幕区域截图。
type target struct {
	hwnd        windows.Handle
	title       string
	processName string
	// 客户区：游戏画面的实际范围
	cx, cy, cw, ch int32
	// 窗口矩形（含边框）：PrintWindow 后端按它取整窗，再裁出客户区
	wx, wy, ww, wh int32
	foreground     bool
	byScreen       bool
}

func locate(process string, x, y, w, h, screenW, screenH int32) (target, error) {
	if process == "" {
		cw, ch := int32(w), int32(h)
		if cw <= 0 {
			cw = screenW
		}
		if ch <= 0 {
			ch = screenH
		}
		return target{cx: int32(x), cy: int32(y), cw: cw, ch: ch, byScreen: true}, nil
	}

	hwnd, ok := win32.FindWindowByProcess(process)
	if !ok {
		return target{}, fmt.Errorf("找不到进程 %q 的可见窗口：请确认游戏已启动、未被最小化（最小化后窗口不可见）", process)
	}
	t := target{hwnd: hwnd, processName: process}
	// 标题只在目标窗口恰好是前台时才读得到；非前台时不去动前台窗口的信息。
	if fg, err := win32.GetForegroundWindow(); err == nil && fg.Handle == hwnd {
		t.title = fg.Title
		t.foreground = true
	}
	if t.title == "" {
		t.title = "(非前台，标题略)"
	}

	ok = false
	t.cw, t.ch, t.cx, t.cy, ok = win32.Screen{}.ClientRect(uintptr(hwnd))
	if !ok {
		return target{}, fmt.Errorf("读取窗口客户区失败")
	}
	rect, ok := win32.GetWindowRectOf(hwnd)
	if !ok {
		return target{}, fmt.Errorf("读取窗口矩形失败")
	}
	t.wx, t.wy, t.ww, t.wh = rect.X, rect.Y, rect.Width, rect.Height
	return t, nil
}

func report(t target) {
	if t.byScreen {
		fmt.Printf("取景方式：屏幕区域 %dx%d @ (%d,%d)\n", t.cw, t.ch, t.cx, t.cy)
		return
	}
	fgText := "否"
	if t.foreground {
		fgText = "是"
	}
	fmt.Printf("窗口：%s | 进程：%s | 位于前台：%s\n", t.title, t.processName, fgText)
	fmt.Printf("窗口矩形：%dx%d @ (%d,%d)（含边框）\n", t.ww, t.wh, t.wx, t.wy)
	fmt.Printf("游戏客户区：%dx%d @ 屏幕(%d,%d)\n", t.cw, t.ch, t.cx, t.cy)
}

// pressKeys 在发送按键前强制校验前台窗口，避免把技能键打进别的程序。
// activate 为真时允许临时接管焦点（标定专用）：切到目标窗口、发完立刻还原。
func pressKeys(list string, t target, activate bool) error {
	if t.byScreen {
		return fmt.Errorf("-press 需要配合 -process 使用")
	}
	if !win32.IsForeground(t.hwnd) {
		if !activate {
			return fmt.Errorf("目标窗口不在前台，拒绝发送按键（否则会打进当前前台程序）。" +
				"请切回游戏再跑，或加 -activate 让探针临时接管焦点，或用 -delay 留出切窗口的时间")
		}
		prev := win32.ForegroundHandle()
		if err := win32.ActivateWindow(t.hwnd); err != nil {
			return err
		}
		time.Sleep(250 * time.Millisecond) // 等焦点真正切过去再发键
		fmt.Println("已临时接管焦点（-activate）")
		defer func() {
			if prev != 0 && !win32.IsForeground(prev) {
				if err := win32.ActivateWindow(prev); err != nil {
					fmt.Printf("[提示] 还原原前台窗口失败：%v\n", err)
					return
				}
				fmt.Println("已还原原前台窗口")
			}
		}()
	}
	for _, part := range splitList(list) {
		vk, err := keys.Parse(part)
		if err != nil {
			return fmt.Errorf("解析按键 %q 失败：%v", part, err)
		}
		if err := win32.Tap(vk); err != nil {
			return fmt.Errorf("发送按键 %s 失败：%v", keys.Label(vk), err)
		}
		fmt.Printf("  → %s\n", keys.Label(vk))
		time.Sleep(60 * time.Millisecond)
	}
	return nil
}

// shoot 按后端选择截图方式，返回图像与所用后端名。
func shoot(t target, backend string) (*image.RGBA, string, error) {
	if t.byScreen {
		img, err := win32.CaptureRect(t.cx, t.cy, t.cw, t.ch)
		return img, "desktop", err
	}

	useWindow := backend == "window" || (backend == "auto" && !t.foreground)
	if useWindow {
		img, err := win32.CaptureWindow(t.hwnd, t.ww, t.wh)
		if err != nil {
			return nil, "", err
		}
		// PrintWindow 抓的是整窗（含标题栏）：裁出客户区
		cropped := win32.CropRGBA(img, t.cx-t.wx, t.cy-t.wy, t.cw, t.ch)
		if cropped == nil {
			return nil, "", fmt.Errorf("裁剪客户区失败（窗口几何异常）")
		}
		return cropped, "window", nil
	}

	img, err := win32.CaptureRect(t.cx, t.cy, t.cw, t.ch)
	return img, "desktop", err
}

func writePNG(path string, img *image.RGBA) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func save(path string, img *image.RGBA, used string) {
	ratio := win32.NonBlackRatio(img)
	start := time.Now()
	if err := writePNG(path, img); err != nil {
		die("写入 PNG 失败：%v", err)
	}

	b := img.Bounds()
	fmt.Printf("截图后端：%s | 输出尺寸：%dx%d\n", used, b.Dx(), b.Dy())
	fmt.Printf("非黑像素占比：%.1f%% | 编码耗时：%s\n", ratio*100, time.Since(start).Round(time.Millisecond))
	if info, err := os.Stat(path); err == nil {
		fmt.Printf("已保存：%s（%d 字节）\n", path, info.Size())
	}

	if ratio < 0.05 {
		fmt.Println("[警告] 画面几乎全黑：")
		fmt.Println("        · desktop 后端：游戏可能运行在「全屏独占」模式（不参与桌面合成），或窗口被遮挡/最小化;")
		fmt.Println("        · window 后端：该游戏可能不响应 WM_PRINT（独占渲染），改用 -backend desktop（需窗口可见）;")
		fmt.Println("        · 也可能只是正好处于读盘/切场景的黑屏过场。")
	} else if used == "window" {
		fmt.Println("[说明] 走的是 PrintWindow 后端：窗口被遮挡也能抓，但个别画面（如过场动画）可能不如桌面合成准确。")
	}
}

func splitList(s string) []string {
	var out []string
	for _, part := range []rune(s) {
		if part == ',' || part == ' ' {
			continue
		}
		out = append(out, string(part))
	}
	return out
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

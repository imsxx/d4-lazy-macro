//go:build windows

// Diablo Lazy Macro · 屏幕标定探针。
//
// 用途：把游戏画面截成 PNG（供人工核对，或交给标定流程），并报告窗口客户区几何与分辨率，
// 用来确认「当前显示模式能不能被抓到画面」——这是所有屏幕识别功能的前置条件。
//
// 只用 GDI BitBlt 从桌面 DC 抓取公开画面：零注入、零内存读取、零驱动，
// 与正式实现（internal/win32）同源，因此探针的结论可以直接当作产品行为。
package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"time"

	"github.com/imsxx/diablo-lazy-macro/internal/win32"
)

func main() {
	process := flag.String("process", "", "截该进程的前台窗口客户区（留空 = 截屏幕区域）")
	out := flag.String("out", "screen.png", "输出 PNG 路径")
	x := flag.Int("x", 0, "区域左上角屏幕坐标 X")
	y := flag.Int("y", 0, "区域左上角屏幕坐标 Y")
	w := flag.Int("w", 0, "区域宽度（0 = 全屏宽）")
	h := flag.Int("h", 0, "区域高度（0 = 全屏高）")
	delay := flag.Int("delay", 0, "截图前等待的秒数（留时间切回游戏窗口）")
	flag.Parse()

	if *delay > 0 {
		fmt.Printf("等待 %d 秒后截图，请切回游戏窗口…\n", *delay)
		time.Sleep(time.Duration(*delay) * time.Second)
	}

	screenW, screenH := win32.ScreenSize()
	fmt.Println("=== Diablo Lazy Macro · 屏幕标定探针 ===")
	fmt.Printf("主显示器分辨率：%d×%d\n", screenW, screenH)

	cx, cy, cw, ch := int32(*x), int32(*y), int32(*w), int32(*h)
	if *process != "" {
		fg, err := win32.GetForegroundWindow()
		if err != nil {
			die("读取前台窗口失败：%v", err)
		}
		fmt.Printf("前台窗口：%s | 类名：%s | 进程：%s (PID %d)\n",
			fg.Title, fg.ClassName, fg.ProcessName, fg.ProcessID)
		if !win32.MatchProcess(fg.ProcessName, *process) {
			die("前台窗口不是 %q（当前是 %q）：请先切到游戏画面再跑本探针", *process, fg.ProcessName)
		}
		ok := false
		cw, ch, cx, cy, ok = win32.Screen{}.ClientRect(uintptr(fg.Handle))
		if !ok {
			die("读取窗口客户区失败")
		}
		fmt.Printf("游戏客户区：%d×%d @ 屏幕(%d,%d)\n", cw, ch, cx, cy)
	} else {
		if cw <= 0 {
			cw = screenW
		}
		if ch <= 0 {
			ch = screenH
		}
	}

	start := time.Now()
	img, err := win32.CaptureRect(cx, cy, cw, ch)
	if err != nil {
		die("截图失败：%v", err)
	}
	elapsed := time.Since(start)
	ratio := win32.NonBlackRatio(img)

	f, err := os.Create(*out)
	if err != nil {
		die("创建输出文件失败：%v", err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		die("写入 PNG 失败：%v", err)
	}
	_ = f.Close()
	info, err := os.Stat(*out)
	if err != nil {
		die("读取输出文件信息失败：%v", err)
	}

	fmt.Printf("截图区域：%dx%d @ 屏幕(%d,%d)\n", cw, ch, cx, cy)
	fmt.Printf("耗时：%s | 非黑像素占比：%.1f%%\n", elapsed.Round(time.Microsecond), ratio*100)
	fmt.Printf("已保存：%s（%d 字节）\n", *out, info.Size())

	if ratio < 0.05 {
		fmt.Println("[警告] 画面几乎全黑，三种可能：")
		fmt.Println("        1) 游戏运行在「全屏独占」模式——该模式不参与桌面合成，GDI 抓不到；")
		fmt.Println("        2) 游戏窗口被其他窗口遮挡，或已最小化；")
		fmt.Println("        3) 游戏正处于读盘/切场景的黑屏过场。")
		fmt.Println("        对策：改用「窗口化」或「无边框窗口化」，并确保窗口完整可见。")
		return
	}
	if cw == screenW && ch == screenH && cx == 0 && cy == 0 {
		fmt.Println("[提示] 客户区尺寸与主显示器一致：可能正是全屏 / 无边框窗口化。既然没有全黑警告，说明能正常抓取。")
	}
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

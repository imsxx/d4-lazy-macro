//go:build windows

package win32

import (
	"fmt"
	"image"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procGetDIBits              = gdi32.NewProc("GetDIBits")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procPrintWindow            = user32.NewProc("PrintWindow")
)

const (
	srcCopy      = 0x00CC0020 // BitBlt 的 SRCCOPY
	dibRGBColors = 0          // GetDIBits 的 DIB_RGB_COLORS
	biRGB        = 0          // BITMAPINFOHEADER 的 BI_RGB（不压缩）

	// pwRenderFullContent 让 PrintWindow 使用 DWM 的完整内容渲染（Win8.1+），
	// 比默认标志更可能正确抓取 GPU 合成的窗口。
	pwRenderFullContent = 0x00000002
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

// CaptureRect 截取屏幕绝对物理坐标 (x, y) 起 w×h 的区域。
//
// 抓的是「桌面合成后的画面」，因此窗口必须可见；被其他窗口遮挡时会抓到遮挡者。
// 与取色（GetPixelColor）同源，不读游戏内存、不注入。
func CaptureRect(x, y, w, h int32) (*image.RGBA, error) {
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("截图区域尺寸无效：%dx%d", w, h)
	}
	return captureWith(w, h, func(hdcMem, hdcScreen uintptr) error {
		ret, _, err := procBitBlt.Call(hdcMem, 0, 0, uintptr(w), uintptr(h),
			hdcScreen, uintptr(x), uintptr(y), srcCopy)
		if ret == 0 {
			return fmt.Errorf("BitBlt 失败：%v", err)
		}
		return nil
	})
}

// CaptureWindow 截取指定窗口的完整画面（含边框），返回尺寸为 w×h 的图像。
//
// 与 CaptureRect 的关键区别：这个走 PrintWindow，是让窗口**自己**把内容画到我们提供的 DC 上，
// 而不是读桌面合成结果。因此窗口被别的窗口挡住、甚至不在前台也能抓到——这正是「游戏放后台、
// 前面照常干活」所需要的。代价是它依赖窗口响应 WM_PRINT，改用独占渲染的程序会返回黑帧，
// 调用方应当用 NonBlackRatio 自检并回退。
func CaptureWindow(hwnd windows.Handle, w, h int32) (*image.RGBA, error) {
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("窗口尺寸无效：%dx%d", w, h)
	}
	return captureWith(w, h, func(hdcMem, _ uintptr) error {
		ret, _, err := procPrintWindow.Call(uintptr(hwnd), hdcMem, pwRenderFullContent)
		if ret == 0 {
			return fmt.Errorf("PrintWindow 失败：%v", err)
		}
		return nil
	})
}

// captureWith 建一张 w×h 的 32 位位图，交给 draw 填充后读回 RGBA。
func captureWith(w, h int32, draw func(hdcMem, hdcScreen uintptr) error) (*image.RGBA, error) {
	hdcScreen, _, _ := procGetDC.Call(0)
	if hdcScreen == 0 {
		return nil, fmt.Errorf("GetDC 失败")
	}
	defer procReleaseDC.Call(0, hdcScreen)

	hdcMem, _, _ := procCreateCompatibleDC.Call(hdcScreen)
	if hdcMem == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC 失败")
	}
	defer procDeleteDC.Call(hdcMem)

	hbm, _, _ := procCreateCompatibleBitmap.Call(hdcScreen, uintptr(w), uintptr(h))
	if hbm == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap 失败")
	}
	defer procDeleteObject.Call(hbm)

	old, _, _ := procSelectObject.Call(hdcMem, hbm)
	if old == 0 {
		return nil, fmt.Errorf("SelectObject 失败")
	}
	defer procSelectObject.Call(hdcMem, old)

	if err := draw(hdcMem, hdcScreen); err != nil {
		return nil, err
	}

	var bmi bitmapInfo
	bmi.Header.Size = uint32(unsafe.Sizeof(bmi.Header))
	bmi.Header.Width = w
	bmi.Header.Height = -h // 负数 = 自上而下，省去逐行翻转
	bmi.Header.Planes = 1
	bmi.Header.BitCount = 32
	bmi.Header.Compression = biRGB

	buf := make([]byte, int(w)*int(h)*4)
	ret, _, err := procGetDIBits.Call(hdcMem, hbm, 0, uintptr(h),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bmi)), dibRGBColors)
	if ret == 0 {
		return nil, fmt.Errorf("GetDIBits 失败：%v", err)
	}

	// GDI 给的是 BGRA，image.RGBA 要 RGBA。
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	for i := 0; i < len(buf); i += 4 {
		img.Pix[i] = buf[i+2]
		img.Pix[i+1] = buf[i+1]
		img.Pix[i+2] = buf[i]
		img.Pix[i+3] = 0xFF
	}
	return img, nil
}

// CropRGBA 从图像里裁出 (x, y) 起 w×h 的子图；越界的部分会被夹到边界内。
func CropRGBA(src *image.RGBA, x, y, w, h int32) *image.RGBA {
	if src == nil {
		return nil
	}
	b := src.Bounds()
	x0, y0 := int(x), int(y)
	x1, y1 := x0+int(w), y0+int(h)
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
		return nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	for row := y0; row < y1; row++ {
		srcOff := src.PixOffset(x0, row)
		dstOff := dst.PixOffset(0, row-y0)
		copy(dst.Pix[dstOff:dstOff+(x1-x0)*4], src.Pix[srcOff:srcOff+(x1-x0)*4])
	}
	return dst
}

// ScreenSize 返回主显示器的物理分辨率。
func ScreenSize() (w, h int32) {
	wv, _, _ := procGetSystemMetrics.Call(0) // SM_CXSCREEN
	hv, _, _ := procGetSystemMetrics.Call(1) // SM_CYSCREEN
	return int32(wv), int32(hv)
}

// NonBlackRatio 返回图像中「非纯黑」像素的占比。
// 用于自检截图是否真的抓到了画面：全屏独占、窗口被遮挡或黑屏过场时会接近 0。
func NonBlackRatio(img *image.RGBA) float64 {
	if img == nil || len(img.Pix) == 0 {
		return 0
	}
	total := 0
	nonBlack := 0
	for i := 0; i < len(img.Pix); i += 4 {
		total++
		if img.Pix[i] > 8 || img.Pix[i+1] > 8 || img.Pix[i+2] > 8 {
			nonBlack++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(nonBlack) / float64(total)
}

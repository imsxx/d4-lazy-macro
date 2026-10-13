//go:build windows

package win32

import (
	"fmt"
	"image"
	"unsafe"
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
)

const (
	srcCopy      = 0x00CC0020 // BitBlt 的 SRCCOPY
	dibRGBColors = 0          // GetDIBits 的 DIB_RGB_COLORS
	biRGB        = 0          // BITMAPINFOHEADER 的 BI_RGB（不压缩）
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

// CaptureRect 截取屏幕绝对物理坐标 (x, y) 起 w×h 的区域，返回 RGBA 图像。
//
// 与取色（GetPixelColor）同源：都是 GDI 从桌面 DC 抓取公开画面，不读游戏内存、不注入。
// 因此有同样的前提——目标窗口必须可见且未被遮挡；游戏若运行在「全屏独占」模式下
// 不参与桌面合成，此时会抓到黑帧（用 -process 模式跑一次即可判断）。
func CaptureRect(x, y, w, h int32) (*image.RGBA, error) {
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("截图区域尺寸无效：%dx%d", w, h)
	}
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

	if ret, _, err := procBitBlt.Call(hdcMem, 0, 0, uintptr(w), uintptr(h),
		hdcScreen, uintptr(x), uintptr(y), srcCopy); ret == 0 {
		return nil, fmt.Errorf("BitBlt 失败：%v", err)
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

// ScreenSize 返回主显示器的物理分辨率。
func ScreenSize() (w, h int32) {
	wv, _, _ := procGetSystemMetrics.Call(0) // SM_CXSCREEN
	hv, _, _ := procGetSystemMetrics.Call(1) // SM_CYSCREEN
	return int32(wv), int32(hv)
}

// NonBlackRatio 返回图像中「非纯黑」像素的占比。
// 用于判断截图是否真的抓到了画面：全屏独占模式或窗口被遮住时会接近 0。
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

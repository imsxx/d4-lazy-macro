// Command mkico 把一张方形源图整理成 Windows 图标资源：
//
//  1. 清理右下角水印区域（用背景色填充）
//  2. 按圆角矩形裁切并羽化边缘（外部透明）
//  3. 输出规范源图（PNG）与多尺寸 ICO（BMP 帧，兼容 Win7+ 与 LoadIconWithScaleDown）
//
// 用法：
//
//	go run ./tools/mkico -in assets/raw.png -out assets/icon.ico -png assets/icon.png
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

var iconSizes = []int{16, 24, 32, 48, 64, 128, 256}

func main() {
	in := flag.String("in", "", "源 PNG 路径（必填）")
	outICO := flag.String("out", "assets/icon.ico", "输出 ICO 路径")
	outPNG := flag.String("png", "assets/icon.png", "输出清理后的源 PNG 路径")
	radius := flag.Float64("radius", 0.22, "圆角半径占边长的比例")
	feather := flag.Float64("feather", 1.5, "圆角边缘羽化像素（源图尺度）")
	flag.Parse()

	if *in == "" {
		fatal("请用 -in 指定源 PNG 路径")
	}
	src, err := loadPNG(*in)
	if err != nil {
		fatal(err.Error())
	}
	clean := cleanIcon(src, *radius, *feather)
	if err := writePNG(*outPNG, clean); err != nil {
		fatal(err.Error())
	}
	frames := make([][]byte, 0, len(iconSizes))
	for _, size := range iconSizes {
		frames = append(frames, bmpFrame(downscale(clean, size)))
	}
	if err := os.MkdirAll(filepath.Dir(*outICO), 0o755); err != nil {
		fatal(err.Error())
	}
	if err := os.WriteFile(*outICO, buildICO(iconSizes, frames), 0o644); err != nil {
		fatal(err.Error())
	}
	fmt.Printf("已生成 %s（%d 尺寸）与 %s（%dx%d）\n", *outICO, len(iconSizes), *outPNG, clean.Bounds().Dx(), clean.Bounds().Dy())
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "mkico:", msg)
	os.Exit(1)
}

func loadPNG(path string) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("解码 %s 失败：%v", path, err)
	}
	b := img.Bounds()
	if b.Dx() != b.Dy() {
		return nil, fmt.Errorf("源图必须是正方形，实际 %dx%d", b.Dx(), b.Dy())
	}
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dst.Set(x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst, nil
}

// cleanIcon 去水印 + 圆角透明。
func cleanIcon(src *image.RGBA, radiusRatio, feather float64) *image.RGBA {
	w := src.Bounds().Dx()
	h := src.Bounds().Dy()
	bg := src.RGBAAt(w/2, h/12) // 顶部中央取样：必为纯背景色

	// 1) 水印区域用背景色填充（右下角，源图里只有背景与圆角外的白底）
	for y := int(float64(h) * 0.88); y < h; y++ {
		for x := int(float64(w) * 0.84); x < w; x++ {
			src.SetRGBA(x, y, bg)
		}
	}

	// 2) 圆角遮罩：外部透明，边缘羽化
	r := radiusRatio * float64(w)
	corners := [4][2]float64{{r, r}, {float64(w) - r, r}, {r, float64(h) - r}, {float64(w) - r, float64(h) - r}}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d := insideDistance(float64(x)+0.5, float64(y)+0.5, float64(w), float64(h), r, corners)
			if d >= feather {
				continue
			}
			coverage := 0.0
			if d > -feather {
				coverage = (d + feather) / (2 * feather)
			}
			c := src.RGBAAt(x, y)
			c.A = uint8(math.Round(float64(c.A) * coverage))
			src.SetRGBA(x, y, c)
		}
	}
	return src
}

// insideDistance 返回点到圆角矩形边界的有符号距离（内部为正，边长为 w/h、圆角半径 r）。
func insideDistance(x, y, w, h, r float64, corners [4][2]float64) float64 {
	dx := math.Min(x, w-x)
	dy := math.Min(y, h-y)
	if dx >= r || dy >= r {
		return math.Min(dx, dy)
	}
	best := math.Inf(-1)
	for _, c := range corners {
		dist := r - math.Hypot(x-c[0], y-c[1])
		if dist > best {
			best = dist
		}
	}
	return best
}

// downscale 用面积平均（盒式滤波）缩放；对透明像素做预乘平均，避免边缘发黑。
func downscale(src *image.RGBA, size int) *image.RGBA {
	sw := src.Bounds().Dx()
	sh := src.Bounds().Dy()
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	sx := float64(sw) / float64(size)
	sy := float64(sh) / float64(size)
	for ty := 0; ty < size; ty++ {
		y0 := int(float64(ty) * sy)
		y1 := int(float64(ty+1) * sy)
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for tx := 0; tx < size; tx++ {
			x0 := int(float64(tx) * sx)
			x1 := int(float64(tx+1) * sx)
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var sr, sg, sb, sa, n float64
			for y := y0; y < y1 && y < sh; y++ {
				for x := x0; x < x1 && x < sw; x++ {
					c := src.RGBAAt(x, y)
					a := float64(c.A) / 255
					sr += float64(c.R) * a
					sg += float64(c.G) * a
					sb += float64(c.B) * a
					sa += a
					n++
				}
			}
			if n == 0 || sa == 0 {
				continue
			}
			dst.SetRGBA(tx, ty, color.RGBA{
				R: clamp8(sr / sa),
				G: clamp8(sg / sa),
				B: clamp8(sb / sa),
				A: clamp8(sa / n * 255),
			})
		}
	}
	return dst
}

func clamp8(v float64) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	default:
		return uint8(math.Round(v))
	}
}

// bmpFrame 把图像编码成 ICO 内的 32 位 BMP 帧（BITMAPINFOHEADER + 自下而上 BGRA + 全零 AND 掩码）。
func bmpFrame(img *image.RGBA) []byte {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	stride := w * 4
	maskStride := ((w + 31) / 32) * 4

	buf := make([]byte, 40+stride*h+maskStride*h)
	le := binary.LittleEndian
	le.PutUint32(buf[0:], 40)          // biSize
	le.PutUint32(buf[4:], uint32(w))   // biWidth
	le.PutUint32(buf[8:], uint32(h*2)) // biHeight：XOR + AND
	le.PutUint16(buf[12:], 1)          // biPlanes
	le.PutUint16(buf[14:], 32)         // biBitCount
	le.PutUint32(buf[16:], 0)          // biCompression：BI_RGB
	le.PutUint32(buf[20:], uint32(stride*h))

	off := 40
	for y := h - 1; y >= 0; y-- { // 自下而上
		for x := 0; x < w; x++ {
			c := img.RGBAAt(x, y)
			buf[off+0] = c.B
			buf[off+1] = c.G
			buf[off+2] = c.R
			buf[off+3] = c.A
			off += 4
		}
	}
	// AND 掩码保持全零：32 位图标以 alpha 通道为准
	return buf
}

// buildICO 组装 ICONDIR + 各帧 ICONDIRENTRY + 帧数据。
func buildICO(sizes []int, frames [][]byte) []byte {
	le := binary.LittleEndian
	offset := 6 + 16*len(frames)
	out := make([]byte, offset)
	le.PutUint16(out[0:], 0) // reserved
	le.PutUint16(out[2:], 1) // type: icon
	le.PutUint16(out[4:], uint16(len(frames)))
	for i, frame := range frames {
		e := 6 + 16*i
		size := sizes[i]
		if size >= 256 {
			out[e+0] = 0
			out[e+1] = 0
		} else {
			out[e+0] = uint8(size)
			out[e+1] = uint8(size)
		}
		out[e+2] = 0                                // 调色板数
		out[e+3] = 0                                // 保留
		le.PutUint16(out[e+4:], 1)                  // 平面数
		le.PutUint16(out[e+6:], 32)                 // 位深
		le.PutUint32(out[e+8:], uint32(len(frame))) // 数据长度
		le.PutUint32(out[e+12:], uint32(offset))    // 数据偏移
		out = append(out, frame...)
		offset += len(frame)
	}
	return out
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	return enc.Encode(f, img)
}

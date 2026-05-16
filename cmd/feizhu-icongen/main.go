// feizhu-icongen：从 icon-src.png 生成 512² 的 macOS/Windows 用图标。
//
// 设计要点（此前「丑」的主要原因已修正）：
//  1) 源图常为「整块不透明底」——不能用 alpha 包围盒，否则整图被当成主体，猪头比例不变；
//  2) 用四边采样估计背景色，再按色差抠出前景包围盒，只围绕猪头缩放；
//  3) 去掉对整张图的浅粉 flood-fill，避免吃掉猪脸边缘、留下脏边；
//  4) 先在 1024² 上合成 + 圆角蒙版，再高质量缩到 512²，圆角轮廓更顺滑。
//
//	go run ./cmd/feizhu-icongen/ -src cmd/feizhu-client-ui/assets/icon-src.png -out cmd/feizhu-client-ui/assets/icon.png
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"

	"golang.org/x/image/draw"
)

func main() {
	srcPath := flag.String("src", "", "源 PNG（可不透明底；会按四边背景色估计前景）")
	outPath := flag.String("out", "", "输出 PNG 路径")
	scale := flag.Float64("scale", 1.3, "前景相对其包围盒的放大倍数（如 1.3 即加大 30%）")
	radiusFrac := flag.Float64("radius", 0.223, "圆角半径占边长的比例（约 0.22，接近常见 App 图标）")
	bgDist := flag.Float64("bgdist", 42, "与估计背景色的 RGB 欧氏距离小于该值视为背景（0–441 量级）")
	padFrac := flag.Float64("pad", 0.06, "前景放入画布时四周留白占边长比例（略「专业」呼吸边）")
	flag.Parse()
	if *srcPath == "" || *outPath == "" {
		flag.Usage()
		os.Exit(2)
	}
	const outSide = 512
	const workSide = 1024
	b, err := os.ReadFile(*srcPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	srcImg, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	src := toNRGBA(srcImg)
	killNearBlack(src, 18)

	bg := estimateBackgroundFromBorder(src)
	bounds := foregroundBounds(src, bg, *bgDist)
	if bounds.Empty() {
		fmt.Fprintln(os.Stderr, "无法检测前景，请检查源图")
		os.Exit(1)
	}
	// 若几乎整图都被判成前景，放宽背景阈值再试一次（背景与肤色太接近时）
	sb := src.Bounds()
	w0, h0 := sb.Dx(), sb.Dy()
	if bounds.Dx()*bounds.Dy() > w0*h0*85/100 {
		b2 := foregroundBounds(src, bg, *bgDist*0.65)
		if !b2.Empty() && b2.Dx()*b2.Dy() < bounds.Dx()*bounds.Dy() {
			bounds = b2
		}
	}
	// 略外扩 2px，避免抗锯齿被裁掉
	bounds = clampRectExpand(bounds, 2, sb)

	sub := src.SubImage(bounds).(*image.NRGBA)
	sw, sh := bounds.Dx(), bounds.Dy()
	tw := int(math.Round(float64(sw) * *scale))
	th := int(math.Round(float64(sh) * *scale))
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}
	scaled := image.NewNRGBA(image.Rect(0, 0, tw, th))
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), sub, sub.Bounds(), draw.Over, nil)

	inner := float64(workSide) * (1.0 - 2.0*(*padFrac))
	if inner < 64 {
		inner = 64
	}
	maxW := int(math.Floor(inner))
	maxH := maxW
	if tw > maxW || th > maxH {
		sf := math.Min(float64(maxW)/float64(tw), float64(maxH)/float64(th))
		tw2 := int(math.Round(float64(tw) * sf))
		th2 := int(math.Round(float64(th) * sf))
		if tw2 < 1 {
			tw2 = 1
		}
		if th2 < 1 {
			th2 = 1
		}
		fit := image.NewNRGBA(image.Rect(0, 0, tw2, th2))
		draw.CatmullRom.Scale(fit, fit.Bounds(), scaled, scaled.Bounds(), draw.Over, nil)
		scaled = fit
		tw, th = tw2, th2
	}

	work := image.NewNRGBA(image.Rect(0, 0, workSide, workSide))
	dx := (workSide - tw) / 2
	dy := (workSide - th) / 2
	draw.Draw(work, image.Rect(dx, dy, dx+tw, dy+th), scaled, image.Point{}, draw.Over)

	applyRoundRectMask(work, *radiusFrac)

	out := image.NewNRGBA(image.Rect(0, 0, outSide, outSide))
	draw.CatmullRom.Scale(out, out.Bounds(), work, work.Bounds(), draw.Over, nil)

	tmp := *outPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := png.Encode(f, out); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = f.Close()
	if err := os.Rename(tmp, *outPath); err != nil {
		_ = os.Remove(tmp)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("written", *outPath)
}

func toNRGBA(m image.Image) *image.NRGBA {
	b := m.Bounds()
	n := image.NewNRGBA(b)
	draw.Draw(n, b, m, b.Min, draw.Src)
	return n
}

func killNearBlack(img *image.NRGBA, thr uint8) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.NRGBAAt(x, y)
			if c.A == 0 {
				continue
			}
			if c.R < thr && c.G < thr && c.B < thr {
				img.SetNRGBA(x, y, color.NRGBA{A: 0})
			}
		}
	}
}

func estimateBackgroundFromBorder(img *image.NRGBA) color.NRGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 2 || h < 2 {
		return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	var sr, sg, sb, sa, n uint32
	sample := func(x, y int) {
		c := img.NRGBAAt(x, y)
		if c.A < 8 {
			return
		}
		sr += uint32(c.R)
		sg += uint32(c.G)
		sb += uint32(c.B)
		sa += uint32(c.A)
		n++
	}
	step := w / 32
	if step < 1 {
		step = 1
	}
	for x := b.Min.X; x < b.Max.X; x += step {
		sample(x, b.Min.Y)
		sample(x, b.Max.Y-1)
	}
	step = h / 32
	if step < 1 {
		step = 1
	}
	for y := b.Min.Y; y < b.Max.Y; y += step {
		sample(b.Min.X, y)
		sample(b.Max.X-1, y)
	}
	if n == 0 {
		return color.NRGBA{R: 250, G: 220, B: 230, A: 255}
	}
	return color.NRGBA{
		R: uint8(sr / n), G: uint8(sg / n), B: uint8(sb / n), A: uint8(sa / n),
	}
}

func colorDist2(a, b color.NRGBA) float64 {
	dr := float64(a.R) - float64(b.R)
	dg := float64(a.G) - float64(b.G)
	db := float64(a.B) - float64(b.B)
	return dr*dr + dg*dg + db*db
}

func foregroundBounds(img *image.NRGBA, bg color.NRGBA, thr float64) image.Rectangle {
	thr2 := thr * thr
	b := img.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.NRGBAAt(x, y)
			if c.A < 12 {
				continue
			}
			if colorDist2(c, bg) <= thr2 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if minX > maxX {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX+1, maxY+1)
}

func inRoundRect(x, y, w, h int, r float64) bool {
	xf := float64(x) + 0.5
	yf := float64(y) + 0.5
	wf := float64(w)
	hf := float64(h)
	if xf >= r && xf < wf-r {
		return true
	}
	if yf >= r && yf < hf-r {
		return true
	}
	if xf < r && yf < r {
		dx, dy := xf-r, yf-r
		return dx*dx+dy*dy < r*r+1e-6
	}
	if xf >= wf-r && yf < r {
		dx, dy := xf-(wf-r), yf-r
		return dx*dx+dy*dy < r*r+1e-6
	}
	if xf < r && yf >= hf-r {
		dx, dy := xf-r, yf-(hf-r)
		return dx*dx+dy*dy < r*r+1e-6
	}
	if xf >= wf-r && yf >= hf-r {
		dx, dy := xf-(wf-r), yf-(hf-r)
		return dx*dx+dy*dy < r*r+1e-6
	}
	return false
}

func applyRoundRectMask(img *image.NRGBA, radiusFrac float64) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	r := float64(min(w, h)) * radiusFrac
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			px, py := x-b.Min.X, y-b.Min.Y
			if !inRoundRect(px, py, w, h, r) {
				img.SetNRGBA(x, y, color.NRGBA{A: 0})
				continue
			}
			// 圆角内侧 1px 软边，减轻锯齿
			a := edgeSoftAlpha(px, py, w, h, r)
			if a < 255 {
				c := img.NRGBAAt(x, y)
				c.A = uint8(uint32(c.A) * uint32(a) / 255)
				img.SetNRGBA(x, y, c)
			}
		}
	}
}

// 在圆角弧附近将 alpha 略降，抗锯齿（仅处理靠外缘的窄带）。
func edgeSoftAlpha(px, py, w, h int, r float64) uint8 {
	const band = 1.25
	xf := float64(px) + 0.5
	yf := float64(py) + 0.5
	wf, hf := float64(w), float64(h)
	// 到「未圆角矩形」外缘的距离；圆角外已裁掉，这里只处理靠圆弧内侧
	var dist float64
	switch {
	case xf < r && yf < r:
		dx, dy := xf-r, yf-r
		dist = r - math.Sqrt(dx*dx+dy*dy)
	case xf >= wf-r && yf < r:
		dx, dy := xf-(wf-r), yf-r
		dist = r - math.Sqrt(dx*dx+dy*dy)
	case xf < r && yf >= hf-r:
		dx, dy := xf-r, yf-(hf-r)
		dist = r - math.Sqrt(dx*dx+dy*dy)
	case xf >= wf-r && yf >= hf-r:
		dx, dy := xf-(wf-r), yf-(hf-r)
		dist = r - math.Sqrt(dx*dx+dy*dy)
	default:
		return 255
	}
	if dist >= band {
		return 255
	}
	if dist <= 0 {
		return 0
	}
	// dist in (0, band) -> smoothstep to 255
	t := dist / band
	t = t * t * (3 - 2*t)
	return uint8(255 * t)
}

func clampRectExpand(r image.Rectangle, pad int, lim image.Rectangle) image.Rectangle {
	return image.Rect(
		imax(r.Min.X-pad, lim.Min.X),
		imax(r.Min.Y-pad, lim.Min.Y),
		imin(r.Max.X+pad, lim.Max.X),
		imin(r.Max.Y+pad, lim.Max.Y),
	)
}

func imin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func imax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

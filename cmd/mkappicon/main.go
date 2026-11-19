package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
)

const masterSize = 2048

type point struct {
	x, y float64
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: mkappicon ICONSET_DIRECTORY")
		os.Exit(2)
	}
	if err := writeIconset(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func writeIconset(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	mark := drawMark()
	sizes := []struct {
		name string
		size int
	}{
		{"icon_16x16.png", 16},
		{"icon_16x16@2x.png", 32},
		{"icon_32x32.png", 32},
		{"icon_32x32@2x.png", 64},
		{"icon_128x128.png", 128},
		{"icon_128x128@2x.png", 256},
		{"icon_256x256.png", 256},
		{"icon_256x256@2x.png", 512},
		{"icon_512x512.png", 512},
		{"icon_512x512@2x.png", 1024},
	}
	cache := map[int]*image.RGBA{masterSize: mark}
	for n := masterSize / 2; n >= 16; n /= 2 {
		cache[n] = downsample2(cache[n*2])
	}
	for _, icon := range sizes {
		f, err := os.Create(filepath.Join(dir, icon.name))
		if err != nil {
			return err
		}
		err = png.Encode(f, cache[icon.size])
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func drawMark() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, masterSize, masterSize))
	scale := float64(masterSize) / 24
	p := func(x, y float64) point { return point{x * scale, y * scale} }

	top, ur, lr := p(12, 1), p(21.5, 6.5), p(21.5, 17.5)
	bot, ll, ul := p(12, 23), p(2.5, 17.5), p(2.5, 6.5)
	mid := p(12, 12)
	fillPolygon(img, []point{top, ur, mid, ul}, color.RGBA{R: 0x6c, G: 0xc4, B: 0xf0, A: 255})
	fillPolygon(img, []point{ul, mid, bot, ll}, color.RGBA{R: 0x2e, G: 0x86, B: 0xc8, A: 255})
	fillPolygon(img, []point{mid, ur, lr, bot}, color.RGBA{R: 0x1b, G: 0x55, B: 0x8c, A: 255})

	fillEllipse(img, p(12, 6.5), 4.6*scale, 2.65*scale, color.RGBA{R: 0x16, G: 0x3d, B: 0x63, A: 255})
	fillEllipse(img, p(12, 7.4), 4.6*0.92*scale, 2.65*0.72*scale, color.RGBA{R: 0x0e, G: 0x2a, B: 0x47, A: 255})

	edge := color.RGBA{R: 255, G: 255, B: 255, A: 140}
	width := 0.6 * scale
	fillStroke(img, mid, ul, width, edge)
	fillStroke(img, mid, ur, width, edge)
	fillStroke(img, mid, bot, width, edge)
	return img
}

func fillEllipse(img *image.RGBA, center point, rx, ry float64, c color.RGBA) {
	pts := make([]point, 64)
	for i := range pts {
		a := 2 * math.Pi * float64(i) / float64(len(pts))
		pts[i] = point{center.x + rx*math.Cos(a), center.y + ry*math.Sin(a)}
	}
	fillPolygon(img, pts, c)
}

func fillStroke(img *image.RGBA, a, b point, width float64, c color.RGBA) {
	dx, dy := b.x-a.x, b.y-a.y
	length := math.Hypot(dx, dy)
	if length == 0 {
		return
	}
	nx, ny := -dy/length*width/2, dx/length*width/2
	fillPolygon(img, []point{
		{a.x + nx, a.y + ny}, {b.x + nx, b.y + ny},
		{b.x - nx, b.y - ny}, {a.x - nx, a.y - ny},
	}, c)
}

func fillPolygon(img *image.RGBA, pts []point, c color.RGBA) {
	minY, maxY := pts[0].y, pts[0].y
	for _, p := range pts[1:] {
		minY = math.Min(minY, p.y)
		maxY = math.Max(maxY, p.y)
	}
	y0 := max(0, int(math.Ceil(minY-0.5)))
	y1 := min(img.Bounds().Dy(), int(math.Ceil(maxY-0.5)))
	xs := make([]float64, 0, len(pts))
	for y := y0; y < y1; y++ {
		scanY := float64(y) + 0.5
		xs = xs[:0]
		for i, a := range pts {
			b := pts[(i+1)%len(pts)]
			if (a.y > scanY) == (b.y > scanY) {
				continue
			}
			xs = append(xs, a.x+(scanY-a.y)*(b.x-a.x)/(b.y-a.y))
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			x0 := max(0, int(math.Ceil(xs[i]-0.5)))
			x1 := min(img.Bounds().Dx(), int(math.Ceil(xs[i+1]-0.5)))
			for x := x0; x < x1; x++ {
				blend(img, x, y, c)
			}
		}
	}
}

func blend(img *image.RGBA, x, y int, src color.RGBA) {
	i := img.PixOffset(x, y)
	inv := 255 - uint32(src.A)
	img.Pix[i+0] = uint8(uint32(src.R)*uint32(src.A)/255 + uint32(img.Pix[i+0])*inv/255)
	img.Pix[i+1] = uint8(uint32(src.G)*uint32(src.A)/255 + uint32(img.Pix[i+1])*inv/255)
	img.Pix[i+2] = uint8(uint32(src.B)*uint32(src.A)/255 + uint32(img.Pix[i+2])*inv/255)
	img.Pix[i+3] = uint8(uint32(src.A) + uint32(img.Pix[i+3])*inv/255)
}

func downsample2(src *image.RGBA) *image.RGBA {
	w, h := src.Bounds().Dx()/2, src.Bounds().Dy()/2
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := src.PixOffset(x*2, y*2)
			j := dst.PixOffset(x, y)
			for c := 0; c < 4; c++ {
				sum := uint16(src.Pix[i+c]) +
					uint16(src.Pix[i+4+c]) +
					uint16(src.Pix[i+src.Stride+c]) +
					uint16(src.Pix[i+src.Stride+4+c])
				dst.Pix[j+c] = uint8(sum / 4)
			}
		}
	}
	return dst
}

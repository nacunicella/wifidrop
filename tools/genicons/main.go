// genicons renders the PWA icons as PNG files. Android requires PNG icons
// for the install prompt, so these are generated once and committed.
//
//	go run ./tools/genicons
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

var (
	teal  = color.RGBA{0x0f, 0x76, 0x6e, 0xff}
	white = color.RGBA{0xff, 0xff, 0xff, 0xff}
)

func main() {
	out := "web"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	must(write(filepath.Join(out, "icon-192.png"), render(192, false)))
	must(write(filepath.Join(out, "icon-512.png"), render(512, false)))
	must(write(filepath.Join(out, "icon-512-maskable.png"), render(512, true)))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func write(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// render draws a rounded teal square with two Wi-Fi arcs and an upward
// arrow, matching icon.svg. The maskable variant fills the whole square and
// keeps the glyph inside the safe zone.
func render(size int, maskable bool) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	radius := s * 14 / 64
	if maskable {
		radius = 0
	}
	scale := 1.0
	if maskable {
		scale = 0.78
	}
	// Transform from the 64x64 SVG coordinate space.
	tx := func(x, y float64) (float64, float64) {
		x = (x-32)*scale + 32
		y = (y-32)*scale + 32
		return x * s / 64, y * s / 64
	}
	stroke := func(w float64) float64 { return w * scale * s / 64 }

	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			x, y := float64(px)+0.5, float64(py)+0.5
			var c color.RGBA
			if insideRoundedRect(x, y, s, radius) {
				c = teal
			} else {
				img.Set(px, py, color.RGBA{})
				continue
			}
			// Arcs: centred at (32,42) in SVG space.
			cx, cy := tx(32, 42)
			d := math.Hypot(x-cx, y-cy)
			ang := math.Atan2(cy-y, x-cx) // 0..pi above centre
			inArc := func(r, w float64, cover float64) bool {
				rr := r * scale * s / 64
				return math.Abs(d-rr) <= w/2 && ang > math.Pi/2-cover && ang < math.Pi/2+cover
			}
			if inArc(22, stroke(4), 0.95) {
				c = blend(teal, white, 0.55)
			}
			if inArc(13, stroke(4), 0.95) {
				c = blend(teal, white, 0.8)
			}
			// Arrow shaft: x=32, y 38..52; arrow head lines from (25,44)-(32,37)-(39,44).
			if segDist(x, y, tx2(tx, 32, 38), tx2(tx, 32, 52)) <= stroke(5)/2 ||
				segDist(x, y, tx2(tx, 25, 44), tx2(tx, 32, 37)) <= stroke(5)/2 ||
				segDist(x, y, tx2(tx, 39, 44), tx2(tx, 32, 37)) <= stroke(5)/2 {
				c = white
			}
			img.Set(px, py, c)
		}
	}
	return img
}

type pt struct{ x, y float64 }

func tx2(tx func(x, y float64) (float64, float64), x, y float64) pt {
	a, b := tx(x, y)
	return pt{a, b}
}

func segDist(x, y float64, a, b pt) float64 {
	vx, vy := b.x-a.x, b.y-a.y
	wx, wy := x-a.x, y-a.y
	t := (vx*wx + vy*wy) / (vx*vx + vy*vy)
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return math.Hypot(x-(a.x+t*vx), y-(a.y+t*vy))
}

func insideRoundedRect(x, y, s, r float64) bool {
	if r == 0 {
		return true
	}
	if x < r && y < r {
		return math.Hypot(x-r, y-r) <= r
	}
	if x > s-r && y < r {
		return math.Hypot(x-(s-r), y-r) <= r
	}
	if x < r && y > s-r {
		return math.Hypot(x-r, y-(s-r)) <= r
	}
	if x > s-r && y > s-r {
		return math.Hypot(x-(s-r), y-(s-r)) <= r
	}
	return true
}

func blend(a, b color.RGBA, t float64) color.RGBA {
	f := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.RGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 0xff}
}

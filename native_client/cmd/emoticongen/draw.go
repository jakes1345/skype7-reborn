package main

import (
	"hash/fnv"
	"image"
	"image/color"
	"image/draw"
	"math"
)

const cell = 40

func inBounds(r image.Rectangle, x, y int) bool {
	return x >= r.Min.X && x < r.Max.X && y >= r.Min.Y && y < r.Max.Y
}

func fill(dst *image.NRGBA, c color.NRGBA) {
	draw.Draw(dst, dst.Bounds(), &image.Uniform{c}, image.Point{}, draw.Src)
}

func hashHue(s string) uint8 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return uint8(h.Sum32() % 200)
}

// drawStem draws one 40×40 tile for a logical emoticon stem (frame used for subtle motion in sheet).
func drawStem(dst *image.NRGBA, stem string, frame int) {
	fill(dst, color.NRGBA{245, 247, 250, 255})
	dx := (frame % 3) - 1
	switch stem {
	case "smile":
		drawFace(dst, color.NRGBA{255, 220, 140, 255}, true, false, 1+dx, 0)
	case "sad":
		drawFace(dst, color.NRGBA{255, 210, 160, 255}, false, true, dx, 0)
	case "laugh":
		drawFace(dst, color.NRGBA{255, 230, 150, 255}, true, false, dx, 2)
	case "wink":
		drawFace(dst, color.NRGBA{255, 220, 140, 255}, true, false, dx, 1)
	case "heart":
		drawHeart(dst, color.NRGBA{230, 40, 60, 255})
	case "kiss":
		drawFace(dst, color.NRGBA{255, 200, 200, 255}, true, false, dx, 3)
	case "cool":
		drawFace(dst, color.NRGBA{255, 210, 150, 255}, true, false, dx, 0)
		drawRect(dst, 6, 10, 34, 18, color.NRGBA{20, 20, 80, 220}) // shades
	case "tongue":
		drawFace(dst, color.NRGBA{255, 220, 140, 255}, true, false, dx, 4)
	case "surprised", "speechless":
		drawFace(dst, color.NRGBA{255, 220, 140, 255}, true, false, dx, 5)
	case "angry":
		drawFace(dst, color.NRGBA{255, 180, 150, 255}, true, false, dx, 6)
	case "cry", "crying":
		drawFace(dst, color.NRGBA{200, 220, 255, 255}, true, true, dx, 1)
		for i := 0; i < 3; i++ {
			filledCircle(dst, 14+i*6, 28, 1, color.NRGBA{80, 140, 255, 200})
		}
	case "bored", "dull":
		drawFace(dst, color.NRGBA{220, 220, 220, 255}, true, false, dx, 7)
	case "confused", "thinking", "typing":
		drawFace(dst, color.NRGBA{255, 225, 180, 255}, true, false, dx, 8)
	case "wonder":
		drawFace(dst, color.NRGBA{255, 235, 200, 255}, true, false, dx, 5)
	case "embarrassed":
		drawFace(dst, color.NRGBA{255, 190, 190, 255}, true, false, dx, 0)
		for x := 10; x < 30; x += 4 {
			filledCircle(dst, x, 16, 2, color.NRGBA{255, 80, 80, 120})
		}
	case "blush":
		drawFace(dst, color.NRGBA{255, 200, 200, 255}, true, false, dx, 0)
		filledCircle(dst, 10, 22, 5, color.NRGBA{255, 120, 140, 140})
		filledCircle(dst, 30, 22, 5, color.NRGBA{255, 120, 140, 140})
	case "devil":
		drawFace(dst, color.NRGBA{255, 160, 160, 255}, true, false, dx, 6)
		filledCircle(dst, 8, 6, 3, color.NRGBA{40, 40, 40, 255})
		filledCircle(dst, 32, 6, 3, color.NRGBA{40, 40, 40, 255})
	case "angel":
		drawFace(dst, color.NRGBA{255, 240, 220, 255}, true, false, dx, 1)
		drawRing(dst, 20, 6, 14, color.NRGBA{255, 255, 200, 200})
	case "coffee":
		drawMug(dst, color.NRGBA{90, 60, 40, 255})
	case "pizza":
		drawTriangle(dst, color.NRGBA{240, 180, 60, 255})
		filledCircle(dst, 20, 22, 6, color.NRGBA{220, 60, 40, 255})
	case "beer":
		drawGlass(dst, color.NRGBA{240, 200, 80, 255})
	case "drink":
		drawGlass(dst, color.NRGBA{180, 220, 255, 255})
	case "cake":
		drawRect(dst, 10, 18, 30, 32, color.NRGBA{250, 200, 160, 255})
		filledCircle(dst, 20, 14, 8, color.NRGBA{255, 80, 120, 255})
	case "sun":
		for a := 0; a < 8; a++ {
			ray(dst, 20, 20, float64(a)*math.Pi/4, 18, color.NRGBA{255, 210, 40, 255})
		}
		filledCircle(dst, 20, 20, 10, color.NRGBA{255, 230, 60, 255})
	case "moon":
		filledCircle(dst, 18, 16, 12, color.NRGBA{240, 240, 200, 255})
		filledCircle(dst, 24, 14, 12, color.NRGBA{245, 247, 250, 255})
	case "star":
		drawStar(dst, 20, 20, 14, color.NRGBA{255, 210, 40, 255})
	case "rain":
		for i := 0; i < 5; i++ {
			drawLine(dst, 4+i*8, 8+i*2, 10+i*8, 32, color.NRGBA{80, 140, 255, 200})
		}
	case "snow":
		for i := 0; i < 6; i++ {
			filledCircle(dst, 8+i*6, 10+(i%3)*8, 2, color.NRGBA{220, 240, 255, 255})
		}
	case "phone":
		drawRect(dst, 14, 8, 26, 32, color.NRGBA{40, 40, 50, 255})
		filledCircle(dst, 20, 28, 2, color.NRGBA{100, 255, 120, 255})
	case "music":
		drawNote(dst)
	case "movie":
		drawRect(dst, 8, 12, 32, 28, color.NRGBA{30, 30, 35, 255})
		filledCircle(dst, 16, 22, 3, color.NRGBA{255, 80, 80, 255})
	case "flower":
		for a := 0; a < 5; a++ {
			px := 20 + int(8*math.Cos(float64(a)*2*math.Pi/5))
			py := 20 + int(8*math.Sin(float64(a)*2*math.Pi/5))
			filledCircle(dst, px, py, 4, color.NRGBA{255, 120, 200, 255})
		}
		filledCircle(dst, 20, 20, 4, color.NRGBA{255, 220, 80, 255})
	case "rose":
		drawRect(dst, 18, 14, 22, 34, color.NRGBA{40, 120, 60, 255})
		filledCircle(dst, 20, 12, 8, color.NRGBA{220, 40, 80, 255})
	case "brokenheart":
		drawHeart(dst, color.NRGBA{80, 80, 90, 255})
		drawLine(dst, 12, 12, 28, 28, color.NRGBA{200, 40, 40, 255})
	case "clap":
		drawHands(dst, true)
	case "handshake":
		drawHands(dst, false)
	case "muscle", "flex":
		drawRect(dst, 14, 10, 26, 30, color.NRGBA{255, 200, 160, 255})
		drawLine(dst, 10, 16, 30, 12, color.NRGBA{60, 60, 60, 255})
	case "thumbsup":
		drawThumb(dst, true)
	case "thumbsdown":
		drawThumb(dst, false)
	case "bow":
		drawArc(dst, 20, 22, 16, color.NRGBA{60, 60, 80, 255})
	case "wave":
		drawLine(dst, 8, 24, 32, 10, color.NRGBA{255, 200, 140, 255})
		drawLine(dst, 8, 20, 32, 8, color.NRGBA{255, 200, 140, 255})
	case "hug":
		drawCurveHug(dst)
	case "finger", "toivo", "tmi", "fubar", "wtf":
		drawBadge(dst, stem, color.NRGBA{200, 40, 40, 255})
	case "rock":
		drawStar(dst, 20, 20, 10, color.NRGBA{200, 100, 240, 255})
	case "poolparty":
		fill(dst, color.NRGBA{120, 200, 255, 255})
		filledCircle(dst, 20, 22, 10, color.NRGBA{255, 220, 100, 255})
	case "mooning":
		drawBadge(dst, "!", color.NRGBA{255, 200, 120, 255})
	case "bug":
		drawBug(dst)
	case "drunk":
		drawSpiral(dst, frame)
	case "bandit":
		drawRect(dst, 8, 14, 32, 26, color.NRGBA{40, 40, 40, 255})
		drawRect(dst, 10, 18, 30, 8, color.NRGBA{255, 255, 255, 200})
	case "sweat":
		drawFace(dst, color.NRGBA{255, 220, 140, 255}, true, false, dx, 0)
		drawLine(dst, 28, 8, 24, 18, color.NRGBA{80, 160, 255, 200})
	case "cheeky":
		drawFace(dst, color.NRGBA{255, 220, 140, 255}, true, false, dx, 4)
	case "sleepy", "yawn":
		drawFace(dst, color.NRGBA{230, 230, 250, 255}, true, false, dx, 7)
		drawLine(dst, 14, 26, 26, 26, color.NRGBA{80, 80, 120, 255})
	case "inlove":
		drawFace(dst, color.NRGBA{255, 200, 210, 255}, true, false, dx, 9)
	case "evilgrin", "egrin":
		drawFace(dst, color.NRGBA{200, 240, 200, 255}, true, false, dx, 2)
	case "party":
		drawConfetti(dst, frame)
	case "dance":
		drawStickDance(dst, frame)
	case "punch":
		drawLine(dst, 10, 20, 30, 12, color.NRGBA{180, 80, 40, 255})
	case "highfive":
		drawLine(dst, 8, 28, 32, 8, color.NRGBA{255, 180, 100, 255})
	case "nod":
		drawArrow(dst, 20, 12, 20, 28, color.NRGBA{60, 160, 80, 255})
	case "shake":
		drawArrow(dst, 20, 28, 20, 12, color.NRGBA{200, 80, 60, 255})
	case "wait":
		drawClock(dst, frame, color.NRGBA{100, 100, 120, 255})
	case "mail", "e":
		drawRect(dst, 10, 12, 30, 26, color.NRGBA{240, 240, 245, 255})
		drawLine(dst, 14, 18, 26, 18, color.NRGBA{60, 60, 80, 255})
	case "idea", "i":
		drawBulb(dst)
	case "time", "clock":
		drawClock(dst, frame, color.NRGBA{60, 60, 80, 255})
	case "laptop", "computer", "work":
		drawRect(dst, 8, 14, 32, 26, color.NRGBA{70, 75, 90, 255})
		drawRect(dst, 10, 16, 28, 18, color.NRGBA{180, 200, 255, 255})
	case "cat":
		drawAnimal(dst, "C", color.NRGBA{200, 200, 210, 255})
	case "dog":
		drawAnimal(dst, "D", color.NRGBA{210, 170, 120, 255})
	case "snail":
		drawSpiral(dst, frame)
	case "cash":
		drawRect(dst, 12, 14, 28, 26, color.NRGBA{60, 140, 80, 255})
		drawLine(dst, 22, 16, 22, 28, color.NRGBA{255, 240, 200, 255})
		drawLine(dst, 18, 20, 26, 20, color.NRGBA{255, 240, 200, 255})
	case "bank":
		drawRect(dst, 8, 16, 32, 28, color.NRGBA{100, 100, 120, 255})
		drawRect(dst, 12, 20, 24, 8, color.NRGBA{255, 220, 120, 255})
	case "pig":
		drawAnimal(dst, "P", color.NRGBA{255, 180, 210, 255})
	case "bike":
		drawCircleOutline(dst, 12, 24, 6, color.NRGBA{60, 60, 60, 255})
		drawCircleOutline(dst, 28, 24, 6, color.NRGBA{60, 60, 60, 255})
		drawLine(dst, 12, 24, 28, 24, color.NRGBA{80, 80, 120, 255})
	case "car":
		drawRect(dst, 8, 18, 32, 28, color.NRGBA{80, 120, 200, 255})
		filledCircle(dst, 12, 28, 3, color.NRGBA{40, 40, 40, 255})
		filledCircle(dst, 28, 28, 3, color.NRGBA{40, 40, 40, 255})
	case "plane":
		drawLine(dst, 8, 20, 32, 16, color.NRGBA{120, 160, 220, 255})
		drawLine(dst, 20, 12, 20, 26, color.NRGBA{120, 160, 220, 255})
	case "gift", "present":
		drawRect(dst, 12, 14, 28, 30, color.NRGBA{220, 80, 80, 255})
		drawRect(dst, 8, 18, 32, 8, color.NRGBA{255, 220, 100, 255})
	case "monkey":
		drawAnimal(dst, "M", color.NRGBA{180, 140, 100, 255})
	case "ninja":
		drawRect(dst, 10, 12, 30, 22, color.NRGBA{40, 40, 50, 255})
		drawLine(dst, 8, 14, 32, 10, color.NRGBA{200, 200, 200, 255})
	case "emo":
		drawHair(dst, color.NRGBA{40, 40, 50, 255})
	case "headbang":
		drawLine(dst, 20, 8, 20+(dx*2), 30, color.NRGBA{120, 80, 60, 255})
	case "whew", "rofl", "giggle", "chuckle":
		drawFace(dst, color.NRGBA{255, 230, 180, 255}, true, false, dx, 2)
	case "puke", "sick":
		fill(dst, color.NRGBA{220, 255, 200, 255})
		drawFace(dst, color.NRGBA{200, 240, 200, 255}, true, false, dx, 5)
	case "doh":
		drawFace(dst, color.NRGBA{255, 220, 160, 255}, true, false, dx, 6)
		drawLine(dst, 10, 8, 30, 12, color.NRGBA{60, 60, 60, 255})
	case "bomb":
		filledCircle(dst, 22, 18, 10, color.NRGBA{40, 40, 40, 255})
		drawLine(dst, 22, 8, 26, 4, color.NRGBA{255, 200, 80, 255})
	case "skull":
		drawSkull(dst)
	case "ghost":
		drawGhost(dst)
	case "rainbow":
		for i := 0; i < 7; i++ {
			drawRect(dst, 4+i*5, 12, 9+i*5, 28, color.NRGBA{uint8(255 - i*30), uint8(60 + i*25), uint8(200 - i*20), 255})
		}
	case "phaze_logo_small":
		fill(dst, color.NRGBA{0, 175, 240, 255})
		drawRect(dst, 10, 12, 30, 28, color.NRGBA{255, 255, 255, 255})
	default:
		h := hashHue(stem)
		fill(dst, color.NRGBA{h, 200 - h/2, 240 - h, 255})
		filledCircle(dst, 20, 20, 12, color.NRGBA{255, 255, 255, 220})
		abbrev := stem
		if len(abbrev) > 2 {
			abbrev = abbrev[:2]
		}
		drawMiniText(dst, abbrev, color.NRGBA{40, 40, 60, 255})
	}
}

func drawFace(dst *image.NRGBA, skin color.NRGBA, eyesOpen, tear bool, dx, mouth int) {
	filledCircle(dst, 20, 20, 16, skin)
	eyeL, eyeR := 14+dx, 26+dx
	if eyesOpen {
		filledCircle(dst, eyeL, 16, 2, color.NRGBA{40, 40, 50, 255})
		filledCircle(dst, eyeR, 16, 2, color.NRGBA{40, 40, 50, 255})
	} else {
		drawLine(dst, eyeL-2, 16, eyeL+2, 16, color.NRGBA{40, 40, 50, 255})
		drawLine(dst, eyeR-2, 16, eyeR+2, 16, color.NRGBA{40, 40, 50, 255})
	}
	if tear {
		filledCircle(dst, eyeL, 20, 2, color.NRGBA{100, 160, 255, 200})
	}
	switch mouth {
	case 0: // neutral
		drawLine(dst, 14, 24, 26, 24, color.NRGBA{80, 40, 40, 255})
	case 1: // sad
		drawArcLow(dst, 20, 22, 8, color.NRGBA{80, 40, 40, 255})
	case 2: // big smile
		drawArcHigh(dst, 20, 20, 10, color.NRGBA{120, 40, 40, 255})
	case 3: // kiss
		drawLine(dst, 18, 24, 24, 24, color.NRGBA{200, 80, 120, 255})
	case 4: // tongue
		drawArcHigh(dst, 20, 18, 8, color.NRGBA{80, 40, 40, 255})
		filledCircle(dst, 20, 28, 4, color.NRGBA{255, 100, 140, 255})
	case 5: // O mouth
		drawCircleOutline(dst, 20, 24, 5, color.NRGBA{60, 40, 40, 255})
	case 6: // angry
		drawLine(dst, 12, 14, 18, 18, color.NRGBA{60, 40, 40, 255})
		drawLine(dst, 28, 14, 22, 18, color.NRGBA{60, 40, 40, 255})
		drawLine(dst, 14, 26, 26, 26, color.NRGBA{80, 40, 40, 255})
	case 7: // flat
		drawLine(dst, 14, 24, 26, 24, color.NRGBA{80, 40, 40, 255})
	case 8: // tilt
		drawLine(dst, 15, 24, 27, 22, color.NRGBA{80, 40, 40, 255})
	case 9: // heart eyes
		drawHeartAt(dst, 14, 14, 4, color.NRGBA{230, 60, 90, 255})
		drawHeartAt(dst, 26, 14, 4, color.NRGBA{230, 60, 90, 255})
		drawArcHigh(dst, 20, 20, 8, color.NRGBA{120, 40, 40, 255})
	default:
		drawArcHigh(dst, 20, 20, 8, color.NRGBA{120, 40, 40, 255})
	}
}

func filledCircle(dst *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if x*x+y*y <= r*r {
				px, py := cx+x, cy+y
				if inBounds(dst.Bounds(), px, py) {
					dst.Set(px, py, c)
				}
			}
		}
	}
}

func drawCircleOutline(dst *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	for t := 0; t < 360; t += 6 {
		a := float64(t) * math.Pi / 180
		px := int(float64(cx) + float64(r)*math.Cos(a))
		py := int(float64(cy) + float64(r)*math.Sin(a))
		if inBounds(dst.Bounds(), px, py) {
			dst.Set(px, py, c)
		}
	}
}

func drawRect(dst *image.NRGBA, x0, y0, x1, y1 int, c color.NRGBA) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			if inBounds(dst.Bounds(), x, y) {
				dst.Set(x, y, c)
			}
		}
	}
}

func drawLine(dst *image.NRGBA, x0, y0, x1, y1 int, c color.NRGBA) {
	dx := abs(x1 - x0)
	dy := abs(y1 - y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy
	x, y := x0, y0
	for {
		if inBounds(dst.Bounds(), x, y) {
			dst.Set(x, y, c)
		}
		if x == x1 && y == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x += sx
		}
		if e2 < dx {
			err += dx
			y += sy
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func drawArcHigh(dst *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	for t := 200; t <= 340; t += 4 {
		a := float64(t) * math.Pi / 180
		px := int(float64(cx) + float64(r)*math.Cos(a))
		py := int(float64(cy) + float64(r)*math.Sin(a))
		if inBounds(dst.Bounds(), px, py) {
			dst.Set(px, py, c)
		}
	}
}

func drawArcLow(dst *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	for t := 20; t <= 160; t += 4 {
		a := float64(t) * math.Pi / 180
		px := int(float64(cx) + float64(r)*math.Cos(a))
		py := int(float64(cy) + float64(r)*math.Sin(a))
		if inBounds(dst.Bounds(), px, py) {
			dst.Set(px, py, c)
		}
	}
}

func drawArc(dst *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	drawCircleOutline(dst, cx, cy, r, c)
}

func drawHeart(dst *image.NRGBA, c color.NRGBA) {
	drawHeartAt(dst, 20, 18, 12, c)
}

func drawHeartAt(dst *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	if r < 3 {
		r = 3
	}
	filledCircle(dst, cx-r/2, cy, r*2/3, c)
	filledCircle(dst, cx+r/2, cy, r*2/3, c)
	for y := cy; y <= cy+r; y++ {
		w := (y - cy) * (r/2 + 1) / (r/2 + 2)
		if w < 0 {
			w = 0
		}
		for x := cx - w; x <= cx+w; x++ {
			if inBounds(dst.Bounds(), x, y) {
				dst.Set(x, y, c)
			}
		}
	}
}

func drawRing(dst *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	drawCircleOutline(dst, cx, cy, r, c)
	drawCircleOutline(dst, cx, cy, r-2, color.NRGBA{245, 247, 250, 255})
}

func drawMug(dst *image.NRGBA, c color.NRGBA) {
	drawRect(dst, 12, 14, 28, 30, c)
	drawRect(dst, 28, 18, 34, 24, c)
	filledCircle(dst, 18, 20, 3, color.NRGBA{240, 240, 250, 200})
}

func drawGlass(dst *image.NRGBA, c color.NRGBA) {
	drawRect(dst, 14, 10, 26, 30, c)
	drawLine(dst, 14, 10, 26, 10, color.NRGBA{200, 220, 255, 255})
}

func drawTriangle(dst *image.NRGBA, c color.NRGBA) {
	for y := 14; y <= 30; y++ {
		w := (y - 14) * 14 / 16
		for x := 20 - w; x <= 20+w; x++ {
			if inBounds(dst.Bounds(), x, y) {
				dst.Set(x, y, c)
			}
		}
	}
}

func ray(dst *image.NRGBA, cx, cy int, ang float64, ln int, c color.NRGBA) {
	for t := 2; t < ln; t++ {
		px := int(float64(cx) + float64(t)*math.Cos(ang))
		py := int(float64(cy) + float64(t)*math.Sin(ang))
		if inBounds(dst.Bounds(), px, py) {
			dst.Set(px, py, c)
		}
	}
}

func drawStar(dst *image.NRGBA, cx, cy, r int, c color.NRGBA) {
	for k := 0; k < 5; k++ {
		a0 := float64(k)*2*math.Pi/5 - math.Pi/2
		a1 := float64(k+1)*2*math.Pi/5 - math.Pi/2
		drawLine(dst,
			int(float64(cx)+float64(r)*math.Cos(a0)),
			int(float64(cy)+float64(r)*math.Sin(a0)),
			int(float64(cx)+float64(r/2)*math.Cos(a1)),
			int(float64(cy)+float64(r/2)*math.Sin(a1)),
			c)
	}
}

func drawNote(dst *image.NRGBA) {
	drawRect(dst, 14, 8, 20, 26, color.NRGBA{40, 40, 50, 255})
	drawRect(dst, 18, 10, 28, 14, color.NRGBA{40, 40, 50, 255})
}

func drawHands(dst *image.NRGBA, clap bool) {
	c := color.NRGBA{255, 210, 170, 255}
	filledCircle(dst, 14, 22, 8, c)
	filledCircle(dst, 26, 22, 8, c)
	if clap {
		drawLine(dst, 14, 14, 26, 14, color.NRGBA{200, 200, 200, 255})
	}
}

func drawThumb(dst *image.NRGBA, up bool) {
	drawRect(dst, 18, 10, 24, 28, color.NRGBA{255, 210, 170, 255})
	if up {
		drawRect(dst, 16, 8, 22, 14, color.NRGBA{255, 210, 170, 255})
	} else {
		drawRect(dst, 16, 24, 22, 30, color.NRGBA{255, 210, 170, 255})
	}
}

func drawCurveHug(dst *image.NRGBA) {
	drawArc(dst, 14, 24, 10, color.NRGBA{255, 200, 160, 255})
	drawArc(dst, 26, 24, 10, color.NRGBA{255, 200, 160, 255})
}

func drawBadge(dst *image.NRGBA, label string, bg color.NRGBA) {
	if label == "" {
		label = "?"
	}
	filledCircle(dst, 20, 20, 16, bg)
	tw := min(2, len(label))
	drawMiniText(dst, label[:tw], color.NRGBA{255, 255, 255, 255})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func drawText(dst *image.NRGBA, s string, c color.NRGBA) {
	drawMiniText(dst, s, c)
}

func drawMiniText(dst *image.NRGBA, s string, c color.NRGBA) {
	// 5×7 crude bitmap for [A-Z0-9] only; fallback block
	x0, y0 := 14, 14
	for i, ch := range s {
		if i > 1 {
			break
		}
		ox := x0 + i*10
		drawRect(dst, ox, y0, ox+6, y0+10, c)
		_ = ch
	}
}

func drawBug(dst *image.NRGBA) {
	drawLine(dst, 10, 20, 30, 20, color.NRGBA{60, 60, 40, 255})
	filledCircle(dst, 8, 18, 3, color.NRGBA{40, 40, 40, 255})
	filledCircle(dst, 32, 18, 3, color.NRGBA{40, 40, 40, 255})
}

func drawSpiral(dst *image.NRGBA, frame int) {
	cx, cy := 20, 20
	for t := 0; t < 120; t++ {
		a := float64(t+frame*4) / 8
		r := float64(t) / 15
		px := int(float64(cx) + r*math.Cos(a))
		py := int(float64(cy) + r*math.Sin(a))
		if inBounds(dst.Bounds(), px, py) {
			dst.Set(px, py, color.NRGBA{120, 100, 180, 255})
		}
	}
}

func drawConfetti(dst *image.NRGBA, frame int) {
	fill(dst, color.NRGBA{40, 44, 60, 255})
	for i := 0; i < 12; i++ {
		x := (i*7 + frame*3) % 36
		y := (i*5 + frame) % 36
		drawRect(dst, 2+x, 2+y, 4+x, 5+y, color.NRGBA{uint8(100 + i*10), uint8(200 - i*8), uint8(150 + i*5), 255})
	}
}

func drawStickDance(dst *image.NRGBA, frame int) {
	d := frame % 4
	drawLine(dst, 20, 10, 20, 26, color.NRGBA{60, 60, 80, 255})
	drawLine(dst, 20, 16, 12+d*2, 20, color.NRGBA{60, 60, 80, 255})
	drawLine(dst, 20, 16, 28-d*2, 20, color.NRGBA{60, 60, 80, 255})
}

func drawArrow(dst *image.NRGBA, x0, y0, x1, y1 int, c color.NRGBA) {
	drawLine(dst, x0, y0, x1, y1, c)
}

func drawClock(dst *image.NRGBA, frame int, c color.NRGBA) {
	drawCircleOutline(dst, 20, 20, 14, c)
	a := float64(frame%60) / 60 * 2 * math.Pi
	drawLine(dst, 20, 20, 20+int(9*math.Sin(a)), 20-int(9*math.Cos(a)), color.NRGBA{200, 80, 80, 255})
}

func drawBulb(dst *image.NRGBA) {
	filledCircle(dst, 20, 14, 10, color.NRGBA{255, 240, 160, 255})
	drawRect(dst, 16, 22, 24, 32, color.NRGBA{160, 160, 170, 255})
}

func drawAnimal(dst *image.NRGBA, letter string, fur color.NRGBA) {
	filledCircle(dst, 20, 22, 12, fur)
	drawMiniText(dst, letter, color.NRGBA{40, 40, 50, 255})
}

func drawHair(dst *image.NRGBA, c color.NRGBA) {
	for x := 8; x < 32; x++ {
		drawLine(dst, x, 8, x+4, 22, c)
	}
}

func drawSkull(dst *image.NRGBA) {
	filledCircle(dst, 20, 20, 12, color.NRGBA{240, 240, 240, 255})
	filledCircle(dst, 16, 18, 2, color.NRGBA{40, 40, 40, 255})
	filledCircle(dst, 24, 18, 2, color.NRGBA{40, 40, 40, 255})
	drawLine(dst, 16, 26, 24, 26, color.NRGBA{40, 40, 40, 255})
}

func drawGhost(dst *image.NRGBA) {
	filledCircle(dst, 20, 16, 10, color.NRGBA{240, 240, 250, 255})
	for x := 10; x < 30; x += 4 {
		drawLine(dst, x, 22, x+2, 34, color.NRGBA{230, 230, 245, 255})
	}
}

package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
)

func main() {
	img := image.NewRGBA(image.Rect(0, 0, 100, 20))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.Transparent}, image.Point{}, draw.Src)

	// Colors for Forensic Skype 7 Status Dots
	online := color.RGBA{125, 190, 0, 255}
	away := color.RGBA{255, 200, 0, 255}
	dnd := color.RGBA{230, 0, 0, 255}
	offline := color.RGBA{180, 180, 180, 255}

	drawDot(img, 0, 0, online)
	drawDot(img, 14, 0, away)
	drawDot(img, 28, 0, dnd)
	drawDot(img, 42, 0, offline)

	f, _ := os.Create("assets/phaze7_master.png")
	defer f.Close()
	png.Encode(f, img)
}

func drawDot(img *image.RGBA, x, y int, c color.Color) {
	for i := 0; i < 12; i++ {
		for j := 0; j < 12; j++ {
			img.Set(x+i, y+j, c)
		}
	}
}

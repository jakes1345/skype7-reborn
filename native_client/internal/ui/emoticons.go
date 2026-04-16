package ui

import (
	"image"
	_ "image/png"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

type AnimatedEmoji struct {
	widget.BaseWidget
	imageObj *canvas.Image
	frames   int
	current  int
}

// EmoticonMap defines the coordinates in the master spritesheet
// Format: Shortcut -> {X, Y, Frames}
var EmoticonMap = map[string]struct {
	X, Y, Frames int
}{
	"(smile)": {0, 0, 32},
	"(sad)":   {40, 0, 32},
	"(wink)":  {80, 0, 32},
	"(laugh)": {120, 0, 32},
	"(cool)":  {160, 0, 32},
	"(cake)":  {200, 40, 1}, // Example static
}

func NewAnimatedEmoji(shortcut string) *AnimatedEmoji {
	coords, ok := EmoticonMap[shortcut]
	if !ok {
		return nil
	}
	
	f, err := os.Open("assets/ui_master_spritesheet.png")
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, _ := image.Decode(f)

	// Tazher 7 emojis were often 20x20 or 40x40 clusters in a strip
	// Let's assume 40x40 for high fidelity
	frameSize := 40
	e := &AnimatedEmoji{
		frames: coords.Frames,
	}
	e.ExtendBaseWidget(e)

	// Animation loop
	go func() {
		ticker := time.NewTicker(80 * time.Millisecond)
		for range ticker.C {
			e.current = (e.current + 1) % e.frames
			
			// Crop current frame relative to starting coords
			rect := image.Rect(coords.X, coords.Y + (e.current*frameSize), coords.X + frameSize, coords.Y + ((e.current+1)*frameSize))
			if sub, ok := img.(interface {
				SubImage(r image.Rectangle) image.Image
			}); ok {
				e.imageObj = canvas.NewImageFromImage(sub.SubImage(rect))
				e.imageObj.FillMode = canvas.ImageFillContain
				e.Refresh()
			}
		}
	}()

	return e
}

func (e *AnimatedEmoji) CreateRenderer() fyne.WidgetRenderer {
	return &emojiRenderer{emoji: e}
}

type emojiRenderer struct {
	emoji *AnimatedEmoji
}

func (r *emojiRenderer) Layout(size fyne.Size) {
	r.emoji.imageObj.Resize(size)
}

func (r *emojiRenderer) MinSize() fyne.Size {
	return fyne.NewSize(40, 40)
}

func (r *emojiRenderer) Refresh() {
	// Fyne automatically handles object refresh
}

func (r *emojiRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.emoji.imageObj}
}

func (r *emojiRenderer) Destroy() {}

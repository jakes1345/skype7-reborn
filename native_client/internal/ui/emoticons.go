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

func NewAnimatedEmoji(path string) *AnimatedEmoji {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, _ := image.Decode(f)

	// Skype 7 emojis were often 20x20 or 40x40 clusters in a strip
	// Let's assume 40x40 for high fidelity
	frameSize := 40
	if img.Bounds().Dx() < 40 {
		frameSize = img.Bounds().Dx()
	}
	frameCount := img.Bounds().Dy() / frameSize

	e := &AnimatedEmoji{
		frames: frameCount,
	}
	e.ExtendBaseWidget(e)

	// Animation loop
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		for range ticker.C {
			e.current = (e.current + 1) % e.frames
			
			// Crop current frame
			rect := image.Rect(0, e.current*frameSize, frameSize, (e.current+1)*frameSize)
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

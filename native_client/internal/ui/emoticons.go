package ui

import (
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
	slicer   *AeroSlicer
	shortcut string
}

// EmoticonMap defines the coordinates in the master spritesheet
var EmoticonMap = map[string]struct {
	X, Y, Frames int
}{
	"(smile)":     {0, 0, 32},
	"(sad)":       {40, 0, 32},
	"(wink)":      {80, 0, 32},
	"(laugh)":     {120, 0, 32},
	"(cool)":      {160, 0, 32},
	"(surprised)": {200, 0, 32},
	"(crying)":    {280, 0, 32},
	"(sweat)":     {320, 0, 32},
	"(kiss)":      {360, 0, 32},
	"(cheeky)":    {400, 0, 32},
	"(blush)":     {440, 0, 32},
	"(sleepy)":    {480, 0, 32},
	"(dull)":      {520, 0, 32},
	"(inlove)":    {560, 0, 32},
	"(egrin)":     {600, 0, 32},
	"(party)":     {640, 0, 32},
	"(beer)":      {680, 40, 1},
	"(dance)":     {720, 0, 32},
	"(rock)":      {760, 0, 32},
	"(punch)":     {800, 0, 32},
	"(flex)":      {840, 0, 32},
	"(highfive)":  {880, 0, 32},
}

func NewAnimatedEmoji(shortcut string, slicer *AeroSlicer) *AnimatedEmoji {
	coords, ok := EmoticonMap[shortcut]
	if !ok || slicer == nil {
		return nil
	}

	e := &AnimatedEmoji{
		frames:   coords.Frames,
		slicer:   slicer,
		shortcut: shortcut,
		imageObj: canvas.NewImageFromResource(slicer.Slice(shortcut+"_0", coords.X, coords.Y, 40, 40)),
	}
	e.imageObj.FillMode = canvas.ImageFillContain
	e.ExtendBaseWidget(e)

	// Animation loop logic - only start if multiple frames
	if e.frames > 1 {
		go func() {
			ticker := time.NewTicker(80 * time.Millisecond)
			for range ticker.C {
				e.current = (e.current + 1) % e.frames
				e.imageObj.Resource = slicer.Slice(shortcut, coords.X, coords.Y+(e.current*40), 40, 40)
				e.imageObj.Refresh()
			}
		}()
	}

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
	return fyne.NewSize(32, 32)
}

func (r *emojiRenderer) Refresh() {
	r.emoji.imageObj.Refresh()
}

func (r *emojiRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.emoji.imageObj}
}

func (r *emojiRenderer) Destroy() {}

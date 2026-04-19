package ui

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// PhazePulsar is the forensic three-dot typing animation from Skype 7.
type PhazePulsar struct {
	Container *fyne.Container
	dots      [3]*canvas.Circle
	stop      chan struct{}
}

func NewPhazePulsar() *PhazePulsar {
	dotColor := color.NRGBA{R: 180, G: 180, B: 180, A: 255}
	var dots [3]*canvas.Circle
	for i := range dots {
		dots[i] = canvas.NewCircle(dotColor)
		dots[i].Resize(fyne.NewSize(6, 6))
	}

	p := &PhazePulsar{
		dots:      dots,
		stop:      make(chan struct{}),
		Container: container.NewHBox(dots[0], dots[1], dots[2]),
	}
	p.Container.Hide() // Hidden by default

	return p
}

func (p *PhazePulsar) Start() {
	p.Container.Show()
	go func() {
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		for {
			select {
			case <-p.stop:
				return
			case <-ticker.C:
				// Reset all
				for _, d := range p.dots {
					d.FillColor = color.NRGBA{R: 180, G: 180, B: 180, A: 100}
					d.Refresh()
				}
				// Pulse the current one
				p.dots[i%3].FillColor = color.NRGBA{R: 120, G: 120, B: 120, A: 255}
				p.dots[i%3].Refresh()
				i++
			}
		}
	}()
}

func (p *PhazePulsar) Stop() {
	select {
	case p.stop <- struct{}{}:
	default:
	}
	p.Container.Hide()
}

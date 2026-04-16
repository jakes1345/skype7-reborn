package ui

import (
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// CallOverlay represents the premium Obsidian call interface
type CallOverlay struct {
	PeerName   string
	AvatarPath string
	IsIncoming bool
	OnAnswer   func()
	OnReject   func()
	OnHangup   func()
	OnMute     func(bool)

	root       *fyne.Container
	status     *widget.Label
	timer      *widget.Label
	startTime  time.Time
	isMuted    bool
	stopAnim   chan bool
}

func NewCallOverlay(name, avatar string, incoming bool) *CallOverlay {
	return &CallOverlay{
		PeerName:   name,
		AvatarPath: avatar,
		IsIncoming: incoming,
	}
}

func (c *CallOverlay) Render() fyne.CanvasObject {
	// Dark Glassmorphism Background
	bg := canvas.NewRectangle(color.NRGBA{R: 20, G: 20, B: 25, A: 240})
	
	// Subtle gradient border
	border := canvas.NewRectangle(color.Transparent)
	border.StrokeColor = color.NRGBA{R: 100, G: 100, B: 255, A: 50}
	border.StrokeWidth = 2
	
	// Large Avatar
	peerAvatar := NewAvatarWithStatus(120, "Busy", c.AvatarPath)
	
	// Pulse Effect (Glow)
	glow := canvas.NewCircle(color.NRGBA{R: 80, G: 120, B: 255, A: 0})
	glow.Resize(fyne.NewSize(140, 140))
	glow.Move(fyne.NewPos(-10, -10)) // Relative to avatar container
	
	avatarContainer := container.NewStack(glow, peerAvatar)
	
	c.stopAnim = make(chan bool)
	go c.animatePulse(glow)
	
	nameLabel := widget.NewLabelWithStyle(c.PeerName, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	nameLabel.Importance = widget.HighImportance

	statusText := "Calling..."
	if c.IsIncoming {
		statusText = "Incoming Call"
	}
	c.status = widget.NewLabelWithStyle(statusText, fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
	
	c.timer = widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Monospace: true})
	c.timer.Hide()

	// Action Buttons
	var buttons *fyne.Container
	if c.IsIncoming {
		answerBtn := widget.NewButtonWithIcon("Answer", theme.ConfirmIcon(), c.OnAnswer)
		answerBtn.Importance = widget.HighImportance
		
		rejectBtn := widget.NewButtonWithIcon("Reject", theme.CancelIcon(), c.OnReject)
		rejectBtn.Importance = widget.DangerImportance

		buttons = container.NewHBox(layout.NewSpacer(), answerBtn, rejectBtn, layout.NewSpacer())
	} else {
		hangupBtn := widget.NewButtonWithIcon("Hang Up", theme.CancelIcon(), c.OnHangup)
		hangupBtn.Importance = widget.DangerImportance

		muteBtn := widget.NewButtonWithIcon("", theme.VolumeUpIcon(), func() {
			c.isMuted = !c.isMuted
			if c.OnMute != nil {
				c.OnMute(c.isMuted)
			}
		})
		
		buttons = container.NewHBox(layout.NewSpacer(), muteBtn, hangupBtn, layout.NewSpacer())
	}

	content := container.NewVBox(
		layout.NewSpacer(),
		container.NewCenter(avatarContainer),
		nameLabel,
		c.status,
		c.timer,
		layout.NewSpacer(),
		container.NewPadded(buttons),
		layout.NewSpacer(),
	)

	c.root = container.NewStack(bg, border, content)
	return c.root
}

func (c *CallOverlay) animatePulse(glow *canvas.Circle) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var step float64
	for {
		select {
		case <-c.stopAnim:
			glow.Hide()
			return
		case <-ticker.C:
			step += 0.1
			// Sinusoidal pulse for alpha (0.1 to 0.4 opacity)
			alpha := uint8(50 + 50*math.Sin(step))
			glow.FillColor = color.NRGBA{R: 80, G: 120, B: 255, A: alpha}
			glow.Refresh()
		}
	}
}

func (c *CallOverlay) StartTimer() {
	c.startTime = time.Now()
	c.status.SetText("In Call")
	c.timer.Show()
	go func() {
		for {
			dur := time.Since(c.startTime).Round(time.Second)
			c.timer.SetText(dur.String())
			time.Sleep(1 * time.Second)
		}
	}()
}

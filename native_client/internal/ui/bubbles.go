package ui

import (
	"image/color"
	"regexp"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// Map of Tazher 7 emoticon tokens → asset filename (relative to assets/).
// Covers the classic full set. Missing assets fall back to the literal text.
var emoticonMap = map[string]string{
	"(smile)":      "emoticon_smile.png",
	":)":           "emoticon_smile.png",
	":-)":          "emoticon_smile.png",
	"(sad)":        "emoticon_sad.png",
	":(":           "emoticon_sad.png",
	":-(":          "emoticon_sad.png",
	"(laugh)":      "emoticon_laugh.png",
	":D":           "emoticon_laugh.png",
	":-D":          "emoticon_laugh.png",
	"(wink)":       "emoticon_wink.png",
	";)":           "emoticon_wink.png",
	";-)":          "emoticon_wink.png",
	"(heart)":      "emoticon_heart.png",
	"<3":           "emoticon_heart.png",
	"(kiss)":       "emoticon_kiss.png",
	":*":           "emoticon_kiss.png",
	":-*":          "emoticon_kiss.png",
	"(cool)":       "emoticon_cool.png",
	"8-)":          "emoticon_cool.png",
	"(tongue)":     "emoticon_tongue.png",
	":p":           "emoticon_tongue.png",
	":P":           "emoticon_tongue.png",
	":-p":          "emoticon_tongue.png",
	":-P":          "emoticon_tongue.png",
	"(surprised)":  "emoticon_surprised.png",
	":o":           "emoticon_surprised.png",
	":O":           "emoticon_surprised.png",
	"(angry)":      "emoticon_angry.png",
	"x(":           "emoticon_angry.png",
	"X(":           "emoticon_angry.png",
	"(cry)":        "emoticon_cry.png",
	":'(":          "emoticon_cry.png",
	"(bored)":      "emoticon_bored.png",
	"|-)":          "emoticon_bored.png",
	"(confused)":   "emoticon_confused.png",
	":S":           "emoticon_confused.png",
	":s":           "emoticon_confused.png",
	"(embarrassed)": "emoticon_embarrassed.png",
	":$":           "emoticon_embarrassed.png",
	"(devil)":      "emoticon_devil.png",
	"(angel)":      "emoticon_angel.png",
	"(coffee)":     "emoticon_coffee.png",
	"(pizza)":      "emoticon_pizza.png",
	"(beer)":       "emoticon_beer.png",
	"(drink)":      "emoticon_drink.png",
	"(cake)":       "emoticon_cake.png",
	"(sun)":        "emoticon_sun.png",
	"(moon)":       "emoticon_moon.png",
	"(star)":       "emoticon_star.png",
	"(rain)":       "emoticon_rain.png",
	"(snow)":       "emoticon_snow.png",
	"(phone)":      "emoticon_phone.png",
	"(music)":      "emoticon_music.png",
	"(movie)":      "emoticon_movie.png",
	"(flower)":     "emoticon_flower.png",
	"(rose)":       "emoticon_rose.png",
	"(brokenheart)": "emoticon_brokenheart.png",
	"(u)":          "emoticon_brokenheart.png",
	"(clap)":       "emoticon_clap.png",
	"(handshake)":  "emoticon_handshake.png",
	"(muscle)":     "emoticon_muscle.png",
	"(thumbsup)":   "emoticon_thumbsup.png",
	"(y)":          "emoticon_thumbsup.png",
	"(thumbsdown)": "emoticon_thumbsdown.png",
	"(n)":          "emoticon_thumbsdown.png",
	"(bow)":        "emoticon_bow.png",
	"(wave)":       "emoticon_wave.png",
	"(hug)":        "emoticon_hug.png",
	// --- Hidden / Party Emoticons ---
	"(finger)":     "emoticon_finger.png",
	"(toivo)":      "emoticon_toivo.png",
	"(rock)":       "emoticon_rock.png",
	"(poolparty)":  "emoticon_poolparty.png",
	"(mooning)":    "emoticon_mooning.png",
	"(bug)":        "emoticon_bug.png",
	"(drunk)":      "emoticon_drunk.png",
	"(bandit)":     "emoticon_bandit.png",
	"(tazher)":      "tazher_logo_small.png",
	"(tmi)":        "emoticon_tmi.png",
	"(fubar)":      "emoticon_fubar.png",
}

// Precomputed regex that matches any token, longest-first so "(smile)" wins over "(s".
var emoticonRegex = func() *regexp.Regexp {
	keys := make([]string, 0, len(emoticonMap))
	for k := range emoticonMap {
		keys = append(keys, regexp.QuoteMeta(k))
	}
	// Sort longest first
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if len(keys[j]) > len(keys[i]) {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return regexp.MustCompile("(" + strings.Join(keys, "|") + ")")
}()

func parseRichText(text string) []fyne.CanvasObject {
	var objects []fyne.CanvasObject
	pos := 0
	matches := emoticonRegex.FindAllStringIndex(text, -1)
	for _, m := range matches {
		start, end := m[0], m[1]
		if start > pos {
			objects = append(objects, widget.NewLabel(text[pos:start]))
		}
		tok := text[start:end]
		if path, ok := emoticonMap[tok]; ok {
			icon := canvas.NewImageFromFile("assets/" + path)
			icon.SetMinSize(fyne.NewSize(19, 19))
			icon.FillMode = canvas.ImageFillContain
			objects = append(objects, icon)
		} else {
			objects = append(objects, widget.NewLabel(tok))
		}
		pos = end
	}
	if pos < len(text) {
		objects = append(objects, widget.NewLabel(text[pos:]))
	}
	if len(objects) == 0 {
		objects = append(objects, widget.NewLabel(text))
	}
	return objects
}

func NewMessageBubble(author, text string, isMe bool) fyne.CanvasObject {
	bubbleColor := color.NRGBA{R: 240, G: 240, B: 240, A: 255}
	if isMe {
		bubbleColor = color.NRGBA{R: 225, G: 245, B: 255, A: 255}
	}

	bg := canvas.NewRectangle(bubbleColor)

	nameLabel := widget.NewLabelWithStyle(author, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	bodyContent := container.NewHBox(parseRichText(text)...)

	content := container.NewVBox(nameLabel, bodyContent)
	return container.NewStack(bg, container.NewPadded(content))
}

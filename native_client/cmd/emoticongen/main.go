// emoticongen draws Phaze-original PNG emoticons, branding images, and a master spritesheet
// aligned with internal/ui/emoticons.go EmoticonMap (keep coordinates in sync when editing).
//
// Usage (from native_client): go run ./cmd/emoticongen [phaze_assets_root]
// Default phaze_assets_root = assets  (i.e. native_client/assets)
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

// sheetLayout mirrors ui.EmoticonMap — update both when changing animation cells.
var sheetLayout = []struct {
	key    string
	x, y   int
	frames int
}{
	{"(smile)", 0, 0, 32},
	{"(sad)", 40, 0, 32},
	{"(wink)", 80, 0, 32},
	{"(laugh)", 120, 0, 32},
	{"(cool)", 160, 0, 32},
	{"(surprised)", 200, 0, 32},
	{"(crying)", 280, 0, 32},
	{"(sweat)", 320, 0, 32},
	{"(kiss)", 360, 0, 32},
	{"(cheeky)", 400, 0, 32},
	{"(blush)", 440, 0, 32},
	{"(sleepy)", 480, 0, 32},
	{"(dull)", 520, 0, 32},
	{"(inlove)", 560, 0, 32},
	{"(egrin)", 600, 0, 32},
	{"(party)", 640, 0, 32},
	{"(beer)", 680, 40, 1},
	{"(dance)", 720, 0, 32},
	{"(rock)", 760, 0, 32},
	{"(punch)", 800, 0, 32},
	{"(flex)", 840, 0, 32},
	{"(highfive)", 880, 0, 32},
}

// sheetStem maps picker key → drawStem name (must match PNG stem without emoticon_ prefix).
var sheetStem = map[string]string{
	"(smile)": "smile", "(sad)": "sad", "(wink)": "wink", "(laugh)": "laugh",
	"(cool)": "cool", "(surprised)": "surprised", "(crying)": "cry", "(sweat)": "sweat",
	"(kiss)": "kiss", "(cheeky)": "cheeky", "(blush)": "blush", "(sleepy)": "sleepy",
	"(dull)": "bored", "(inlove)": "inlove", "(egrin)": "evilgrin", "(party)": "party",
	"(beer)": "beer", "(dance)": "dance", "(rock)": "rock", "(punch)": "punch",
	"(flex)": "muscle", "(highfive)": "highfive",
}

var allStems = []string{
	"angel", "angry", "bandit", "bank", "beer", "bike", "bomb", "bow", "brokenheart",
	"bug", "car", "cash", "cat", "cheeky", "chuckle", "clap", "coffee", "cake", "confused",
	"cry", "cool", "dance", "devil", "dog", "doh", "drink", "drunk", "embarrassed", "emo",
	"evilgrin", "finger", "flower", "fubar", "ghost", "giggle", "gift", "handshake", "headbang",
	"heart", "highfive", "hug", "idea", "inlove", "kiss", "laugh", "laptop", "mail", "monkey",
	"moon", "mooning", "movie", "music", "muscle", "ninja", "nod", "party", "phaze_logo_small",
	"pig", "plane", "poolparty", "punch", "puke", "rain", "rainbow", "rock", "rose", "rofl",
	"sad", "shake", "sick", "skull", "sleepy", "smile", "snail", "snow", "speechless", "star",
	"sun", "surprised", "sweat", "thinking", "thumbsdown", "thumbsup", "time", "tmi", "toivo",
	"tongue", "wait", "wave", "whew", "wink", "wonder", "wtf", "yawn",
}

func main() {
	root := filepath.Join("assets")
	if len(os.Args) > 1 && os.Args[1] != "" {
		root = os.Args[1]
	}
	emDir := filepath.Join(root, "emoticons")
	must(os.MkdirAll(emDir, 0755))

	stems := uniqueStems()
	for _, s := range stems {
		img := image.NewNRGBA(image.Rect(0, 0, cell, cell))
		drawStem(img, s, 0)
		fn := stemFilename(s)
		mustWritePNG(filepath.Join(emDir, fn), img)
		fmt.Println("  Generated:", filepath.Join("emoticons", fn))
	}

	writeBranding(root)
	mustWritePNG(filepath.Join(root, "ui_master_spritesheet.png"), buildMasterSheet())
	fmt.Println("  Generated: ui_master_spritesheet.png")
	fmt.Println("All emoticon assets in", root)
}

func uniqueStems() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range allStems {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func stemFilename(stem string) string {
	if stem == "phaze_logo_small" {
		return "phaze_logo_small.png"
	}
	return "emoticon_" + stem + ".png"
}

func buildMasterSheet() *image.NRGBA {
	const W = 960
	const H = 1400
	sheet := image.NewNRGBA(image.Rect(0, 0, W, H))
	fill(sheet, color.NRGBA{52, 56, 64, 255})
	for _, e := range sheetLayout {
		stem := sheetStem[e.key]
		if stem == "" {
			stem = "smile"
		}
		for f := 0; f < e.frames; f++ {
			tile := image.NewNRGBA(image.Rect(0, 0, cell, cell))
			drawStem(tile, stem, f)
			r := image.Rect(e.x, e.y+f*cell, e.x+cell, e.y+(f+1)*cell)
			draw.Draw(sheet, r, tile, image.Point{}, draw.Src)
		}
	}
	return sheet
}

func writeBranding(root string) {
	// App icon 128×128
	icon := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	fill(icon, color.NRGBA{0, 175, 240, 255})
	drawRect(icon, 32, 32, 96, 96, color.NRGBA{255, 255, 255, 255})
	mustWritePNG(filepath.Join(root, "Icon.png"), icon)
	fmt.Println("  Generated: Icon.png")

	logo := image.NewNRGBA(image.Rect(0, 0, 240, 56))
	fill(logo, color.NRGBA{0, 175, 240, 255})
	for x := 16; x < 220; x += 40 {
		drawRect(logo, x, 18, x+28, 38, color.NRGBA{255, 255, 255, 220})
	}
	mustWritePNG(filepath.Join(root, "phaze_logo.png"), logo)
	fmt.Println("  Generated: phaze_logo.png")

	av := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	fill(av, color.NRGBA{210, 220, 230, 255})
	filledCircle(av, 64, 48, 28, color.NRGBA{255, 210, 180, 255})
	filledCircle(av, 64, 100, 40, color.NRGBA{180, 200, 220, 255})
	mustWritePNG(filepath.Join(root, "default_avatar.png"), av)
	fmt.Println("  Generated: default_avatar.png")
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func mustWritePNG(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}

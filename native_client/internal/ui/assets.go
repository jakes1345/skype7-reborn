package ui

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"image"
	"image/png"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

var (
	vaultCache = make(map[string][]byte)
	vaultKey   = []byte("PHAZ3-S0V3R31GN-F0R3NS1C-KV-2026")
)

func UnlockVault() error {
	data, err := os.ReadFile("assets.vault")
	if err != nil {
		return err
	}

	block, _ := aes.NewCipher(vaultKey)
	gcm, _ := cipher.NewGCM(block)
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return io.ErrUnexpectedEOF
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return err
	}

	gr, _ := gzip.NewReader(bytes.NewReader(plaintext))
	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		buf := new(bytes.Buffer)
		io.Copy(buf, tr)
		vaultCache[header.Name] = buf.Bytes()
	}
	return nil
}

// ResolveAsset finds an asset by trying CWD first, then the directory of
// the running binary, then $Phaze_ASSETS. This makes the client work
// whether launched via `./phaze`, a desktop shortcut, or a system path.
func GetAssetResource(rel string) fyne.Resource {
	rel = strings.TrimPrefix(rel, "assets/")
	if data, ok := vaultCache[rel]; ok {
		return fyne.NewStaticResource(rel, data)
	}
	// Fallback to legacy path if vault is not loaded
	path := ResolveAsset("assets/" + rel)
	if data, err := os.ReadFile(path); err == nil {
		return fyne.NewStaticResource(rel, data)
	}
	return theme.DefaultTheme().Icon(theme.IconNameHelp)
}

func ResolveAsset(rel string) string {
	if _, err := os.Stat(rel); err == nil {
		return rel
	}
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), rel)
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	if base := os.Getenv("Phaze_ASSETS"); base != "" {
		cand := filepath.Join(base, rel)
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	return rel // fall back; caller will see the open error
}

// VaultSoundBytes returns WAV bytes for name (e.g. "Login.wav") from the decrypted vault,
// trying common tar entry prefixes. Playback uses this when assets/sounds is missing on disk.
func VaultSoundBytes(name string) ([]byte, bool) {
	if name == "" || strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return nil, false
	}
	keys := []string{
		path.Join("sounds", name),
		path.Join("assets", "sounds", name),
	}
	for _, k := range keys {
		if b, ok := vaultCache[k]; ok && len(b) > 0 {
			return b, true
		}
	}
	return nil, false
}

// ReadAssetRaw loads bytes for a path under assets/ (e.g. "emoticons/emoticon_smile.png")
// from the vault or filesystem. Used for emoticons when avoiding the default-theme placeholder.
func ReadAssetRaw(rel string) ([]byte, bool) {
	rel = strings.TrimPrefix(rel, "assets/")
	if strings.Contains(rel, "..") {
		return nil, false
	}
	rel = path.Clean("/" + rel)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, false
	}
	tryKeys := []string{rel}
	if b := path.Base(rel); b != "" && b != "." && b != "/" {
		tryKeys = append(tryKeys, path.Join("emoticons", b))
	}
	for _, k := range tryKeys {
		if b, ok := vaultCache[k]; ok && len(b) > 64 {
			return b, true
		}
	}
	p := ResolveAsset("assets/" + rel)
	data, err := os.ReadFile(p)
	if err == nil && len(data) > 64 {
		return data, true
	}
	return nil, false
}

// AeroSlicer extracts pixel-perfect UI elements from the original Phaze 7
// master sprite sheet.
type AeroSlicer struct {
	MasterSheet image.Image
}

func NewAeroSlicer(path string) (*AeroSlicer, error) {
	res := GetAssetResource(path)
	img, _, err := image.Decode(bytes.NewReader(res.Content()))
	if err != nil {
		return nil, err
	}
	return &AeroSlicer{MasterSheet: img}, nil
}

// Slice crops a rect from the master sheet and re-encodes it as a PNG
// packaged in a fyne.Resource ready for widgets/icons.
func (a *AeroSlicer) Slice(name string, x, y, w, h int) fyne.Resource {
	if a.MasterSheet == nil {
		return nil
	}
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	si, ok := a.MasterSheet.(subImager)
	if !ok {
		return nil
	}
	sub := si.SubImage(image.Rect(x, y, x+w, y+h))

	var buf bytes.Buffer
	if err := png.Encode(&buf, sub); err != nil {
		return nil
	}
	return fyne.NewStaticResource(name, buf.Bytes())
}

// GetStatusIcon returns the Phaze 7 presence dot for a given state.
// Coordinates correspond to the 12x12 dots strip on the master spritesheet.
func (a *AeroSlicer) GetStatusIcon(presence string) fyne.Resource {
	var x int
	switch presence {
	case "Online":
		x = 0
	case "Away":
		x = 14
	case "Do Not Disturb", "DND":
		x = 28
	default:
		x = 42 // Offline / Invisible
	}
	return a.Slice("status_"+presence+".png", x, 0, 12, 12)
}

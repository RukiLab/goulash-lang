//go:build gui

package gui

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// System font discovery.
//
// Text is rendered with an OS system font; no font file is bundled.
// GOULASH_FONT (path to a .ttf/.otf/.ttc file) overrides discovery.
// Otherwise the first readable, parseable candidate for the current OS
// wins; Japanese-capable fonts are listed before Latin-only fallbacks.
const fontEnvOverride = "GOULASH_FONT"

// systemFontCandidates lists font files in preference order.
func systemFontCandidates() []string {
	switch runtime.GOOS {
	case "windows":
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		dir := filepath.Join(root, "Fonts")
		names := []string{
			"YuGothR.ttc",       // Yu Gothic Regular (default Japanese UI font)
			"meiryo.ttc",        // Meiryo
			"NotoSansJP-VF.ttf", // Noto Sans JP (variable font, default instance)
			"BIZ-UDGothicR.ttc", // BIZ UD Gothic
			"msgothic.ttc",      // MS Gothic
			"msmincho.ttc",      // MS Mincho
			"YuGothM.ttc",       // Yu Gothic Medium
			"segoeui.ttf",       // Latin-only last resort
		}
		out := make([]string, 0, len(names))
		for _, n := range names {
			out = append(out, filepath.Join(dir, n))
		}
		return out
	case "darwin":
		return []string{
			"/System/Library/Fonts/ヒラギノ角ゴシック W3.ttc",
			"/System/Library/Fonts/Hiragino Sans GB.ttc",
			"/Library/Fonts/Hiragino Sans GB.ttc",
			"/System/Library/Fonts/Supplemental/NotoSansJP-Regular.otf",
			"/System/Library/Fonts/Helvetica.ttc",
		}
	default: // linux and other unix-likes
		return []string{
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/opentype/noto/NotoSerifCJK-Regular.ttc",
			"/usr/share/fonts/truetype/noto/NotoSansJP-Regular.ttf",
			"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		}
	}
}

var (
	fontMu   sync.Mutex
	fontSrc  *text.GoTextFaceSource
	fontPath string
	fontErr  error
	fontDone bool
)

// systemFontSource loads and caches the system font source.
func systemFontSource() (*text.GoTextFaceSource, string, error) {
	fontMu.Lock()
	defer fontMu.Unlock()
	if fontDone {
		return fontSrc, fontPath, fontErr
	}
	fontDone = true
	if p := os.Getenv(fontEnvOverride); p != "" {
		src, err := loadFontFile(p)
		if err != nil {
			fontErr = fmt.Errorf("font: GOULASH_FONT %q: %w", p, err)
			return nil, "", fontErr
		}
		fontSrc, fontPath = src, p
		return fontSrc, fontPath, nil
	}
	for _, p := range systemFontCandidates() {
		src, err := loadFontFile(p)
		if err != nil {
			continue
		}
		fontSrc, fontPath = src, p
		return fontSrc, fontPath, nil
	}
	fontErr = fmt.Errorf("font: no usable system font found (set GOULASH_FONT to a .ttf/.otf/.ttc file)")
	return nil, "", fontErr
}

// loadFontFile parses one font file; .ttc collections yield their
// first face (conventionally the Regular weight).
func loadFontFile(path string) (*text.GoTextFaceSource, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Ext(path), ".ttc") {
		srcs, err := text.NewGoTextFaceSourcesFromCollection(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		if len(srcs) == 0 {
			return nil, fmt.Errorf("font: empty collection %q", path)
		}
		return srcs[0], nil
	}
	return text.NewGoTextFaceSource(bytes.NewReader(b))
}

// loadFace builds a text face of the given pixel size from the cached
// system font.
func loadFace(size float64) (*text.GoTextFace, error) {
	src, _, err := systemFontSource()
	if err != nil {
		return nil, err
	}
	return &text.GoTextFace{Source: src, Size: size}, nil
}

// fontDirs lists directories searched for bare font file names.
func fontDirs() []string {
	switch runtime.GOOS {
	case "windows":
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		return []string{filepath.Join(root, "Fonts")}
	case "darwin":
		return []string{
			"/System/Library/Fonts",
			"/System/Library/Fonts/Supplemental",
			"/Library/Fonts",
			filepath.Join(os.Getenv("HOME"), "Library/Fonts"),
		}
	default:
		home := os.Getenv("HOME")
		return []string{
			"/usr/share/fonts",
			"/usr/local/share/fonts",
			filepath.Join(home, ".fonts"),
			filepath.Join(home, ".local/share/fonts"),
		}
	}
}

// resolveFontSpec resolves a font() typeface spec to a file path: an
// existing path is used as-is, otherwise bare names (with or without
// extension) are searched in the system font directories
// (case-insensitive base-name match; extension tried when omitted).
func resolveFontSpec(spec string) (string, error) {
	if spec == "" {
		return "", fmt.Errorf("font: 空のフォント指定です")
	}
	if isFontPath(spec) {
		if _, err := os.Stat(spec); err != nil {
			return "", fmt.Errorf("font: フォントファイル %q が見つかりません", spec)
		}
		return spec, nil
	}
	exts := []string{""}
	if filepath.Ext(spec) == "" {
		exts = []string{".ttc", ".ttf", ".otf"}
	}
	for _, dir := range fontDirs() {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			for _, ext := range exts {
				if strings.EqualFold(name, spec+ext) {
					return filepath.Join(dir, name), nil
				}
			}
		}
	}
	return "", fmt.Errorf("font: フォント %q がシステムフォント内に見つかりません", spec)
}

// isFontPath reports whether spec looks like a file path rather than a
// bare font name: absolute paths, paths with separators, or an
// extension-bearing name that exists relative to the working directory.
func isFontPath(spec string) bool {
	if filepath.IsAbs(spec) || strings.ContainsAny(spec, `/\`) {
		return true
	}
	if filepath.Ext(spec) != "" {
		if _, err := os.Stat(spec); err == nil {
			return true
		}
	}
	return false
}

// loadFontSpec parses a resolved-or-bare font spec into a face source.
func loadFontSpec(spec string) (*text.GoTextFaceSource, string, error) {
	path, err := resolveFontSpec(spec)
	if err != nil {
		return nil, "", err
	}
	src, err := loadFontFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("font: %q を読み込めません: %w", path, err)
	}
	return src, path, nil
}

// FontPath reports the system font file in use ("" if none loaded).
// Handy for diagnostics and for tests to assert discovery works.
func FontPath() string {
	src, p, _ := systemFontSource()
	if src == nil {
		return ""
	}
	return p
}

package gui

import (
	"fmt"
	"image"
	"io/fs"

	"github.com/hajimehoshi/ebiten/v2"
)

// Dropped files (A7). ebiten reports a virtual FS snapshot per Update
// tick (nil when idle), so pumpDrops collects names on arrival and
// dropfiles() consumes them. The snapshot is retained for dropload.

// pumpDrops snapshots newly dropped file names. Called from Update.
func (b *WindowBackend) pumpDrops() {
	fsys := ebiten.DroppedFiles()
	if fsys == nil {
		return
	}
	var names []string
	_ = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil // best effort; skip unreadable entries
		}
		if path != "." {
			names = append(names, path)
		}
		return nil
	})
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dropFS = fsys
	b.dropNames = append(b.dropNames, names...)
}

// DropFiles returns pending dropped file names (consumed).
func (b *WindowBackend) DropFiles() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	names := b.dropNames
	b.dropNames = nil
	return names
}

// DropLoad decodes a dropped file (by name from dropfiles) into a new
// buffer, returning its id.
func (b *WindowBackend) DropLoad(name string) (int, error) {
	b.mu.Lock()
	fsys := b.dropFS
	b.mu.Unlock()
	if fsys == nil {
		return 0, fmt.Errorf("dropload：ドロップされたファイルがありません")
	}
	f, err := fsys.Open(name)
	if err != nil {
		return 0, fmt.Errorf("dropload：%s", err.Error())
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return 0, fmt.Errorf("dropload：%s", err.Error())
	}
	return b.allocUpload(src)
}

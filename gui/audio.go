package gui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/audio/mp3"
	"github.com/hajimehoshi/ebiten/v2/audio/vorbis"
	"github.com/hajimehoshi/ebiten/v2/audio/wav"
)

// Sound model (G2): files decode fully to PCM at load; players are created
// per mmplay so sounds restart and overlap freely. Loop uses InfiniteLoop.

const audioRate = 44100

type sound struct {
	pcm []byte
	vol float64 // 0..1, applied to players at play time
}

type audioState struct {
	mu      sync.Mutex
	sounds  map[int]*sound
	players map[int]*audio.Player
	nextID  int
}

// sharedCtx is the single process-wide audio context: ebiten allows only
// one (like the single game loop), and tests create multiple backends.
var sharedCtx struct {
	sync.Mutex
	ctx *audio.Context
}

func sharedContext() *audio.Context {
	sharedCtx.Lock()
	defer sharedCtx.Unlock()
	if sharedCtx.ctx == nil {
		sharedCtx.ctx = audio.NewContext(audioRate)
	}
	return sharedCtx.ctx
}

func (b *WindowBackend) audioState() *audioState {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.audio == nil {
		b.audio = &audioState{sounds: map[int]*sound{}, players: map[int]*audio.Player{}}
	}
	return b.audio
}

// MmLoad decodes wav/mp3/ogg into memory, returning a sound id.
func (b *WindowBackend) MmLoad(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("mmload：%s", err.Error())
	}
	var stream io.Reader
	switch strings.ToLower(filepath.Ext(path)) {
	case ".wav":
		stream, err = wav.DecodeWithSampleRate(audioRate, bytes.NewReader(data))
	case ".mp3":
		stream, err = mp3.DecodeWithSampleRate(audioRate, bytes.NewReader(data))
	case ".ogg":
		stream, err = vorbis.DecodeWithSampleRate(audioRate, bytes.NewReader(data))
	default:
		return 0, fmt.Errorf("mmload：サポートされていない形式 %q です（.wav/.mp3/.ogg が必要です）", filepath.Ext(path))
	}
	if err != nil {
		return 0, fmt.Errorf("mmload：%s", err.Error())
	}
	pcm, err := io.ReadAll(stream)
	if err != nil {
		return 0, fmt.Errorf("mmload：%s", err.Error())
	}
	st := b.audioState()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.nextID++
	st.sounds[st.nextID] = &sound{pcm: pcm, vol: 1}
	return st.nextID, nil
}

// MmPlay plays a sound (loop repeats forever). Restarts any current play.
func (b *WindowBackend) MmPlay(id int, loop bool) error {
	st := b.audioState()
	st.mu.Lock()
	s, ok := st.sounds[id]
	if !ok {
		st.mu.Unlock()
		return fmt.Errorf("mmplay：不明なサウンド %d です", id)
	}
	ctx := sharedContext()
	if p, ok := st.players[id]; ok {
		st.mu.Unlock()
		p.Close()
		st.mu.Lock()
	}
	var src io.Reader = bytes.NewReader(s.pcm)
	if loop {
		src = audio.NewInfiniteLoop(bytes.NewReader(s.pcm), int64(len(s.pcm)))
	}
	p, err := audio.NewPlayer(ctx, src)
	if err != nil {
		st.mu.Unlock()
		return fmt.Errorf("mmplay：%s", err.Error())
	}
	p.SetVolume(s.vol)
	st.players[id] = p
	st.mu.Unlock()
	p.Play()
	return nil
}

// MmVolume sets a sound's volume (0..100 percent). It applies to the
// live player if any, and is stored for future plays. Player.SetVolume
// is safe for concurrent use, so this never blocks the script.
func (b *WindowBackend) MmVolume(id, vol int) error {
	if vol < 0 || vol > 100 {
		return fmt.Errorf("mmvol：音量は 0 から 100 の範囲で指定してください。%d が指定されました", vol)
	}
	st := b.audioState()
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.sounds[id]
	if !ok {
		return fmt.Errorf("mmvol：不明なサウンド %d です", id)
	}
	s.vol = float64(vol) / 100
	if p, ok := st.players[id]; ok {
		p.SetVolume(s.vol)
	}
	return nil
}

// MmStop stops one sound (nil = all). Unknown ids are a silent no-op.
func (b *WindowBackend) MmStop(id *int) {
	st := b.audioState()
	st.mu.Lock()
	defer st.mu.Unlock()
	stop := func(p *audio.Player) { p.Close() }
	if id == nil {
		for sid, p := range st.players {
			stop(p)
			delete(st.players, sid)
		}
		return
	}
	if p, ok := st.players[*id]; ok {
		stop(p)
		delete(st.players, *id)
	}
}

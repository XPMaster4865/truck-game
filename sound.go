package main

import (
	"math"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"

	"github.com/hajimehoshi/ebiten/v2/audio"
)

const sampleRate = 44100

// sirenStream synthesises a UK-style two-tone emergency siren. Its volume is
// set every frame from how close the nearest emergency vehicle is.
type sirenStream struct {
	target atomic.Uint64 // float64 bits
	pan    atomic.Uint64 // float64 bits, -1 left .. 1 right
	vol    float64
	phase  float64
	pos    int64
}

func (s *sirenStream) Read(b []byte) (int, error) {
	n := len(b) / 4 * 4
	target := math.Float64frombits(s.target.Load())
	pan := math.Float64frombits(s.pan.Load())
	lg, rg := math.Min(1, 1-pan), math.Min(1, 1+pan)
	for i := 0; i < n; i += 4 {
		s.vol += (target - s.vol) * 0.0005
		t := float64(s.pos) / sampleRate
		f := 600.0
		if math.Mod(t, 1.3) >= 0.65 {
			f = 800
		}
		s.phase += 2 * math.Pi * f / sampleRate
		if s.phase > 2*math.Pi {
			s.phase -= 2 * math.Pi
		}
		x := math.Tanh(2.5*math.Sin(s.phase)) * 0.6
		l := int16(x * s.vol * lg * 9000)
		r := int16(x * s.vol * rg * 9000)
		b[i], b[i+1] = byte(l), byte(l>>8)
		b[i+2], b[i+3] = byte(r), byte(r>>8)
		s.pos++
	}
	return n, nil
}

type Sound struct {
	ctx    *audio.Context
	siren  *sirenStream
	player *audio.Player
}

func newSound() *Sound {
	s := &Sound{siren: &sirenStream{}}
	if os.Getenv("TRUCKHAUL_NOSOUND") != "" {
		return s
	}
	defer func() { recover() }() // no audio device: play silently
	s.ctx = audio.NewContext(sampleRate)
	p, err := s.ctx.NewPlayer(s.siren)
	if err != nil {
		return s
	}
	p.SetBufferSize(80_000_000) // 80ms
	p.Play()
	s.player = p
	return s
}

func (s *Sound) setSiren(vol, pan float64) {
	if s == nil || s.siren == nil {
		return
	}
	s.siren.target.Store(math.Float64bits(vol))
	s.siren.pan.Store(math.Float64bits(pan))
}

// updateSound points the siren at the nearest vehicle with blue lights on.
func (g *Game) updateSound() {
	if g.sound == nil {
		return
	}
	if g.state != stPlay {
		g.sound.setSiren(0, 0)
		return
	}
	best, pan := 1e9, 0.0
	for _, c := range g.cars {
		if !c.isEV() && !c.chase {
			continue
		}
		d := math.Hypot(c.x-g.truck.X, c.y-g.truck.Y)
		if d < best {
			best = d
			pan = math.Max(-1, math.Min(1, (c.x-g.truck.X)/700))
		}
	}
	vol := 0.0
	if best < 1600 {
		v := 1 - best/1600
		vol = v * v * 0.9
	}
	g.sound.setSiren(vol, pan*0.7)
}

// runWithSoundFallback restarts the game without sound if the audio device fails
// (e.g. a PC with no speakers / audio driver).
func runWithSoundFallback(err error) bool {
	if err == nil || !strings.Contains(err.Error(), "audio") || os.Getenv("TRUCKHAUL_NOSOUND") != "" {
		return false
	}
	exe, e := os.Executable()
	if e != nil {
		return false
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = append(os.Environ(), "TRUCKHAUL_NOSOUND=1")
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	return cmd.Start() == nil
}

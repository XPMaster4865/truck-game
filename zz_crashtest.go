//go:build crashtest

package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"runtime/debug"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

var shot string
var shotX, shotY float64

type ct struct {
	*Game
	n     int
	slowU int
	slowD int
	maxU  time.Duration
	maxD  time.Duration
	sumU  time.Duration
}

func (t *ct) Update() error {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("PANIC in Update:", r)
			debug.PrintStack()
			os.Exit(1)
		}
	}()
	t.n++
	if t.n == 3 {
		t0 := time.Now()
		if os.Getenv("LOADSAVE") != "" {
			fmt.Println("load ok:", t.load())
		} else {
			t.reset()
		}
		t.state = stPlay
		fmt.Printf("enter handled in %.2fs, cars=%d\n", time.Since(t0).Seconds(), len(t.cars))
	}
	if t.n > 3 && t.n%50 == 0 { // drive somewhere new so map sections get built
		t.truck.X += 1500
		if t.truck.X > worldW-2000 {
			t.truck.X = 2000
			t.truck.Y = lonOY + 1000
		}
	}
	t0 := time.Now()
	err := t.Game.Update()
	d := time.Since(t0)
	if t.n > 5 {
		t.sumU += d
		if d > t.maxU {
			t.maxU = d
		}
	}
	if t.n >= 20 && t.n < 80 && t.n%15 == 5 {
		// park the camera on a lights junction that has a minor road
		k := 0
		for id, nd := range t.nodes {
			if nd.Ctrl == ctrlLights && nd.City == 0 && len(nd.Roads) == 3 {
				if k == (t.n-20)/15 {
					shotX, shotY = nd.X-screenW/2, nd.Y-screenH/2
					t.truck.X, t.truck.Y = nd.X-900, nd.Y-900
					shot = fmt.Sprintf("/tmp/ct/junction%d.png", k)
					_ = id
					break
				}
				k++
			}
		}
	}
	if t.n == 600 {
		fmt.Printf("600 frames OK. update avg %.2fms max %.1fms | draw max %.1fms\n", float64(t.sumU.Microseconds())/595/1000, float64(t.maxU.Microseconds())/1000, float64(t.maxD.Microseconds())/1000)
		return ebiten.Termination
	}
	return err
}

func (t *ct) Draw(s *ebiten.Image) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("PANIC in Draw:", r)
			debug.PrintStack()
			os.Exit(1)
		}
	}()
	t0 := time.Now()
	if shot != "" {
		t.camX, t.camY = shotX, shotY
	}
	t.Game.Draw(s)
	if shot != "" && t.n%15 == 6 {
		b := s.Bounds()
		px := make([]byte, 4*b.Dx()*b.Dy())
		s.ReadPixels(px)
		f, _ := os.Create(shot)
		png.Encode(f, &image.RGBA{Pix: px, Stride: 4 * b.Dx(), Rect: b})
		f.Close()
		shot = ""
	}
	if d := time.Since(t0); t.n > 5 && d > t.maxD {
		t.maxD = d
	}
}

func (t *ct) Layout(w, h int) (int, int) { return screenW, screenH }

func init() {
	os.Setenv("TRUCKHAUL_NOSOUND", "1")
	t0 := time.Now()
	g := NewGame()
	fmt.Printf("NewGame %.2fs\n", time.Since(t0).Seconds())
	if err := ebiten.RunGame(&ct{Game: g}); err != nil {
		fmt.Println("err", err)
	}
	os.Exit(0)
}

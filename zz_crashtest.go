//go:build crashtest

package main

import (
	"fmt"
	"image"
	"image/png"
	"math"
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
		if srv := os.Getenv("ONLINE"); srv != "" {
			t.net = startNet(srv, os.Getenv("PNAME"), t.cabColor(t.cur), t.cur)
			t.truck.X += float64(len(os.Getenv("PNAME"))) * 40
		}
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
	if t.n == 95 || t.n == 110 {
		k := 0
		for _, p := range places {
			if p.MW > 0 {
				if (t.n == 95 && k == 0) || (t.n == 110 && k == 0) {
					sl := t.slips[k*2+(t.n-95)/15]
					x, y, _, _ := sl.at(sl.Len * 0.3)
					if (t.n-95)/15 == 1 {
						x, y, _, _ = sl.at(sl.Len * 0.7)
					}
					shotX, shotY = x-screenW/2, y-screenH/2
					shot = fmt.Sprintf("/tmp/ct/services%d_%d.png", k, t.n)
					t.truck.X, t.truck.Y = x+600, y+900
				}
				k++
			}
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
	if t.net != nil && t.n == 150 {
		t.net.sendMsg(netMsg{T: "chat", Msg: "hello from " + os.Getenv("PNAME")})
	}
	if t.net != nil && t.n == 500 {
		t.net.mu.Lock()
		fmt.Printf("[%s] status=%q online=%v others=%d\n", os.Getenv("PNAME"), t.net.status, t.net.online, len(t.net.players))
		for _, r := range t.net.players {
			fmt.Printf("[%s]   sees %s at %.0f,%.0f\n", os.Getenv("PNAME"), r.Name, r.X, r.Y)
		}
		for _, c := range t.net.chat {
			fmt.Printf("[%s]   chat %s: %s\n", os.Getenv("PNAME"), c.from, c.msg)
		}
		t.net.mu.Unlock()
		return ebiten.Termination
	}
	if t.n == 600 {
		// traffic soak: count vehicles overlapping and cars stuck for a long time
		g := t.Game
		overlaps, worstStill := 0, 0.0
		still := map[*Car]float64{}
		for i := 0; i < 6000; i++ {
			g.clock += dt
			g.updateCars()
			g.updateIncidents()
			if i%10 == 0 {
				for a := 0; a < len(g.cars); a++ {
					c := g.cars[a]
					for b := a + 1; b < len(g.cars); b++ {
						o := g.cars[b]
						if math.Abs(o.x-c.x) > 120 || math.Abs(o.y-c.y) > 120 {
							continue
						}
						along := (o.x-c.x)*c.hx + (o.y-c.y)*c.hy
						lat := (o.x-c.x)*-c.hy + (o.y-c.y)*c.hx
						if math.Abs(along) < (c.half+o.half)-6 && math.Abs(lat) < (c.width+o.width)/2-4 && c.crashed <= 0 && o.crashed <= 0 && !c.chase && !o.chase {
							overlaps++
							if overlaps <= 14 {
								fmt.Printf("  overlap: kinds %d/%d inTurn %v/%v dot=%.2f ghost %.1f/%.1f speed %.0f/%.0f roads %d/%d motor=%v pull %.1f/%.1f along=%.0f lat=%.0f\n", c.kind, o.kind, c.inTurn, o.inTurn, c.hx*o.hx+c.hy*o.hy, c.ghost, o.ghost, c.speed, o.speed, c.road, o.road, g.isMotor(c.road), c.pull, o.pull, along, lat)
							}
						}
					}
				}
			}
			for _, c := range g.cars {
				if c.speed < 1 && c.crashed <= 0 && c.broken <= 0 {
					still[c] += dt
				} else {
					still[c] = 0
				}
				worstStill = math.Max(worstStill, still[c])
			}
		}
		stuck := 0
		for c, v := range still {
			if v > 40 {
				stuck++
				n := g.nodes[c.to]
				fmt.Printf("  STUCK kind=%d inTurn=%v t=%.0f L=%.0f ctrl=%d roads@node=%d cleared=%v yielded=%v turn=%d motor=%v exitBlocked=%v nroad=%d\n", c.kind, c.inTurn, c.t, g.carL(c), n.Ctrl, len(n.Roads), c.cleared, c.yielded, c.turnSign, g.isMotor(c.road), g.exitBlocked(c), c.nroad)
			}
		}
		fmt.Printf("soak 100s: overlap-samples=%d  stuck>40s=%d  worst wait=%.0fs\n", overlaps, stuck, worstStill)
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
	if shot != "" {
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

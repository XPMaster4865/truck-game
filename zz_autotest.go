//go:build autotest

package main

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"

	"github.com/hajimehoshi/ebiten/v2"
)

var frame int
var shotName string

type testGame struct{ *Game }

func (t *testGame) Update() error {
	frame++
	g := t.Game
	switch frame {
	case 40:
		shotName = "title"
	case 50:
		g.reset()
		g.state = stPlay
		// traffic soak test: 150 seconds
		still := map[*Car]float64{}
		worst := 0.0
		for i := 0; i < 9000; i++ {
			g.clock += dt
			g.updateCars()
			g.updateIncidents()
			for _, c := range g.cars {
				if c.speed < 1 && c.crashed <= 0 {
					still[c] += dt
				} else {
					still[c] = 0
				}
				worst = math.Max(worst, still[c])
			}
		}
		n := 0
		for c, v := range still {
			if v > 30 {
				n++
				fmt.Printf("  stuck car kind=%d inTurn=%v edge=(%d,%d)->(%d,%d) next=(%d,%d) t=%.0f pull=%.2f ghost=%.1f wait=%.1f x=%.0f y=%.0f\n", c.kind, c.inTurn, c.ai, c.aj, c.bi, c.bj, c.ni, c.nj, c.t, c.pull, c.ghost, c.wait, c.x, c.y)
			}
		}
		fmt.Printf("soak: cars=%d incidents=%d stuck>30s now=%d worst=%.1fs\n", len(g.cars), len(g.incidents), n, worst)
		// put truck at a junction approach
		x, y := nodePos(4, 3)
		g.truck.X, g.truck.Y, g.truck.Angle = x-250, y-30, 0
		g.camX, g.camY = g.truck.X-screenW/2+150, g.truck.Y-screenH/2
	case 55:
		shotName = "junction"
	case 60:
		// incidents + an EV right behind the truck
		x, y := nodePos(4, 3)
		ax, _ := nodePos(5, 3)
		g.incidents = append(g.incidents, &Incident{i: 4, j: 3, ni: 5, nj: 3, kind: incRTC, timer: 99, mx: (x + ax) / 2, my: y, horiz: true, seed: 3})
		_, by := nodePos(4, 4)
		g.incidents = append(g.incidents, &Incident{i: 4, j: 3, ni: 4, nj: 4, kind: incWorks, timer: 99, mx: x, my: (y + by) / 2, horiz: false, seed: 4})
		g.truck.X, g.truck.Y = x-200, y-30
		g.camX, g.camY = x-screenW/2, y-screenH/2+150
		g.newsFlash("Road traffic collision on High Street.\nRoad closed - emergency services on scene.")
		g.fuel[0] = 20
	case 64:
		shotName = "incidents"
	case 70:
		g.state = stShop
		g.money = 20000
		g.shopSel = 2
		g.shopPaint = 4
	case 74:
		shotName = "shop"
	case 80:
		g.state = stMap
	case 84:
		shotName = "map"
	case 90:
		g.state = stPlay
		// police chase test: drive forward with police behind, speeding
		x, y := nodePos(2, 2)
		g.truck.X, g.truck.Y, g.truck.Angle, g.truck.Speed = x+100, y-30, 0, 0
		g.trail = nil
		pol := g.makeCar(kPolice)
		pol.ai, pol.aj, pol.bi, pol.bj = 2, 2, 3, 2
		pol.t = 0
		g.carPose(pol)
		g.setNext(pol)
		g.cars = append(g.cars, pol)
		g.cool = map[string]float64{}
		for i := 0; i < 40; i++ {
			g.truck.Speed = 380
			g.updateTruck()
			g.updateCars()
			g.updateLaw()
		}
		fmt.Println("chase started:", g.chaser != nil, g.chaseReason, "trail", len(g.trail), "policeNear", g.policeNear(380), "road", g.roadName, g.limit, "cool", g.cool, "pol", pol.x-g.truck.X, pol.y-g.truck.Y, pol.chase)
		for i := 0; i < 400 && g.chaser != nil; i++ {
			g.truck.Speed = 0
			g.updateTruck()
			g.updateCars()
			g.updateLaw()
			if i == 30 {
				g.camX, g.camY = g.truck.X-screenW/2, g.truck.Y-screenH/2
			}
		}
		fmt.Println("after stopping: chaser=", g.chaser != nil, "money", g.money, "points", g.points(), g.fineTitle)
		// EV obey test: spawn an ambulance on a road and count cars pulled over
		ev := g.makeCar(kAmbulance)
		ev.life = 99
		ev.ai, ev.aj, ev.bi, ev.bj = 6, 5, 7, 5
		ev.t = 0
		g.setNext(ev)
		g.carPose(ev)
		g.cars = append(g.cars, ev)
		pulled := 0
		for i := 0; i < 600; i++ {
			g.clock += dt
			g.updateCars()
			for _, c := range g.cars {
				if c.pull > 0.8 {
					pulled++
				}
			}
		}
		fmt.Println("car-frames pulled over for EV:", pulled)
		g.camX, g.camY = ev.x-screenW/2, ev.y-screenH/2
		g.truck.X, g.truck.Y = ev.x-300, ev.y+400
	case 94:
		shotName = "ev"
	case 100:
		// fuel station + dealer behaviour
		for _, p := range places {
			if p.Fuel > 0 {
				x, y := p.bayCenter()
				g.truck.X, g.truck.Y, g.truck.Speed = x, y, 0
				g.camX, g.camY = x-screenW/2, y-screenH/2
				break
			}
		}
		g.fuel[g.cur] = 50
		m0 := g.money
		for i := 0; i < 120; i++ {
			g.updateJobs()
		}
		fmt.Printf("fuel after 2s at pump: %.0f L, spent £%d\n", g.fuel[g.cur], m0-g.money)
	case 104:
		shotName = "fuel"
	case 110:
		g.offences = []Offence{{g.clock, 12}}
		g.state = stGameOver
	case 114:
		shotName = "gameover"
	case 120:
		return ebiten.Termination
	}
	if frame > 50 && g.state == stPlay {
		g.clock += dt
	}
	return nil
}

func (t *testGame) Draw(screen *ebiten.Image) {
	t.Game.Draw(screen)
	if shotName != "" {
		b := screen.Bounds()
		px := make([]byte, 4*b.Dx()*b.Dy())
		screen.ReadPixels(px)
		img := &image.RGBA{Pix: px, Stride: 4 * b.Dx(), Rect: b}
		fp, _ := os.Create("/tmp/shot_" + shotName + ".png")
		png.Encode(fp, img)
		fp.Close()
		shotName = ""
	}
}

func (t *testGame) Layout(w, h int) (int, int) { return screenW, screenH }

func init() {
	ebiten.SetWindowSize(screenW, screenH)
	ebiten.SetTPS(10)
	g := NewGame()
	g.Update()
	if err := ebiten.RunGame(&testGame{g}); err != nil {
		panic(err)
	}
	os.Exit(0)
}

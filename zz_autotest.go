//go:build autotest

package main

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

var frame int
var shotName string

type testGame struct{ *Game }

func (t *testGame) look(x, y float64) { t.camX, t.camY = x-screenW/2, y-screenH/2 }

func (t *testGame) Update() error {
	frame++
	g := t.Game
	if frame%5 == 0 {
		fmt.Println("frame", frame, time.Now().Format("15:04:05"))
	}
	switch frame {
	case 40:
		shotName = "title"
	case 50:
		g.reset()
		g.state = stPlay
		still := map[*Car]float64{}
		worst := 0.0
		t0 := time.Now()
		for i := 0; i < 9000; i++ {
			g.clock += dt
			g.updateCars()
			g.updateIncidents()
			for _, c := range g.cars {
				if c.speed < 1 && c.crashed <= 0 && c.broken <= 0 {
					still[c] += dt
				} else {
					still[c] = 0
				}
				worst = math.Max(worst, still[c])
			}
		}
		n, nm := 0, 0
		for c, v := range still {
			if v > 40 {
				n++
				fmt.Printf("  stuck kind=%d inTurn=%v road=%d from=%d to=%d t=%.0f L=%.0f x=%.0f y=%.0f ctrl=%d cleared=%v\n", c.kind, c.inTurn, c.road, c.from, c.to, c.t, g.carL(c), c.x, c.y, g.nodes[c.to].Ctrl, c.cleared)
			}
		}
		for _, c := range g.cars {
			if g.isMotor(c.road) {
				nm++
			}
		}
		fmt.Printf("soak: cars=%d onMotorway=%d incidents=%d stuck>40s=%d worst=%.1fs  (%.1fs real for 150s sim)\n", len(g.cars), nm, len(g.incidents), n, worst, time.Since(t0).Seconds())
		// find a lights junction and a priority junction to photograph
		for id, nd := range g.nodes {
			if nd.Ctrl == ctrlLights && nd.City == 0 {
				g.truck.X, g.truck.Y = nd.X-200, nd.Y+300
				t.look(nd.X, nd.Y)
				_ = id
				break
			}
		}
	case 55:
		shotName = "lights"
	case 60:
		for _, nd := range g.nodes {
			stop := false
			for _, a := range nd.Appr {
				if a.Stop {
					stop = true
				}
			}
			if nd.Ctrl == ctrlPriority && nd.City == 0 && stop {
				t.look(nd.X, nd.Y)
				g.truck.X, g.truck.Y = nd.X+400, nd.Y+400
				break
			}
		}
	case 64:
		shotName = "giveway"
	case 70:
		mr := g.roads[g.motorways[0]]
		x, y, tx, ty := mr.at(4000)
		g.truck.X, g.truck.Y, g.truck.Angle = x+ty*mwLanes[1], y-tx*mwLanes[1], math.Atan2(ty, tx)
		t.look(x+300, y)
		// drive along the motorway on autopilot for 40s
		crashes := 0
		g.fuel[g.cur] = 1000
		for i := 0; i < 60*30; i++ {
			h := g.roadQuery(g.truck.X, g.truck.Y, 400)
			_, _, rtx, rty := mr.at(h.S + 200)
			want := math.Atan2(rty, rtx)
			// steer towards lane centre
			err := mwLanes[1] - h.Lat
			want -= err * 0.004
			g.truck.Angle += math.Remainder(want-g.truck.Angle, 2*math.Pi) * 0.2
			g.truck.Speed = 380
			before := g.truck.Speed
			g.updateTruck()
			if g.truck.Speed < 0 && before > 0 {
				crashes++
			}
			g.clock += dt
			g.updateCars()
			g.updateLaw()
		}
		fmt.Printf("motorway autopilot: x=%.0f crashes=%d road=%q limit=%.0f\n", g.truck.X, crashes, g.roadName, g.limit)
		x, y, tx, ty = mr.at(mr.Len * 0.4)
		g.truck.X, g.truck.Y, g.truck.Angle, g.truck.Speed = x+ty*mwLanes[0], y-tx*mwLanes[0], math.Atan2(ty, tx), 0
		t.look(x, y-150)
	case 74:
		shotName = "motorway"
	case 80:
		// GPS route to Cambridge
		for _, p := range places {
			if p.Name == "King's Collage" {
				x, y := p.bayCenter()
				g.gpsOn, g.gpsX, g.gpsY = true, x, y
			}
		}
		for _, p := range places {
			if p.Name == "Amazin' Warehouse" {
				x, y := p.bayCenter()
				g.truck.X, g.truck.Y, g.truck.Angle = x, y, 0
			}
		}
		g.routeDirty = true
		g.updateGPS()
		fmt.Printf("gps: route pts=%d len=%s steps=%d text=%q\n", len(g.route), fmtDist(g.routeLen), len(g.routeSteps), g.gpsText)
		t.look(g.truck.X, g.truck.Y)
	case 84:
		shotName = "gps"
	case 90:
		g.state = stMap
	case 94:
		shotName = "map"
	case 100:
		g.state = stPlay
		for _, nd := range g.nodes {
			if nd.City == 1 && len(nd.Roads) >= 3 {
				t.look(nd.X, nd.Y)
				break
			}
		}
	case 104:
		shotName = "cambridge"
	case 110:
		// roadworks with temporary lights + RTC
		for ri, rd := range g.roads {
			if rd.City == 0 && rd.Len > 800 && g.visible(0, 0, 0) == false {
				x, y, tx, ty := rd.at(rd.Len / 2)
				g.incidents = []*Incident{{road: ri, s: rd.Len / 2, closedDir: 1, kind: incWorksLight, timer: 999, mx: x, my: y, tx: tx, ty: ty, seed: 5}}
				t.look(x, y)
				break
			}
		}
		for i := 0; i < 60*20; i++ {
			g.clock += dt
			g.updateCars()
		}
	case 114:
		shotName = "roadworks"
	case 120:
		// save/load
		g.money = 12345
		g.save()
		g.money = 0
		ok := g.load()
		fmt.Println("save/load:", ok, g.money)
	case 125:
		g.state = stJobMaker
		g.jmFrom, g.jmTo, g.jmCol = 2, 30, 1
	case 128:
		shotName = "jobmaker"
	case 131:
		g.state = stPlay
		for _, nd := range g.nodes {
			if nd.City == 2 && len(nd.Roads) >= 3 {
				t.look(nd.X, nd.Y)
				break
			}
		}
	case 133:
		shotName = "manchester"
	case 140:
		return ebiten.Termination
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
	os.Setenv("XDG_CONFIG_HOME", "/tmp/thcfg")
	os.Setenv("TRUCKHAUL_NOSOUND", "1")
	ebiten.SetWindowSize(screenW, screenH)
	ebiten.SetTPS(10)
	t0 := time.Now()
	g := NewGame()
	fmt.Printf("generate: %.1fs  roads=%d nodes=%d buildings=%d\n", time.Since(t0).Seconds(), len(g.roads), len(g.nodes), len(g.buildings))
	t0 = time.Now()
	g.Update()
	fmt.Printf("first update (world build): %.1fs\n", time.Since(t0).Seconds())
	if err := ebiten.RunGame(&testGame{g}); err != nil {
		panic(err)
	}
	os.Exit(0)
}

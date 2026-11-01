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

func (t *testGame) look(x, y float64) {
	t.camX, t.camY = x-screenW/2, y-screenH/2
}

func (t *testGame) Update() error {
	frame++
	g := t.Game
	switch frame {
	case 40:
		shotName = "title"
	case 50:
		g.reset()
		g.state = stPlay
		still := map[*Car]float64{}
		worst := 0.0
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
				for _, o := range g.cars {
					if o != c && math.Hypot(o.x-c.x, o.y-c.y) < 250 {
						fmt.Printf("     near: kind=%d spd=%.0f dist=%.0f along=%.0f lat=%.0f inTurn=%v pull=%.2f crashed=%.0f a=%d b=%d chase=%v\n", o.kind, o.speed, math.Hypot(o.x-c.x, o.y-c.y), (o.x-c.x)*c.hx+(o.y-c.y)*c.hy, (o.x-c.x)*-c.hy+(o.y-c.y)*c.hx, o.inTurn, o.pull, o.crashed, o.a, o.b, o.chase)
					}
				}
				fmt.Printf("     pulling=%v pull=%.2f ghost=%.1f wait=%.1f truckdist=%.0f\n", c.pulling, c.pull, c.ghost, c.wait, math.Hypot(g.truck.X-c.x, g.truck.Y-c.y))
				fmt.Printf("  stuck kind=%d inTurn=%v a=%d b=%d n=%d t=%.0f off=%.0f x=%.0f y=%.0f motor=%v\n", c.kind, c.inTurn, c.a, c.b, c.n, c.t, c.off, c.x, c.y, g.isMotor(c.a, c.b))
			}
		}
		for _, c := range g.cars {
			if g.isMotor(c.a, c.b) {
				nm++
			}
		}
		fmt.Printf("soak: cars=%d onMotorway=%d incidents=%d stuck>40s=%d worst=%.1fs\n", len(g.cars), nm, len(g.incidents), n, worst)
		x, y := g.nodes[cities[0].nodeID(4, 3)].X, g.nodes[cities[0].nodeID(4, 3)].Y
		g.truck.X, g.truck.Y, g.truck.Angle = x-250, y-30, 0
		g.ind = -1
		g.clock = 0.1
		t.look(g.truck.X+150, g.truck.Y)
	case 55:
		shotName = "junction"
	case 60:
		g.ind = 0
		g.truck.X, g.truck.Y, g.truck.Angle, g.truck.Speed = float64(lonW*tile)+40, barrierY-32, 0, 0
		g.fuel[g.cur] = 1000
		x0 := g.truck.X
		crashes := 0
		for i := 0; i < 60*45; i++ {
			g.truck.Speed = math.Max(g.truck.Speed, 300)
			before := g.truck.Speed
			g.updateTruck()
			if g.truck.Speed < 0 && before > 0 {
				crashes++
				g.truck.Speed = 300
				g.truck.Y = barrierY - 32
				g.truck.Angle = 0
			}
			g.clock += dt
			g.updateCars()
			g.updateLaw()
		}
		fmt.Printf("motorway drive: moved %.0f px (%.1f miles) crashes=%d now at x=%.0f town=%q road=%q limit=%.0f\n", g.truck.X-x0, (g.truck.X-x0)/pxPerMile, crashes, g.truck.X, g.town, g.roadName, g.limit)
		g.truck.X = float64((lonW+60)*tile) + 20
		g.truck.Y = barrierY - 94
		t.look(g.truck.X+200, g.truck.Y+40)
	case 64:
		shotName = "motorway"
	case 70:
		p := places[len(places)-2]
		x, y := p.bayCenter()
		g.truck.X, g.truck.Y, g.truck.Speed = x, y+200, 0
		t.look(x, y+150)
	case 74:
		shotName = "services"
	case 80:
		c := cities[1]
		x, y := g.nodes[c.nodeID(3, 2)].X, g.nodes[c.nodeID(3, 2)].Y
		g.truck.X, g.truck.Y = x+150, y-30
		t.look(x, y)
	case 84:
		shotName = "cambridge"
	case 90:
		c := cities[0]
		a, b := c.nodeID(6, 2), c.nodeID(7, 2)
		na, nb := g.nodes[a], g.nodes[b]
		g.incidents = []*Incident{{a: a, b: b, kind: incWorksLight, timer: 999, mx: (na.X + nb.X) / 2, my: na.Y, horiz: true, seed: 5}}
		a2, b2 := c.nodeID(6, 2), c.nodeID(6, 3)
		n2 := g.nodes[b2]
		g.incidents = append(g.incidents, &Incident{a: a2, b: b2, kind: incRTC, timer: 999, mx: na.X, my: (na.Y + n2.Y) / 2, seed: 9})
		g.truck.X, g.truck.Y = na.X-300, na.Y-400
		for i := 0; i < 60*40; i++ {
			g.clock += dt
			g.updateCars()
		}
		on := 0
		for _, car := range g.cars {
			if sameEdge(car.a, car.b, a, b) {
				on++
			}
		}
		fmt.Println("cars on roadworks edge:", on)
		t.look((na.X+nb.X)/2, na.Y+150)
	case 94:
		shotName = "roadworks"
	case 100:
		g.state = stShop
		g.money = 40000
		g.shopSel = 6
		g.shopPaint = 0
	case 104:
		shotName = "shop"
	case 110:
		g.state = stMap
	case 114:
		shotName = "map"
	case 120:
		g.state = stPlay
		g.incidents = nil
		x, y := g.nodes[cities[0].nodeID(2, 2)].X, g.nodes[cities[0].nodeID(2, 2)].Y
		g.truck.X, g.truck.Y, g.truck.Angle, g.truck.Speed = x+100, y-30, 0, 0
		g.trail = nil
		pol := g.makeCar(kPolice)
		pol.a, pol.b = cities[0].nodeID(2, 2), cities[0].nodeID(3, 2)
		pol.t = 0
		pol.off = pol.lane
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
		fmt.Println("chase started:", g.chaser != nil, g.chaseReason)
		for i := 0; i < 400 && g.chaser != nil; i++ {
			g.truck.Speed = 0
			g.updateTruck()
			g.updateCars()
			g.updateLaw()
		}
		fmt.Println("after stopping: chaser=", g.chaser != nil, "points", g.points(), g.fineTitle)
		g.owned[5] = true
		g.cur = 5
		g.fuel[5] = 0
		for _, p := range places {
			if p.Name == "GridSwerve Charging Hub" {
				x, y := p.bayCenter()
				g.truck.X, g.truck.Y, g.truck.Speed = x, y, 0
				t.look(x, y)
			}
		}
		secs := 0.0
		for g.fuel[5] < models[5].Tank-0.01 && secs < 300 {
			g.updateJobs()
			secs += dt
		}
		fmt.Printf("electric charge 0-100%%: %.1f s\n", secs)
		g.fuel[5] = models[5].Tank * 0.4
		for i := 0; i < 10; i++ {
			g.updateJobs()
		}
	case 124:
		shotName = "charging"
	case 130:
		g.money = 12345
		g.deliveries = 7
		g.paint[5] = 3
		g.save()
		g.money = 0
		ok := g.load()
		fmt.Println("save/load:", ok, "money", g.money, "deliveries", g.deliveries, "cur", g.cur, "paint", g.paint[5], "path", savePath())
	case 140:
		g.offences = []Offence{{g.clock, 12}}
		g.state = stGameOver
	case 144:
		shotName = "gameover"
	case 150:
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
	g := NewGame()
	g.Update()
	if err := ebiten.RunGame(&testGame{g}); err != nil {
		panic(err)
	}
	os.Exit(0)
}

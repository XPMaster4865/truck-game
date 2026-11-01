package main

import (
	"fmt"
	"image/color"
	"math"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2"
)

// ---------- traffic lights ----------

// light returns 0 green, 1 amber, 2 red, 3 red+amber. axis 0 = east/west traffic, 1 = north/south.
func (g *Game) light(i, j, axis int) int {
	t := math.Mod(g.clock+g.lightOff[i][j], 22)
	if axis == 1 {
		t = math.Mod(t+11, 22)
	}
	switch {
	case t < 8:
		return 0
	case t < 10:
		return 1
	case t < 21:
		return 2
	}
	return 3
}

// ---------- incidents ----------

const (
	incRTC = iota
	incWorks
)

type Incident struct {
	i, j, ni, nj int
	kind         int
	timer        float64
	mx, my       float64
	horiz        bool
	seed         int64
}

func (in *Incident) rect() (x0, y0, x1, y1 float64) {
	if in.horiz {
		return in.mx - 70, in.my - 64, in.mx + 70, in.my + 64
	}
	return in.mx - 64, in.my - 70, in.mx + 64, in.my + 70
}

func sameEdge(a, b, c, d, e, f, h, k int) bool {
	return (a == e && b == f && c == h && d == k) || (a == h && b == k && c == e && d == f)
}

func (g *Game) edgeBlocked(i, j, ni, nj int) bool {
	for _, in := range g.incidents {
		if sameEdge(i, j, ni, nj, in.i, in.j, in.ni, in.nj) {
			return true
		}
	}
	return false
}

func (g *Game) visible(x, y, margin float64) bool {
	return x > g.camX-margin && x < g.camX+screenW+margin && y > g.camY-margin && y < g.camY+screenH+margin
}

func (g *Game) spawnIncident() {
	for try := 0; try < 60; try++ {
		i, j := g.rng.Intn(nodesX), g.rng.Intn(nodesY)
		ni, nj := i+1, j
		if g.rng.Intn(2) == 0 {
			ni, nj = i, j+1
		}
		if ni >= nodesX || nj >= nodesY || g.edgeBlocked(i, j, ni, nj) {
			continue
		}
		cam := false
		for _, c := range g.cameras {
			if sameEdge(i, j, ni, nj, c.i, c.j, c.ni, c.nj) {
				cam = true
			}
		}
		if cam {
			continue
		}
		ax, ay := nodePos(i, j)
		bx, by := nodePos(ni, nj)
		mx, my := (ax+bx)/2, (ay+by)/2
		if g.visible(mx, my, 300) {
			continue
		}
		in := &Incident{i: i, j: j, ni: ni, nj: nj, mx: mx, my: my, horiz: j == nj, seed: g.rng.Int63()}
		name := edgeName(i, j, ni, nj)
		if g.rng.Float64() < 0.55 {
			in.kind = incRTC
			in.timer = 60 + g.rng.Float64()*50
			g.newsFlash(fmt.Sprintf("Road traffic collision on %s.\nRoad closed - emergency services on scene.", name))
		} else {
			in.kind = incWorks
			in.timer = 120 + g.rng.Float64()*90
			g.newsFlash(fmt.Sprintf("Emergency roadworks on %s.\nRoad closed - find another route.", name))
		}
		g.incidents = append(g.incidents, in)
		for _, c := range g.cars {
			if c.chase {
				continue
			}
			if sameEdge(c.ai, c.aj, c.bi, c.bj, i, j, ni, nj) || (c.inTurn && sameEdge(c.bi, c.bj, c.ni, c.nj, i, j, ni, nj)) {
				g.respawnCar(c)
			}
		}
		if in.kind == incRTC {
			g.dispatchEV(evAmbulance)
		}
		return
	}
}

func (g *Game) updateIncidents() {
	g.nextIncident -= dt
	if g.nextIncident <= 0 {
		g.nextIncident = 40 + g.rng.Float64()*35
		if len(g.incidents) < 4 {
			g.spawnIncident()
		}
	}
	keep := g.incidents[:0]
	for _, in := range g.incidents {
		in.timer -= dt
		if in.timer > 0 {
			keep = append(keep, in)
		} else {
			g.newsFlash(edgeName(in.i, in.j, in.ni, in.nj) + " has now reopened.")
		}
	}
	g.incidents = keep
}

// ---------- vehicles ----------

const (
	kCar = iota
	kVan
	kBus
	kPolice
	kAmbulance
	kFire
	kPoliceEV
)

const (
	evAmbulance = kAmbulance
	evFire      = kFire
	evPolice    = kPoliceEV
)

type Car struct {
	ai, aj, bi, bj, ni, nj int
	inTurn                 bool
	t                      float64
	p0x, p0y, p1x, p1y     float64
	p2x, p2y, turnLen      float64
	speed, factor          float64
	kind                   int
	half, width            float64
	img                    *ebiten.Image
	x, y, hx, hy           float64
	wait, ghost, crashed   float64
	braking                bool
	turnSign               int
	lane, pull             float64
	pulling                bool
	life                   float64
	// police chase
	chase    bool
	trailIdx int
}

func (c *Car) isEV() bool { return c.kind == kAmbulance || c.kind == kFire || c.kind == kPoliceEV }

func (c *Car) angle() float64 { return math.Atan2(c.hy, c.hx) }

func (c *Car) circles() [][2]float64 {
	var out [][2]float64
	n := 2
	if c.half > 30 {
		n = 3
	}
	for k := 0; k < n; k++ {
		off := -(c.half - 11) + float64(k)*2*(c.half-11)/float64(n-1)
		out = append(out, [2]float64{c.x + c.hx*off, c.y + c.hy*off})
	}
	return out
}

var carColors = []color.RGBA{rgb(50, 110, 200), rgb(230, 230, 230), rgb(40, 40, 45), rgb(220, 190, 40),
	rgb(60, 160, 80), rgb(180, 60, 160), rgb(240, 140, 40), rgb(120, 200, 220), rgb(150, 30, 30), rgb(160, 160, 165)}

func (g *Game) makeCar(kind int) *Car {
	c := &Car{kind: kind, lane: laneOff}
	switch kind {
	case kCar:
		c.half, c.width = 22, 22
		c.img = makeCarImg(44, 22, carColors[g.rng.Intn(len(carColors))], 0)
		c.factor = 0.85 + g.rng.Float64()*0.15
	case kVan:
		c.half, c.width = 27, 24
		c.img = makeCarImg(54, 24, rgb(235, 235, 235), 1)
		c.factor = 0.85 + g.rng.Float64()*0.1
	case kBus:
		c.half, c.width = 43, 28
		c.img = makeCarImg(86, 28, rgb(200, 30, 30), 2)
		c.factor = 0.8
	case kPolice:
		c.half, c.width = 23, 22
		c.img = makeCarImg(46, 22, rgb(245, 245, 245), 3)
		c.factor = 0.9
	case kAmbulance:
		c.half, c.width = 28, 26
		c.img = makeCarImg(56, 26, rgb(250, 220, 0), 4)
		c.factor = 1.6
		c.lane = 6
	case kFire:
		c.half, c.width = 36, 28
		c.img = makeCarImg(72, 28, rgb(210, 20, 20), 5)
		c.factor = 1.45
		c.lane = 6
	case kPoliceEV:
		c.half, c.width = 23, 22
		c.img = makeCarImg(46, 22, rgb(245, 245, 245), 3)
		c.factor = 1.7
		c.lane = 6
	}
	return c
}

func (g *Game) newCar(kind int) *Car {
	c := g.makeCar(kind)
	g.respawnCar(c)
	return c
}

func (g *Game) respawnCar(c *Car) {
	c.chase = false
	c.crashed, c.ghost, c.wait, c.pull = 0, 0, 0, 0
	for try := 0; try < 80; try++ {
		i, j := g.rng.Intn(nodesX), g.rng.Intn(nodesY)
		ni, nj := g.pickNext(i, j, -1, -1)
		if g.edgeBlocked(i, j, ni, nj) {
			continue
		}
		c.ai, c.aj, c.bi, c.bj = i, j, ni, nj
		c.inTurn = false
		c.t = 20 + g.rng.Float64()*(edgeLen-140)
		g.carPose(c)
		if try < 60 && (math.Abs(c.x-g.truck.X) < 900 && math.Abs(c.y-g.truck.Y) < 600) {
			continue
		}
		ok := true
		for _, o := range g.cars {
			if o != c && math.Hypot(o.x-c.x, o.y-c.y) < 90 {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		break
	}
	g.setNext(c)
	c.speed = g.carLimit(c) * 0.7
	g.carPose(c)
}

func (g *Game) pickNext(i, j, pi, pj int) (int, int) {
	var opts [][2]int
	back := [2]int{-1, -1}
	for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		ni, nj := i+d[0], j+d[1]
		if ni < 0 || nj < 0 || ni >= nodesX || nj >= nodesY {
			continue
		}
		if ni == pi && nj == pj {
			back = [2]int{ni, nj}
			continue
		}
		if g.edgeBlocked(i, j, ni, nj) {
			continue
		}
		opts = append(opts, [2]int{ni, nj})
	}
	if len(opts) == 0 {
		if back[0] >= 0 {
			return back[0], back[1]
		}
		return pi, pj
	}
	o := opts[g.rng.Intn(len(opts))]
	return o[0], o[1]
}

func (g *Game) setNext(c *Car) {
	c.ni, c.nj = g.pickNext(c.bi, c.bj, c.ai, c.aj)
	dx, dy := float64(c.bi-c.ai), float64(c.bj-c.aj)
	ex, ey := float64(c.ni-c.bi), float64(c.nj-c.bj)
	cr := dx*ey - dy*ex
	switch {
	case cr > 0:
		c.turnSign = 1
	case cr < 0:
		c.turnSign = -1
	case dx == -ex && dy == -ey:
		c.turnSign = 1
	default:
		c.turnSign = 0
	}
}

func (g *Game) carLimit(c *Car) float64 {
	return edgeLimitMph(c.ai, c.aj, c.bi, c.bj) / mphK * c.factor
}

func (g *Game) startTurn(c *Car) {
	bx, by := nodePos(c.bi, c.bj)
	dx, dy := float64(c.bi-c.ai), float64(c.bj-c.aj)
	ex, ey := float64(c.ni-c.bi), float64(c.nj-c.bj)
	lx, ly := dy, -dx
	mx, my := ey, -ex
	off := c.lane
	c.p0x, c.p0y = bx-dx*64+lx*off, by-dy*64+ly*off
	c.p2x, c.p2y = bx+ex*64+mx*off, by+ey*64+my*off
	if dx == ex && dy == ey {
		c.p1x, c.p1y = (c.p0x+c.p2x)/2, (c.p0y+c.p2y)/2
	} else if dx == -ex && dy == -ey {
		c.p1x, c.p1y = bx+dx*30, by+dy*30
	} else {
		c.p1x, c.p1y = bx+lx*off+mx*off, by+ly*off+my*off
	}
	l := 0.0
	px, py := c.p0x, c.p0y
	for k := 1; k <= 10; k++ {
		x, y := bez(c, float64(k)/10)
		l += math.Hypot(x-px, y-py)
		px, py = x, y
	}
	c.turnLen = math.Max(l, 1)
	c.inTurn = true
}

func bez(c *Car, u float64) (float64, float64) {
	a, b, d := (1-u)*(1-u), 2*(1-u)*u, u*u
	return a*c.p0x + b*c.p1x + d*c.p2x, a*c.p0y + b*c.p1y + d*c.p2y
}

func (g *Game) carPose(c *Car) {
	if c.chase {
		return
	}
	if !c.inTurn {
		ax, ay := nodePos(c.ai, c.aj)
		dx, dy := float64(c.bi-c.ai), float64(c.bj-c.aj)
		lx, ly := dy, -dx
		off := c.lane + 24*c.pull
		c.x = ax + dx*(64+c.t) + lx*off
		c.y = ay + dy*(64+c.t) + ly*off
		c.hx, c.hy = dx, dy
		return
	}
	u := math.Min(1, c.t/c.turnLen)
	c.x, c.y = bez(c, u)
	tx := 2*(1-u)*(c.p1x-c.p0x) + 2*u*(c.p2x-c.p1x)
	ty := 2*(1-u)*(c.p1y-c.p0y) + 2*u*(c.p2y-c.p1y)
	l := math.Hypot(tx, ty)
	if l > 0.001 {
		c.hx, c.hy = tx/l, ty/l
	}
}

func follow(gap float64) float64 {
	if gap <= 0 {
		return 0
	}
	return math.Sqrt(2 * 500 * gap)
}

func (g *Game) updateCars() {
	tc := g.truckCircles()
	var evs []*Car
	for _, c := range g.cars {
		if c.isEV() {
			evs = append(evs, c)
		}
	}
	keep := g.cars[:0]
	for _, c := range g.cars {
		if c.isEV() {
			c.life -= dt
			if c.life < 0 && !g.visible(c.x, c.y, 400) {
				continue // despawn
			}
		}
		keep = append(keep, c)
		if c.chase {
			g.updateChaser(c)
			continue
		}
		if c.crashed > 0 {
			c.crashed -= dt
			c.speed = 0
			c.braking = true
			continue
		}
		if !c.inTurn && g.edgeBlocked(c.bi, c.bj, c.ni, c.nj) {
			g.setNext(c)
		}
		limit := g.carLimit(c)
		desired := limit
		if c.inTurn {
			ts := 105.0
			if c.turnSign < 0 {
				ts = 85
			}
			if c.isEV() {
				ts = 150
			}
			desired = math.Min(desired, ts)
		}
		rx, ry := -c.hy, c.hx
		var blocker *Car

		// pull over for emergency vehicles
		c.pulling = false
		if !c.isEV() && !c.inTurn {
			for _, e := range evs {
				ox, oy := c.x-e.x, c.y-e.y
				if math.Abs(ox) > 320 || math.Abs(oy) > 320 {
					continue
				}
				along := ox*e.hx + oy*e.hy
				lat := ox*-e.hy + oy*e.hx
				if along > -10 && along < 300 && math.Abs(lat) < 80 {
					c.pulling = true
				}
			}
		}
		if c.pulling {
			c.pull = math.Min(1, c.pull+dt*1.2)
			if c.pull > 0.8 {
				desired = 0
			} else {
				desired = math.Min(desired, 70)
			}
		} else if c.pull > 0 {
			c.pull = math.Max(0, c.pull-dt*0.8)
			desired = math.Min(desired, 60)
		}

		if c.ghost > 0 {
			c.ghost -= dt
		}
		if !c.isEV() && c.ghost <= 0 && !c.pulling {
			for _, o := range g.cars {
				if o == c {
					continue
				}
				ox, oy := o.x-c.x, o.y-c.y
				if math.Abs(ox) > 200 || math.Abs(oy) > 200 {
					continue
				}
				along := ox*c.hx + oy*c.hy
				lat := ox*rx + oy*ry
				latLim := 16.0
				if o.pull > 0.5 && !c.inTurn {
					latLim = 6
				}
				if along > 0 && along < 170 && math.Abs(lat) < latLim {
					s := follow(along - c.half - o.half - 10)
					if s < desired {
						desired = s
						blocker = o
					}
				}
			}
		}
		if c.isEV() {
			for _, o := range evs {
				if o == c || o.hx*c.hx+o.hy*c.hy < 0.3 {
					continue
				}
				ox, oy := o.x-c.x, o.y-c.y
				along := ox*c.hx + oy*c.hy
				lat := ox*rx + oy*ry
				if along > 0 && along < 170 && math.Abs(lat) < 16 {
					desired = math.Min(desired, follow(along-c.half-o.half-10))
				}
			}
		}
		truckBlock := false
		for _, p := range tc {
			ox, oy := p[0]-c.x, p[1]-c.y
			along := ox*c.hx + oy*c.hy
			lat := ox*rx + oy*ry
			if along > 0 && along < 170 && math.Abs(lat) < 30 {
				s := follow(along - c.half - 17 - 10)
				if s < desired {
					desired = s
					truckBlock = true
					blocker = nil
				}
			}
		}
		if c.isEV() && truckBlock && c.speed < 30 {
			g.evBlockedTime += dt
		}
		if !c.inTurn {
			stopT := edgeLen - 28 - c.half
			dist := stopT - c.t
			axis := 0
			if c.bi == c.ai {
				axis = 1
			}
			st := g.light(c.bi, c.bj, axis)
			if !c.isEV() && st == 0 {
				// hold back at the stop line if an emergency vehicle is about to cross the junction
				bx, by := nodePos(c.bi, c.bj)
				for _, e := range evs {
					if math.Hypot(e.x-bx, e.y-by) < 230 && dist > -2 {
						st = 2
					}
				}
			}
			if c.isEV() {
				if st != 0 {
					desired = math.Min(desired, math.Sqrt(140*140+2*400*math.Max(0, dist)))
				}
			} else if st != 0 && dist > -2 {
				bd := c.speed * c.speed / (2 * 600)
				if !(st == 1 && dist < bd*0.7) {
					desired = math.Min(desired, follow(dist))
				}
			}
			if c.turnSign != 0 {
				ts := 105.0
				if c.turnSign < 0 {
					ts = 85
				}
				if c.isEV() {
					ts = 150
				}
				desired = math.Min(desired, math.Sqrt(ts*ts+2*350*math.Max(0, edgeLen-c.t)))
			}
		}
		c.braking = desired < c.speed-2 || c.speed < 2
		if c.speed > desired {
			c.speed = math.Max(desired, c.speed-1000*dt)
		} else {
			c.speed = math.Min(desired, c.speed+150*dt)
		}
		if c.speed < 3 && blocker != nil && (c.inTurn || blocker.inTurn) {
			c.wait += dt
			if c.wait > 2.5 {
				c.ghost = 1.5
				c.wait = 0
			}
		} else if c.speed > 20 {
			c.wait = 0
		}
		c.t += c.speed * dt
		if !c.inTurn && c.t >= edgeLen {
			c.t -= edgeLen
			g.startTurn(c)
		}
		if c.inTurn && c.t >= c.turnLen {
			c.t -= c.turnLen
			c.ai, c.aj, c.bi, c.bj = c.bi, c.bj, c.ni, c.nj
			c.inTurn = false
			g.setNext(c)
		}
		g.carPose(c)
	}
	g.cars = keep

	g.nextEV -= dt
	if g.nextEV <= 0 {
		g.nextEV = 30 + g.rng.Float64()*40
		kinds := []int{evAmbulance, evFire, evPolice}
		g.dispatchEV(kinds[g.rng.Intn(len(kinds))])
	}
}

func (g *Game) dispatchEV(kind int) {
	n := 0
	for _, c := range g.cars {
		if c.isEV() {
			n++
		}
	}
	if n >= 3 {
		return
	}
	// spawn somewhere nearish to the player so they actually meet it
	c := g.makeCar(kind)
	c.life = 45
	for try := 0; try < 80; try++ {
		i, j := g.rng.Intn(nodesX), g.rng.Intn(nodesY)
		ni, nj := g.pickNext(i, j, -1, -1)
		if g.edgeBlocked(i, j, ni, nj) {
			continue
		}
		c.ai, c.aj, c.bi, c.bj = i, j, ni, nj
		c.t = 10
		c.inTurn = false
		g.carPose(c)
		d := math.Hypot(c.x-g.truck.X, c.y-g.truck.Y)
		if (d > 750 && d < 1500) || try > 70 {
			break
		}
	}
	g.setNext(c)
	c.speed = 150
	g.carPose(c)
	g.cars = append(g.cars, c)
}

// ---------- police chase ----------

func (g *Game) startChase(reason string, amount, pts int) {
	if g.chaser != nil || len(g.trail) < 2 {
		return
	}
	var best *Car
	bestIdx, bestD := 0, 1e9
	for _, c := range g.cars {
		if c.kind != kPolice || c.crashed > 0 {
			continue
		}
		if math.Hypot(c.x-g.truck.X, c.y-g.truck.Y) > 380 {
			continue
		}
		for k := len(g.trail) - 1; k >= 0; k-- {
			d := math.Hypot(g.trail[k][0]-c.x, g.trail[k][1]-c.y)
			if d < bestD {
				bestD, bestIdx, best = d, k, c
			}
		}
	}
	if best == nil || bestD > 140 {
		// fall back to a police car right near the truck
		for _, c := range g.cars {
			if c.kind == kPolice && c.crashed <= 0 && math.Hypot(c.x-g.truck.X, c.y-g.truck.Y) < 200 {
				best, bestIdx = c, len(g.trail)-1
				break
			}
		}
		if best == nil {
			return
		}
	}
	best.chase = true
	best.trailIdx = bestIdx
	g.chaser = best
	g.chaseReason, g.chaseFine, g.chasePts = reason, amount, pts
	g.chaseTime = 0
	g.stopTime = 0
	g.flash("POLICE! Pull over and stop - " + reason)
}

func (g *Game) updateChaser(c *Car) {
	// follow the breadcrumb trail the truck left
	td := math.Hypot(g.truck.X-c.x, g.truck.Y-c.y)
	target := math.Min(560, math.Abs(g.truck.Speed)+150)
	if td < 130 {
		target = 0
	} else if td < 200 {
		target = math.Abs(g.truck.Speed)
	}
	if c.speed < target {
		c.speed = math.Min(target, c.speed+400*dt)
	} else {
		c.speed = math.Max(target, c.speed-900*dt)
	}
	move := c.speed * dt
	for move > 0 && c.trailIdx < len(g.trail) {
		p := g.trail[c.trailIdx]
		dx, dy := p[0]-c.x, p[1]-c.y
		d := math.Hypot(dx, dy)
		if d < 0.5 {
			c.trailIdx++
			continue
		}
		step := math.Min(d, move)
		c.x += dx / d * step
		c.y += dy / d * step
		k := math.Min(1, dt*10)
		nhx, nhy := c.hx+(dx/d-c.hx)*k, c.hy+(dy/d-c.hy)*k
		l := math.Hypot(nhx, nhy)
		c.hx, c.hy = nhx/l, nhy/l
		move -= step
		if step >= d {
			c.trailIdx++
		}
	}
}

func (g *Game) endChase() {
	if g.chaser != nil {
		g.respawnCar(g.chaser)
		g.chaser = nil
	}
}

// ---------- images ----------

func makeCarImg(w, h int, c color.RGBA, style int) *ebiten.Image {
	img := ebiten.NewImage(w, h)
	fw, fh := float64(w), float64(h)
	dark := color.RGBA{c.R * 2 / 3, c.G * 2 / 3, c.B * 2 / 3, 255}
	fillRect(img, 0, 0, fw, fh, dark)
	fillRect(img, 2, 2, fw-4, fh-4, c)
	glass := rgb(140, 190, 220)
	switch style {
	case 0, 3: // car / police
		fillRect(img, fw-16, 3, 7, fh-6, glass)
		fillRect(img, 6, 4, 5, fh-8, rgb(120, 160, 190))
		if style == 3 {
			for x := 2.0; x < fw-4; x += 8 {
				col := rgb(30, 60, 200)
				if int(x/8)%2 == 0 {
					col = rgb(250, 220, 0)
				}
				fillRect(img, x, 2, 8, 4, col)
				fillRect(img, x, fh-6, 8, 4, col)
			}
			fillRect(img, fw/2-3, 3, 6, fh-6, rgb(40, 40, 60))
		}
	case 1: // van
		fillRect(img, fw-12, 3, 6, fh-6, glass)
		fillRect(img, 4, 4, fw-20, fh-8, rgb(220, 220, 220))
	case 2: // bus
		fillRect(img, fw-6, 3, 4, fh-6, glass)
		for x := 6.0; x < fw-12; x += 12 {
			fillRect(img, x, 3, 8, 3, glass)
			fillRect(img, x, fh-6, 8, 3, glass)
		}
	case 4: // ambulance
		fillRect(img, fw-12, 3, 6, fh-6, glass)
		for x := 2.0; x < fw-14; x += 8 {
			if int(x/8)%2 == 0 {
				fillRect(img, x, 2, 8, 4, rgb(30, 160, 60))
				fillRect(img, x, fh-6, 8, 4, rgb(30, 160, 60))
			}
		}
		fillRect(img, fw/2-8, fh/2-2, 16, 4, rgb(30, 160, 60))
		fillRect(img, fw/2-2, fh/2-8, 4, 16, rgb(30, 160, 60))
	case 5: // fire engine
		fillRect(img, fw-12, 3, 6, fh-6, glass)
		fillRect(img, 6, fh/2-4, fw-24, 8, rgb(200, 200, 200))
		for x := 8.0; x < fw-24; x += 8 {
			fillRect(img, x, fh/2-5, 2, 10, rgb(150, 150, 150))
		}
	}
	return img
}

// ---------- drawing helpers for dynamic objects ----------

func drawRot(dst, img *ebiten.Image, x, y, a float64) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(-float64(w)/2, -float64(h)/2)
	op.GeoM.Rotate(a)
	op.GeoM.Translate(x, y)
	dst.DrawImage(img, op)
}

func (g *Game) drawCar(screen *ebiten.Image, c *Car, cx, cy float64) {
	sx, sy := c.x-cx, c.y-cy
	if sx < -100 || sy < -100 || sx > screenW+100 || sy > screenH+100 {
		return
	}
	drawRot(screen, c.img, sx, sy, c.angle())
	rx, ry := -c.hy, c.hx
	hw := c.width/2 - 2
	at := func(along, side float64) (float64, float64) {
		return sx + c.hx*along + rx*side, sy + c.hy*along + ry*side
	}
	blink := math.Mod(g.clock, 0.7) < 0.35
	amber := rgb(255, 170, 0)
	ind := func(side float64) {
		x1, y1 := at(c.half-2, side*hw)
		x2, y2 := at(-c.half+2, side*hw)
		fillCircle(screen, x1, y1, 3.5, amber)
		fillCircle(screen, x2, y2, 3.5, amber)
	}
	if blink {
		switch {
		case c.crashed > 0:
			ind(1)
			ind(-1)
		case c.pulling || (c.pull > 0.05 && !c.isEV()):
			ind(-1)
		case c.turnSign != 0 && !c.chase && (c.inTurn || c.t > edgeLen-170):
			ind(float64(c.turnSign))
		}
	}
	if c.braking {
		x1, y1 := at(-c.half+1, hw-1)
		x2, y2 := at(-c.half+1, -hw+1)
		fillCircle(screen, x1, y1, 2.5, rgb(255, 30, 30))
		fillCircle(screen, x2, y2, 2.5, rgb(255, 30, 30))
	}
	if c.isEV() || c.chase {
		ph := math.Mod(g.clock, 0.3) < 0.15
		b1, b2 := rgb(40, 90, 255), rgb(20, 30, 90)
		if c.kind == kFire || c.kind == kAmbulance {
			// blue lights too in the UK
		}
		if !ph {
			b1, b2 = b2, b1
		}
		x1, y1 := at(c.half*0.35, 5)
		x2, y2 := at(c.half*0.35, -5)
		fillCircle(screen, x1, y1, 4, b1)
		fillCircle(screen, x2, y2, 4, b2)
		if ph {
			fillCircle(screen, x1, y1, 14, color.NRGBA{40, 90, 255, 50})
		} else {
			fillCircle(screen, x2, y2, 14, color.NRGBA{40, 90, 255, 50})
		}
	}
}

func (g *Game) drawIncident(screen *ebiten.Image, in *Incident, cx, cy float64) {
	if !g.visible(in.mx, in.my, 200) {
		return
	}
	r := rand.New(rand.NewSource(in.seed))
	// local axes: a = along road, b = across road
	ax, ay, bx, by := 1.0, 0.0, 0.0, 1.0
	if !in.horiz {
		ax, ay, bx, by = 0, 1, 1, 0
	}
	P := func(a, b float64) (float64, float64) {
		return in.mx + ax*a + bx*b - cx, in.my + ay*a + by*b - cy
	}
	cone := func(a, b float64) {
		x, y := P(a, b)
		fillCircle(screen, x, y, 6, rgb(255, 110, 0))
		fillCircle(screen, x, y, 2.5, color.White)
	}
	ang := 0.0
	if !in.horiz {
		ang = math.Pi / 2
	}
	if in.kind == incRTC {
		x, y := P(0, 0)
		fillCircle(screen, x+8, y+4, 30, color.NRGBA{20, 20, 25, 120})
		for k := 0; k < 30; k++ {
			gx, gy := P(r.Float64()*80-40, r.Float64()*80-40)
			fillRect(screen, gx, gy, 2, 2, rgb(190, 220, 240))
		}
		c1 := makeCarCached(carColors[r.Intn(len(carColors))], 0)
		c2 := makeCarCached(carColors[r.Intn(len(carColors))], 0)
		x1, y1 := P(-18, 12)
		x2, y2 := P(16, -14)
		drawRot(screen, c1, x1, y1, ang+0.5)
		drawRot(screen, c2, x2, y2, ang-1.1)
		px, py := P(48, 34)
		drawRot(screen, makeCarCached(rgb(245, 245, 245), 3), px, py, ang)
		ph := math.Mod(g.clock, 0.3) < 0.15
		col := rgb(40, 90, 255)
		if ph {
			fillCircle(screen, px, py, 16, color.NRGBA{40, 90, 255, 60})
		}
		fillCircle(screen, px, py, 4, col)
		amx, amy := P(-48, -34)
		drawRot(screen, makeCarCached(rgb(250, 220, 0), 4), amx, amy, ang)
		if !ph {
			fillCircle(screen, amx, amy, 16, color.NRGBA{40, 90, 255, 60})
		}
		for _, end := range []float64{-66, 66} {
			for b := -56.0; b <= 56; b += 16 {
				cone(end, b)
			}
		}
		lx, ly := P(0, -50)
		drawTextShadow(screen, "POLICE - ROAD CLOSED", faceTiny, lx, ly, color.White, 1)
	} else {
		x, y := P(0, 0)
		pw, ph := 70.0, 90.0
		if !in.horiz {
			pw, ph = ph, pw
		}
		fillRect(screen, x-pw/2, y-ph/2, pw, ph, rgb(110, 85, 60))
		fillRect(screen, x-pw/2+8, y-ph/2+8, pw-16, ph-16, rgb(90, 68, 48))
		dx, dy := P(-20, 30)
		fillRect(screen, dx-20, dy-13, 40, 26, rgb(240, 190, 20))
		fillRect(screen, dx-4, dy-10, 14, 20, rgb(60, 60, 60))
		ex, ey := P(10, 0)
		strokeLine(screen, dx, dy, ex, ey, 6, rgb(220, 170, 20))
		fillRect(screen, ex-6, ey-6, 12, 12, rgb(80, 80, 80))
		for k := 0; k < 3; k++ {
			wx, wy := P(r.Float64()*80-40, r.Float64()*80-40)
			fillCircle(screen, wx, wy, 7, rgb(255, 120, 0))
			fillCircle(screen, wx, wy, 4, color.White)
		}
		for _, end := range []float64{-70, 70} {
			for b := -62.0; b < 62; b += 12 {
				bx1, by1 := P(end, b)
				col := rgb(220, 30, 30)
				if int((b+62)/12)%2 == 0 {
					col = color.RGBA{240, 240, 240, 255}
				}
				fillRect(screen, bx1-4, by1-4, 8, 12, col)
			}
		}
		lx, ly := P(0, -50)
		drawTextShadow(screen, "ROAD WORKS - ROAD CLOSED", faceTiny, lx, ly, rgb(255, 210, 60), 1)
	}
}

var carCache = map[[4]int]*ebiten.Image{}

func makeCarCached(c color.RGBA, style int) *ebiten.Image {
	k := [4]int{int(c.R), int(c.G), int(c.B), style}
	if img, ok := carCache[k]; ok {
		return img
	}
	w, h := 44, 22
	switch style {
	case 3:
		w = 46
	case 4:
		w, h = 56, 26
	}
	img := makeCarImg(w, h, c, style)
	carCache[k] = img
	return img
}

func (g *Game) drawLights(screen *ebiten.Image, cx, cy float64) {
	for i := 0; i < nodesX; i++ {
		for j := 0; j < nodesY; j++ {
			nx, ny := nodePos(i, j)
			if !g.visible(nx, ny, 150) {
				continue
			}
			for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				fi, fj := i-d[0], j-d[1] // approaching from neighbour
				if fi < 0 || fj < 0 || fi >= nodesX || fj >= nodesY {
					continue
				}
				dx, dy := float64(d[0]), float64(d[1])
				lx, ly := dy, -dx
				px := nx - dx*(64+26) + lx*(64+14) - cx
				py := ny - dy*(64+26) + ly*(64+14) - cy
				axis := 0
				if d[0] == 0 {
					axis = 1
				}
				st := g.light(i, j, axis)
				fillRect(screen, px-9, py-9, 18, 18, rgb(25, 25, 25))
				var col color.RGBA
				switch st {
				case 0:
					col = rgb(40, 230, 90)
				case 1:
					col = rgb(255, 180, 0)
				default:
					col = rgb(240, 40, 40)
				}
				fillCircle(screen, px, py, 6, col)
				fillCircle(screen, px, py, 11, color.NRGBA{col.R, col.G, col.B, 45})
				if st == 3 {
					fillCircle(screen, px+5, py+5, 3, rgb(255, 180, 0))
				}
			}
		}
	}
}

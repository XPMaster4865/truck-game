package main

import (
	"fmt"
	"image/color"
	"math"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// ---------- traffic lights ----------

// light returns 0 green, 1 amber, 2 red, 3 red+amber. axis 0 = east/west traffic, 1 = north/south.
func (g *Game) light(node, axis int) int {
	t := math.Mod(g.clock+g.nodes[node].LightOff, 22)
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
	incRTC        = iota
	incWorks      // full road closure
	incWorksLight // one lane closed, temporary traffic lights
)

type Incident struct {
	a, b   int // closed lane belongs to traffic travelling a -> b
	kind   int
	timer  float64
	mx, my float64
	horiz  bool
	seed   int64
	phase0 float64
}

const zoneHalf = 50.0

func (in *Incident) rect() (x0, y0, x1, y1 float64) {
	if in.kind == incWorksLight {
		// only the a->b lane (left of a->b direction) is closed
		var lx, ly float64
		if in.horiz {
			dx := 1.0
			if in.b < in.a {
				dx = -1
			}
			lx, ly = 0, -dx
		} else {
			dy := 1.0
			if in.b < in.a {
				dy = -1
			}
			lx, ly = dy, 0
		}
		cx, cy := in.mx+lx*32, in.my+ly*32
		if in.horiz {
			return cx - zoneHalf, cy - 30, cx + zoneHalf, cy + 30
		}
		return cx - 30, cy - zoneHalf, cx + 30, cy + zoneHalf
	}
	if in.horiz {
		return in.mx - 70, in.my - 64, in.mx + 70, in.my + 64
	}
	return in.mx - 64, in.my - 70, in.mx + 64, in.my + 70
}

// tempLight: state for traffic travelling a->b through a lane-closure. 0 green, 2 red.
func (g *Game) tempLight(in *Incident, a, b int) int {
	t := math.Mod(g.clock+in.phase0, 30)
	if a != in.a {
		t = math.Mod(t+15, 30)
	}
	if t < 8 {
		return 0
	}
	if t < 9.5 {
		return 1
	}
	return 2
}

func sameEdge(a, b, c, d int) bool { return (a == c && b == d) || (a == d && b == c) }

func (g *Game) edgeBlocked(a, b int) bool {
	for _, in := range g.incidents {
		if in.kind != incWorksLight && sameEdge(a, b, in.a, in.b) {
			return true
		}
	}
	return false
}

func (g *Game) laneWorks(a, b int) *Incident {
	for _, in := range g.incidents {
		if in.kind == incWorksLight && sameEdge(a, b, in.a, in.b) {
			return in
		}
	}
	return nil
}

func (g *Game) visible(x, y, margin float64) bool {
	return x > g.camX-margin && x < g.camX+screenW+margin && y > g.camY-margin && y < g.camY+screenH+margin
}

func (g *Game) spawnIncident() {
	for try := 0; try < 60; try++ {
		a := g.rng.Intn(len(g.nodes))
		b := g.nodes[a].Nb[g.rng.Intn(len(g.nodes[a].Nb))]
		if g.isMotor(a, b) || g.edgeBlocked(a, b) || g.laneWorks(a, b) != nil {
			continue
		}
		cam := false
		for _, c := range g.cameras {
			if sameEdge(a, b, c.a, c.b) {
				cam = true
			}
		}
		if cam {
			continue
		}
		na, nb := g.nodes[a], g.nodes[b]
		mx, my := (na.X+nb.X)/2, (na.Y+nb.Y)/2
		if g.visible(mx, my, 300) {
			continue
		}
		in := &Incident{a: a, b: b, mx: mx, my: my, horiz: na.J == nb.J, seed: g.rng.Int63(), phase0: g.rng.Float64() * 24}
		name := g.edgeName(a, b)
		town := cities[na.City].Name
		switch r := g.rng.Float64(); {
		case r < 0.4:
			in.kind = incRTC
			in.timer = 60 + g.rng.Float64()*50
			g.newsFlash(fmt.Sprintf("Collision on %s, %s.\nRoad closed - emergency services on scene.", name, town))
		case r < 0.65:
			in.kind = incWorks
			in.timer = 120 + g.rng.Float64()*90
			g.newsFlash(fmt.Sprintf("Emergency roadworks on %s, %s.\nRoad closed - find another route.", name, town))
		default:
			in.kind = incWorksLight
			in.timer = 150 + g.rng.Float64()*120
			g.newsFlash(fmt.Sprintf("Roadworks on %s, %s.\nTemporary traffic lights - expect delays.", name, town))
		}
		g.incidents = append(g.incidents, in)
		for _, c := range g.cars {
			if c.chase {
				continue
			}
			if sameEdge(c.a, c.b, a, b) || (c.inTurn && sameEdge(c.b, c.n, a, b)) {
				g.respawnCar(c)
			}
		}
		if in.kind == incRTC {
			g.dispatchEV(kAmbulance)
		}
		return
	}
}

func (g *Game) updateIncidents() {
	g.nextIncident -= dt
	if g.nextIncident <= 0 {
		g.nextIncident = 35 + g.rng.Float64()*30
		if len(g.incidents) < 6 {
			g.spawnIncident()
		}
	}
	keep := g.incidents[:0]
	for _, in := range g.incidents {
		in.timer -= dt
		if in.timer > 0 {
			keep = append(keep, in)
		} else if in.kind == incWorksLight {
			g.newsFlash(g.edgeName(in.a, in.b) + ": roadworks finished,\ntemporary lights removed.")
		} else {
			g.newsFlash(g.edgeName(in.a, in.b) + " has now reopened.")
		}
	}
	g.incidents = keep

	// random breakdowns on the motorway hard shoulder
	g.nextBreakdown -= dt
	if g.nextBreakdown <= 0 {
		g.nextBreakdown = 20 + g.rng.Float64()*25
		for _, c := range g.cars {
			if !c.isEV() && !c.chase && !c.inTurn && g.isMotor(c.a, c.b) && c.t > 800 && c.t < g.edgeLen(c.a, c.b)-800 && !g.visible(c.x, c.y, 100) {
				c.broken = 30 + g.rng.Float64()*30
				break
			}
		}
	}
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
	kLorry
)

type Car struct {
	a, b, n            int
	inTurn             bool
	t                  float64
	p0x, p0y, p1x, p1y float64
	p2x, p2y, turnLen  float64
	speed, factor      float64
	kind               int
	half, width        float64
	img                *ebiten.Image
	x, y, hx, hy       float64
	wait, ghost        float64
	crashed, broken    float64
	hazard             float64
	braking            bool
	turnSign           int
	lane, mlane        float64
	off                float64 // current smoothed lateral offset
	pull               float64
	pulling            bool
	life               float64
	chase              bool
	trailIdx           int
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
	case kLorry:
		c.half, c.width = 44, 32
		c.img = makeTruckImg(carColors[g.rng.Intn(len(carColors))], &places[g.rng.Intn(9)].Color, 1)
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
	c.mlane = c.lane
	return c
}

func (g *Game) newCar(kind int) *Car {
	c := g.makeCar(kind)
	g.respawnCar(c)
	return c
}

func (g *Game) startEdge(c *Car) {
	c.mlane = c.lane
	if g.isMotor(c.a, c.b) && !c.isEV() {
		if c.kind == kLorry || c.kind == kBus || g.rng.Float64() < 0.55 {
			c.mlane = motorLaneOff // left (slow) lane
		}
	}
}

func (g *Game) respawnCar(c *Car) {
	c.chase = false
	c.crashed, c.broken, c.ghost, c.wait, c.pull = 0, 0, 0, 0, 0
	for try := 0; try < 80; try++ {
		a := g.rng.Intn(len(g.nodes))
		b := g.pickNext(a, -1)
		if g.edgeBlocked(a, b) {
			continue
		}
		c.a, c.b = a, b
		c.inTurn = false
		g.startEdge(c)
		L := g.edgeLen(a, b)
		c.t = 20 + g.rng.Float64()*(L-140)
		c.off = g.laneTarget(c)
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
		if ok {
			break
		}
	}
	g.setNext(c)
	c.speed = g.carLimit(c) * 0.7
	g.carPose(c)
}

func (g *Game) pickNext(at, prev int) int {
	var opts []int
	back := -1
	for _, nb := range g.nodes[at].Nb {
		if nb == prev {
			back = nb
			continue
		}
		if g.edgeBlocked(at, nb) {
			continue
		}
		opts = append(opts, nb)
	}
	if len(opts) == 0 {
		if back >= 0 {
			return back
		}
		return g.nodes[at].Nb[0]
	}
	for _, o := range opts {
		if g.isMotor(at, o) && g.rng.Float64() < 0.5 {
			return o
		}
	}
	return opts[g.rng.Intn(len(opts))]
}

func (g *Game) setNext(c *Car) {
	c.n = g.pickNext(c.b, c.a)
	dx, dy := g.edgeDir(c.a, c.b)
	ex, ey := g.edgeDir(c.b, c.n)
	cr := dx*ey - dy*ex
	switch {
	case cr > 0.5:
		c.turnSign = 1
	case cr < -0.5:
		c.turnSign = -1
	case dx*ex+dy*ey < -0.5:
		c.turnSign = 1
	default:
		c.turnSign = 0
	}
}

func (g *Game) carLimit(c *Car) float64 {
	lim := g.edgeLimitMph(c.a, c.b)
	if c.kind == kLorry || c.kind == kBus {
		lim = math.Min(lim, 56)
	}
	return lim / mphK * c.factor
}

func smooth01(v float64) float64 {
	v = math.Max(0, math.Min(1, v))
	return v * v * (3 - 2*v)
}

// laneTarget is the lateral offset the car wants right now.
func (g *Game) laneTarget(c *Car) float64 {
	off := c.lane
	L := g.edgeLen(c.a, c.b)
	if c.mlane != c.lane {
		k := smooth01(math.Min(c.t, L-c.t) / 600)
		off = c.lane + (c.mlane-c.lane)*k
	}
	if c.broken > 0 {
		off = 160
	}
	if w := g.laneWorks(c.a, c.b); w != nil && c.a == w.a {
		// closed lane: swing over to the other side through the works
		d := math.Abs(c.t - L/2)
		k := smooth01((zoneHalf + 60 - d) / 50)
		off = off + (-laneOff-off)*k
	}
	return off + 24*c.pull
}

func (g *Game) startTurn(c *Car) {
	B := g.nodes[c.b]
	dx, dy := g.edgeDir(c.a, c.b)
	ex, ey := g.edgeDir(c.b, c.n)
	lx, ly := dy, -dx
	mx, my := ey, -ex
	off := c.lane
	c.p0x, c.p0y = B.X-dx*64+lx*off, B.Y-dy*64+ly*off
	c.p2x, c.p2y = B.X+ex*64+mx*off, B.Y+ey*64+my*off
	switch {
	case dx*ex+dy*ey > 0.5:
		c.p1x, c.p1y = (c.p0x+c.p2x)/2, (c.p0y+c.p2y)/2
	case dx*ex+dy*ey < -0.5:
		c.p1x, c.p1y = B.X+dx*30, B.Y+dy*30
	default:
		c.p1x, c.p1y = B.X+lx*off+mx*off, B.Y+ly*off+my*off
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
		A := g.nodes[c.a]
		dx, dy := g.edgeDir(c.a, c.b)
		lx, ly := dy, -dx
		c.x = A.X + dx*(64+c.t) + lx*c.off
		c.y = A.Y + dy*(64+c.t) + ly*c.off
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
				continue
			}
		}
		keep = append(keep, c)
		if c.hazard > 0 {
			c.hazard -= dt
		}
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
		if c.broken > 0 {
			c.broken -= dt
			c.speed = math.Max(0, c.speed-400*dt)
			c.off += (g.laneTarget(c) - c.off) * math.Min(1, dt*1.5)
			if c.speed > 0 {
				c.t += c.speed * dt
			}
			g.carPose(c)
			continue
		}
		if !c.inTurn && g.edgeBlocked(c.b, c.n) {
			g.setNext(c)
		}
		L := g.edgeLen(c.a, c.b)
		motor := g.isMotor(c.a, c.b) && !c.inTurn
		desired := g.carLimit(c)
		turnSpeed := func() float64 {
			if c.isEV() {
				return 150
			}
			if c.turnSign < 0 {
				return 85
			}
			return 105
		}
		if c.inTurn {
			desired = math.Min(desired, turnSpeed())
		}
		rx, ry := -c.hy, c.hx
		var blocker *Car

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
				if math.Abs(ox) > 260 || math.Abs(oy) > 260 {
					continue
				}
				along := ox*c.hx + oy*c.hy
				lat := ox*rx + oy*ry
				latLim := 16.0
				if (o.pull > 0.5 || o.broken > 0) && !c.inTurn {
					latLim = 6
				}
				look := 170.0
				if motor {
					look = 240
				}
				if along > 0 && along < look && math.Abs(lat) < latLim {
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
			if along > 0 && along < 200 && math.Abs(lat) < 30 {
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
			// temporary lights at lane-closure roadworks
			if w := g.laneWorks(c.a, c.b); w != nil && !c.isEV() {
				stop := math.Max(0, L/2-zoneHalf-100-c.half)
				dist := stop - c.t
				if dist > -2 && g.tempLight(w, c.a, c.b) != 0 {
					st := g.tempLight(w, c.a, c.b)
					bd := c.speed * c.speed / (2 * 600)
					if !(st == 1 && dist < bd*0.7) {
						desired = math.Min(desired, follow(dist))
					}
				}
			}
			stopT := L - 28 - c.half
			dist := stopT - c.t
			axis := 0
			if g.nodes[c.a].J != g.nodes[c.b].J {
				axis = 1
			}
			st := g.light(c.b, axis)
			if !c.isEV() && st == 0 {
				B := g.nodes[c.b]
				for _, e := range evs {
					if math.Hypot(e.x-B.X, e.y-B.Y) < 230 && dist > -2 {
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
				ts := turnSpeed()
				desired = math.Min(desired, math.Sqrt(ts*ts+2*350*math.Max(0, L-c.t)))
			}
		}
		prev := c.speed
		c.braking = desired < c.speed-2 || c.speed < 2
		if c.speed > desired {
			c.speed = math.Max(desired, c.speed-1000*dt)
		} else {
			c.speed = math.Min(desired, c.speed+150*dt)
		}
		// hazard warning lights when braking hard on the motorway
		if motor && prev-c.speed > 700*dt && prev > 250 {
			c.hazard = 3
		}
		headOn := blocker != nil && blocker.hx*c.hx+blocker.hy*c.hy < -0.5
		if c.speed < 3 && blocker != nil && (c.inTurn || blocker.inTurn || headOn) {
			c.wait += dt
			if c.wait > 2.5 {
				c.ghost = 1.5
				c.wait = 0
			}
		} else if c.speed > 20 {
			c.wait = 0
		}
		c.t += c.speed * dt
		if !c.inTurn {
			c.off += (g.laneTarget(c) - c.off) * math.Min(1, dt*3)
		}
		if !c.inTurn && c.t >= L {
			c.t -= L
			g.startTurn(c)
		}
		if c.inTurn && c.t >= c.turnLen {
			c.t -= c.turnLen
			c.a, c.b = c.b, c.n
			c.inTurn = false
			g.startEdge(c)
			c.off = c.lane
			g.setNext(c)
		}
		g.carPose(c)
	}
	g.cars = keep

	// keep the M11 busy: move an off-screen city car onto the motorway when it gets quiet
	g.motorCheck -= dt
	if g.motorCheck <= 0 {
		g.motorCheck = 1.5
		n := 0
		for _, c := range g.cars {
			if g.isMotor(c.a, c.b) {
				n++
			}
		}
		if n < 16 {
			for _, c := range g.cars {
				if c.isEV() || c.chase || c.kind == kPolice || g.isMotor(c.a, c.b) || g.visible(c.x, c.y, 300) {
					continue
				}
				c.a, c.b = g.motorA, g.motorB
				if g.rng.Intn(2) == 0 {
					c.a, c.b = g.motorB, g.motorA
				}
				c.inTurn = false
				c.crashed, c.broken, c.pull = 0, 0, 0
				g.startEdge(c)
				L := g.edgeLen(c.a, c.b)
				for try := 0; try < 20; try++ {
					c.t = 900 + g.rng.Float64()*(L-1800)
					c.off = g.laneTarget(c)
					g.carPose(c)
					if !g.visible(c.x, c.y, 400) {
						break
					}
				}
				g.setNext(c)
				c.speed = g.carLimit(c)
				break
			}
		}
	}

	g.nextEV -= dt
	if g.nextEV <= 0 {
		g.nextEV = 30 + g.rng.Float64()*40
		kinds := []int{kAmbulance, kFire, kPoliceEV}
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
	c := g.makeCar(kind)
	c.life = 45
	for try := 0; try < 80; try++ {
		a := g.rng.Intn(len(g.nodes))
		b := g.pickNext(a, -1)
		if g.edgeBlocked(a, b) {
			continue
		}
		c.a, c.b = a, b
		c.t = 10
		c.inTurn = false
		g.startEdge(c)
		c.off = c.lane
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
		best = nil
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
	td := math.Hypot(g.truck.X-c.x, g.truck.Y-c.y)
	target := math.Min(600, math.Abs(g.truck.Speed)+150)
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
	case 0, 3:
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
	case 1:
		fillRect(img, fw-12, 3, 6, fh-6, glass)
		fillRect(img, 4, 4, fw-20, fh-8, rgb(220, 220, 220))
	case 2:
		fillRect(img, fw-6, 3, 4, fh-6, glass)
		for x := 6.0; x < fw-12; x += 12 {
			fillRect(img, x, 3, 8, 3, glass)
			fillRect(img, x, fh-6, 8, 3, glass)
		}
	case 4:
		fillRect(img, fw-12, 3, 6, fh-6, glass)
		for x := 2.0; x < fw-14; x += 8 {
			if int(x/8)%2 == 0 {
				fillRect(img, x, 2, 8, 4, rgb(30, 160, 60))
				fillRect(img, x, fh-6, 8, 4, rgb(30, 160, 60))
			}
		}
		fillRect(img, fw/2-8, fh/2-2, 16, 4, rgb(30, 160, 60))
		fillRect(img, fw/2-2, fh/2-8, 4, 16, rgb(30, 160, 60))
	case 5:
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

// drawLamps draws indicator / hazard / brake lamps for a vehicle at screen pos sx,sy.
// ind: 0 none, -1 left, 1 right, 2 hazards.
func (g *Game) drawLamps(screen *ebiten.Image, sx, sy, hx, hy, half, width float64, ind int, braking bool) {
	rx, ry := -hy, hx
	hw := width/2 - 2
	at := func(along, side float64) (float64, float64) {
		return sx + hx*along + rx*side, sy + hy*along + ry*side
	}
	amber := rgb(255, 170, 0)
	side := func(s float64) {
		x1, y1 := at(half-2, s*hw)
		x2, y2 := at(-half+2, s*hw)
		fillCircle(screen, x1, y1, 3.5, amber)
		fillCircle(screen, x2, y2, 3.5, amber)
		fillCircle(screen, x1, y1, 8, color.NRGBA{255, 170, 0, 70})
		fillCircle(screen, x2, y2, 8, color.NRGBA{255, 170, 0, 70})
	}
	if math.Mod(g.clock, 0.7) < 0.35 {
		switch ind {
		case 2:
			side(1)
			side(-1)
		case -1, 1:
			side(float64(ind))
		}
	}
	if braking {
		x1, y1 := at(-half+1, hw-1)
		x2, y2 := at(-half+1, -hw+1)
		fillCircle(screen, x1, y1, 2.5, rgb(255, 30, 30))
		fillCircle(screen, x2, y2, 2.5, rgb(255, 30, 30))
	}
}

func (g *Game) drawCar(screen *ebiten.Image, c *Car, cx, cy float64) {
	sx, sy := c.x-cx, c.y-cy
	if sx < -100 || sy < -100 || sx > screenW+100 || sy > screenH+100 {
		return
	}
	drawRot(screen, c.img, sx, sy, c.angle())
	ind := 0
	switch {
	case c.crashed > 0 || c.broken > 0 || c.hazard > 0:
		ind = 2
	case c.pulling || (c.pull > 0.05 && !c.isEV()):
		ind = -1
	case c.turnSign != 0 && !c.chase && (c.inTurn || c.t > g.edgeLen(c.a, c.b)-170):
		ind = c.turnSign
	}
	g.drawLamps(screen, sx, sy, c.hx, c.hy, c.half, c.width, ind, c.braking)
	if c.isEV() || c.chase {
		rx, ry := -c.hy, c.hx
		ph := math.Mod(g.clock, 0.3) < 0.15
		b1, b2 := rgb(40, 90, 255), rgb(20, 30, 90)
		if !ph {
			b1, b2 = b2, b1
		}
		x1, y1 := sx+c.hx*c.half*0.35+rx*5, sy+c.hy*c.half*0.35+ry*5
		x2, y2 := sx+c.hx*c.half*0.35-rx*5, sy+c.hy*c.half*0.35-ry*5
		fillCircle(screen, x1, y1, 4, b1)
		fillCircle(screen, x2, y2, 4, b2)
		if ph {
			fillCircle(screen, x1, y1, 16, color.NRGBA{40, 90, 255, 60})
		} else {
			fillCircle(screen, x2, y2, 16, color.NRGBA{40, 90, 255, 60})
		}
	}
}

func drawTempLight(screen *ebiten.Image, x, y float64, st int) {
	fillRect(screen, x-10, y-10, 20, 20, rgb(250, 200, 0))
	fillRect(screen, x-8, y-8, 16, 16, rgb(25, 25, 25))
	col := rgb(240, 40, 40)
	switch st {
	case 0:
		col = rgb(40, 230, 90)
	case 1:
		col = rgb(255, 180, 0)
	}
	fillCircle(screen, x, y, 6, col)
	fillCircle(screen, x, y, 12, color.NRGBA{col.R, col.G, col.B, 60})
}

func (g *Game) drawIncident(screen *ebiten.Image, in *Incident, cx, cy float64) {
	if !g.visible(in.mx, in.my, 250) {
		return
	}
	r := rand.New(rand.NewSource(in.seed))
	ax, ay := g.edgeDir(in.a, in.b) // along a->b
	bx, by := ay, -ax               // left of a->b (the closed lane side)
	P := func(a, b float64) (float64, float64) {
		return in.mx + ax*a + bx*b - cx, in.my + ay*a + by*b - cy
	}
	cone := func(a, b float64) {
		x, y := P(a, b)
		fillCircle(screen, x, y, 6, rgb(255, 110, 0))
		fillCircle(screen, x, y, 2.5, color.White)
	}
	ang := math.Atan2(ay, ax)
	switch in.kind {
	case incRTC:
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
		// hazards on the crashed cars
		if math.Mod(g.clock, 0.7) < 0.35 {
			for _, p := range [][2]float64{{x1, y1}, {x2, y2}} {
				fillCircle(screen, p[0]-14, p[1]-6, 3.5, rgb(255, 170, 0))
				fillCircle(screen, p[0]+14, p[1]+6, 3.5, rgb(255, 170, 0))
			}
		}
		px, py := P(48, 34)
		drawRot(screen, makeCarCached(rgb(245, 245, 245), 3), px, py, ang)
		ph := math.Mod(g.clock, 0.3) < 0.15
		if ph {
			fillCircle(screen, px, py, 16, color.NRGBA{40, 90, 255, 60})
		}
		fillCircle(screen, px, py, 4, rgb(40, 90, 255))
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
		drawTextShadow(screen, "POLICE - ROAD CLOSED", faceTiny, lx, ly, color.White, text.AlignCenter)
	case incWorks, incWorksLight:
		closedOnly := in.kind == incWorksLight
		x0, y0, x1, y1 := in.rect()
		fillRect(screen, x0-cx+6, y0-cy+6, x1-x0-12, y1-y0-12, rgb(110, 85, 60))
		fillRect(screen, x0-cx+14, y0-cy+14, x1-x0-28, y1-y0-28, rgb(90, 68, 48))
		bw := 30.0
		if closedOnly {
			bw = 32
		}
		dx, dy := P(-25, bw)
		if !closedOnly {
			dx, dy = P(-20, 30)
		}
		fillRect(screen, dx-14, dy-10, 28, 20, rgb(240, 190, 20))
		fillRect(screen, dx-4, dy-8, 10, 16, rgb(60, 60, 60))
		ex, ey := P(10, bw)
		strokeLine(screen, dx, dy, ex, ey, 5, rgb(220, 170, 20))
		fillRect(screen, ex-5, ey-5, 10, 10, rgb(80, 80, 80))
		for k := 0; k < 2; k++ {
			wx, wy := P(r.Float64()*80-40, bw+r.Float64()*20-10)
			fillCircle(screen, wx, wy, 7, rgb(255, 120, 0))
			fillCircle(screen, wx, wy, 4, color.White)
		}
		if closedOnly {
			// cones along the centre line + temporary traffic lights at both ends
			for a := -zoneHalf - 30; a <= zoneHalf+30; a += 16 {
				cone(a, 2)
			}
			for _, end := range []float64{-zoneHalf, zoneHalf} {
				for b := 6.0; b < 62; b += 12 {
					bx1, by1 := P(end, b)
					col := rgb(220, 30, 30)
					if int(b/12)%2 == 0 {
						col = rgb(240, 240, 240)
					}
					fillRect(screen, bx1-4, by1-4, 8, 8, col)
				}
			}
			L := g.edgeLen(in.a, in.b)
			_ = L
			// light for a->b traffic (before the works on its left side)
			lx, ly := P(-zoneHalf-100, 78)
			drawTempLight(screen, lx, ly, g.tempLight(in, in.a, in.b))
			// light for b->a traffic (approaching from the other end, their left = our right)
			lx2, ly2 := P(zoneHalf+100, -78)
			drawTempLight(screen, lx2, ly2, g.tempLight(in, in.b, in.a))
			// temporary stop lines
			s1x, s1y := P(-zoneHalf-100, 32)
			s2x, s2y := P(zoneHalf+100, -32)
			if in.horiz {
				fillRect(screen, s1x-2, s1y-30, 4, 60, color.White)
				fillRect(screen, s2x-2, s2y-30, 4, 60, color.White)
			} else {
				fillRect(screen, s1x-30, s1y-2, 60, 4, color.White)
				fillRect(screen, s2x-30, s2y-2, 60, 4, color.White)
			}
			tx, ty := P(0, 80)
			drawTextShadow(screen, "ROAD WORKS - TEMPORARY LIGHTS", faceTiny, tx, ty, rgb(255, 210, 60), text.AlignCenter)
		} else {
			for _, end := range []float64{-70, 70} {
				for b := -62.0; b < 62; b += 12 {
					bx1, by1 := P(end, b)
					col := rgb(220, 30, 30)
					if int((b+62)/12)%2 == 0 {
						col = rgb(240, 240, 240)
					}
					fillRect(screen, bx1-4, by1-4, 8, 12, col)
				}
			}
			lx, ly := P(0, -50)
			drawTextShadow(screen, "ROAD WORKS - ROAD CLOSED", faceTiny, lx, ly, rgb(255, 210, 60), text.AlignCenter)
		}
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
	for id, n := range g.nodes {
		if !g.visible(n.X, n.Y, 150) {
			continue
		}
		for _, from := range n.Nb {
			dx, dy := g.edgeDir(from, id)
			lx, ly := dy, -dx
			px := n.X - dx*(64+26) + lx*(64+14) - cx
			py := n.Y - dy*(64+26) + ly*(64+14) - cy
			axis := 0
			if math.Abs(dy) > 0.5 {
				axis = 1
			}
			st := g.light(id, axis)
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

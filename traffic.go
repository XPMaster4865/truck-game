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

// light returns 0 green, 1 amber, 2 red, 3 red+amber. axis 0 = mostly east/west traffic, 1 = north/south.
func (g *Game) light(node, axis int) int {
	t := math.Mod(g.clock+g.nodes[node].LightOff, 24)
	if axis == 1 {
		t = math.Mod(t+12, 24)
	}
	switch {
	case t < 8.5:
		return 0
	case t < 10.5:
		return 1
	case t < 23:
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

const zoneHalf = 50.0

type Incident struct {
	road      int
	s         float64
	closedDir int // for lane closures: traffic in this direction (+1 = a->b) loses its lane
	kind      int
	timer     float64
	mx, my    float64
	tx, ty    float64
	seed      int64
	phase0    float64
}

func (in *Incident) local(x, y float64) (a, b float64) {
	dx, dy := x-in.mx, y-in.my
	return dx*in.tx + dy*in.ty, dx*in.ty - dy*in.tx
}

func (in *Incident) contains(g *Game, x, y float64) bool {
	a, b := in.local(x, y)
	if in.kind == incWorksLight {
		if math.Abs(a) > zoneHalf {
			return false
		}
		if in.closedDir > 0 {
			return b > 3 && b < cityHalf+4
		}
		return b < -3 && b > -cityHalf-4
	}
	return math.Abs(a) < 70 && math.Abs(b) < cityHalf+6
}

// tempLight: state for traffic travelling in direction dir through a lane closure. 0 green, 1 amber, 2 red.
func (g *Game) tempLight(in *Incident, dir int) int {
	t := math.Mod(g.clock+in.phase0, 30)
	if dir != in.closedDir {
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

func (g *Game) roadBlocked(road int) bool {
	for _, in := range g.incidents {
		if in.kind != incWorksLight && in.road == road {
			return true
		}
	}
	return false
}

func (g *Game) laneWorks(road int) *Incident {
	for _, in := range g.incidents {
		if in.kind == incWorksLight && in.road == road {
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
		ri := g.rng.Intn(len(g.roads))
		rd := g.roads[ri]
		if rd.City < 0 || rd.Len < 700 || g.roadBlocked(ri) || g.laneWorks(ri) != nil {
			continue
		}
		cam := false
		for _, c := range g.cameras {
			if c.road == ri {
				cam = true
			}
		}
		if cam {
			continue
		}
		s := rd.Len / 2
		x, y, tx, ty := rd.at(s)
		if g.visible(x, y, 300) {
			continue
		}
		in := &Incident{road: ri, s: s, mx: x, my: y, tx: tx, ty: ty, seed: g.rng.Int63(), phase0: g.rng.Float64() * 30, closedDir: 1}
		if g.rng.Intn(2) == 0 {
			in.closedDir = -1
		}
		town := cities[rd.City].Name
		switch v := g.rng.Float64(); {
		case v < 0.4:
			in.kind = incRTC
			in.timer = 60 + g.rng.Float64()*50
			g.newsFlash(fmt.Sprintf("Collision on %s, %s.\nRoad closed - emergency services on scene.", rd.Name, town))
		case v < 0.65:
			in.kind = incWorks
			in.timer = 120 + g.rng.Float64()*90
			g.newsFlash(fmt.Sprintf("Emergency roadworks on %s, %s.\nRoad closed - find another route.", rd.Name, town))
		default:
			in.kind = incWorksLight
			in.timer = 150 + g.rng.Float64()*120
			g.newsFlash(fmt.Sprintf("Roadworks on %s, %s.\nTemporary traffic lights - expect delays.", rd.Name, town))
		}
		g.incidents = append(g.incidents, in)
		if in.kind != incWorksLight {
			for _, c := range g.cars {
				if c.chase {
					continue
				}
				if c.road == ri || (c.inTurn && c.nroad == ri) {
					g.respawnCar(c)
				}
			}
			g.routeDirty = true
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
			continue
		}
		name := g.roads[in.road].Name
		if in.kind == incWorksLight {
			g.newsFlash(name + ": roadworks finished,\ntemporary lights removed.")
		} else {
			g.newsFlash(name + " has now reopened.")
			g.routeDirty = true
		}
	}
	g.incidents = keep

	g.nextBreakdown -= dt
	if g.nextBreakdown <= 0 {
		g.nextBreakdown = 20 + g.rng.Float64()*25
		for _, c := range g.cars {
			if !c.isEV() && !c.chase && !c.inTurn && g.isMotor(c.road) && c.t > 900 && c.t < g.carL(c)-900 && !g.visible(c.x, c.y, 100) {
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
	road, dir, from, to int
	nroad, nto          int
	inTurn              bool
	t                   float64
	p0x, p0y, p1x, p1y  float64
	p2x, p2y, turnLen   float64
	speed, factor       float64
	kind                int
	half, width         float64
	img                 *ebiten.Image
	x, y, hx, hy        float64
	wait, ghost         float64
	crashed, broken     float64
	hazard              float64
	braking             bool
	turnSign            int
	lane, mlane         float64
	off                 float64
	pull                float64
	pulling             bool
	life                float64
	chase               bool
	trailIdx            int
	stopT               float64 // time spent stopped at a STOP line
	lineWait            float64
	cleared, yielded    bool
	honkCD              float64
	truckWait           float64
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
		c.lane = 8
	case kFire:
		c.half, c.width = 36, 28
		c.img = makeCarImg(72, 28, rgb(210, 20, 20), 5)
		c.factor = 1.45
		c.lane = 8
	case kPoliceEV:
		c.half, c.width = 23, 22
		c.img = makeCarImg(46, 22, rgb(245, 245, 245), 3)
		c.factor = 1.7
		c.lane = 8
	}
	c.mlane = c.lane
	return c
}

func (g *Game) newCar(kind int) *Car {
	c := g.makeCar(kind)
	g.respawnCar(c)
	return c
}

func (g *Game) carL(c *Car) float64 {
	return g.roads[c.road].Len - g.nodes[c.from].R - g.nodes[c.to].R
}

func (g *Game) sOf(c *Car, t float64) float64 {
	if c.dir > 0 {
		return g.nodes[c.from].R + t
	}
	return g.roads[c.road].Len - g.nodes[c.from].R - t
}

func (g *Game) setRoad(c *Car, road, from int) {
	rd := g.roads[road]
	c.road, c.from = road, from
	if rd.A == from {
		c.dir, c.to = 1, rd.B
	} else {
		c.dir, c.to = -1, rd.A
	}
	c.mlane = c.lane
	if g.isMotor(road) {
		switch {
		case c.isEV():
			c.mlane = mwLanes[2]
		case c.kind == kLorry || c.kind == kBus:
			c.mlane = mwLanes[0]
		default:
			c.mlane = mwLanes[g.rng.Intn(3)]
		}
	}
	c.cleared, c.yielded, c.stopT, c.lineWait = false, false, 0, 0
}

func (g *Game) respawnCar(c *Car) {
	c.chase = false
	c.crashed, c.broken, c.ghost, c.wait, c.pull = 0, 0, 0, 0, 0
	for try := 0; try < 80; try++ {
		ri := g.rng.Intn(len(g.roads))
		if g.roadBlocked(ri) {
			continue
		}
		rd := g.roads[ri]
		from := rd.A
		if g.rng.Intn(2) == 0 {
			from = rd.B
		}
		g.setRoad(c, ri, from)
		L := g.carL(c)
		if L < 200 {
			continue
		}
		c.inTurn = false
		c.t = 20 + g.rng.Float64()*(L-160)
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

func (g *Game) otherEnd(road, node int) int {
	if g.roads[road].A == node {
		return g.roads[road].B
	}
	return g.roads[road].A
}

func (g *Game) pickNext(node, fromRoad int) int {
	var opts []int
	for _, ri := range g.nodes[node].Roads {
		if ri == fromRoad || g.roadBlocked(ri) {
			continue
		}
		opts = append(opts, ri)
	}
	if len(opts) == 0 {
		return fromRoad
	}
	for _, o := range opts {
		if g.isMotor(o) && g.rng.Float64() < 0.5 {
			return o
		}
	}
	// prefer going roughly straight on
	if in := g.apprOf(node, fromRoad); in != nil && g.rng.Float64() < 0.4 {
		best, bd := opts[0], -2.0
		for _, o := range opts {
			out := g.apprOf(node, o)
			d := in.Tx*out.OutTx + in.Ty*out.OutTy
			if d > bd {
				best, bd = o, d
			}
		}
		return best
	}
	return opts[g.rng.Intn(len(opts))]
}

func (g *Game) setNext(c *Car) {
	c.nroad = g.pickNext(c.to, c.road)
	c.nto = g.otherEnd(c.nroad, c.to)
	in := g.apprOf(c.to, c.road)
	out := g.apprOf(c.to, c.nroad)
	cr := in.Tx*out.OutTy - in.Ty*out.OutTx
	dot := in.Tx*out.OutTx + in.Ty*out.OutTy
	switch {
	case c.nroad == c.road || dot < -0.7:
		c.turnSign = 1 // U-turn goes right
	case len(g.nodes[c.to].Roads) < 3:
		c.turnSign = 0 // just a bend
	case cr > 0.45:
		c.turnSign = 1
	case cr < -0.45:
		c.turnSign = -1
	default:
		c.turnSign = 0
	}
}

func (g *Game) carLimit(c *Car) float64 {
	lim := g.roads[c.road].Limit
	if c.kind == kLorry || c.kind == kBus {
		lim = math.Min(lim, 56)
	}
	return lim / mphK * c.factor
}

func smooth01(v float64) float64 {
	v = math.Max(0, math.Min(1, v))
	return v * v * (3 - 2*v)
}

func (g *Game) laneTarget(c *Car) float64 {
	off := c.lane
	L := g.carL(c)
	if c.mlane != c.lane {
		k := smooth01((math.Min(c.t, L-c.t) - 150) / 700)
		off = c.lane + (c.mlane-c.lane)*k
	}
	if c.broken > 0 {
		off = mwRes + 3*mwLane + mwShoulder/2
	}
	if w := g.laneWorks(c.road); w != nil && c.dir == w.closedDir {
		d := math.Abs(g.sOf(c, c.t) - w.s)
		k := smooth01((zoneHalf + 60 - d) / 50)
		off = off + (-laneOff-off)*k
	}
	pullW := 24.0
	if g.isMotor(c.road) {
		pullW = mwLane
	}
	return off + pullW*c.pull
}

func (g *Game) roadPose(road, dir int, s, off float64) (x, y, hx, hy float64) {
	x, y, tx, ty := g.roads[road].at(s)
	if dir < 0 {
		tx, ty = -tx, -ty
	}
	lx, ly := ty, -tx
	return x + lx*off, y + ly*off, tx, ty
}

func (g *Game) startTurn(c *Car) {
	L := g.carL(c)
	p0x, p0y, ix, iy := g.roadPose(c.road, c.dir, g.sOf(c, L), c.lane)
	nd := 1
	if g.roads[c.nroad].A != c.to {
		nd = -1
	}
	ns := g.nodes[c.to].R
	if nd < 0 {
		ns = g.roads[c.nroad].Len - g.nodes[c.to].R
	}
	p2x, p2y, ox, oy := g.roadPose(c.nroad, nd, ns, c.lane)
	c.p0x, c.p0y, c.p2x, c.p2y = p0x, p0y, p2x, p2y
	den := ix*oy - iy*ox
	dot := ix*ox + iy*oy
	switch {
	case dot < -0.7:
		n := g.nodes[c.to]
		c.p1x, c.p1y = n.X+ix*30, n.Y+iy*30
	case math.Abs(den) < 0.15:
		c.p1x, c.p1y = (p0x+p2x)/2, (p0y+p2y)/2
	default:
		// intersection of the incoming and outgoing lane lines
		u := ((p2x-p0x)*oy - (p2y-p0y)*ox) / den
		c.p1x, c.p1y = p0x+ix*u, p0y+iy*u
		if u < 0 || u > 300 {
			c.p1x, c.p1y = (p0x+p2x)/2, (p0y+p2y)/2
		}
	}
	l := 0.0
	px, py := c.p0x, c.p0y
	for k := 1; k <= 12; k++ {
		x, y := bez(c, float64(k)/12)
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
		c.x, c.y, c.hx, c.hy = g.roadPose(c.road, c.dir, g.sOf(c, c.t), c.off)
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

// ---------- right of way ----------

func (g *Game) truckApproaching(node int, within float64) bool {
	n := g.nodes[node]
	d := math.Hypot(n.X-g.truck.X, n.Y-g.truck.Y)
	if d < n.R+30 {
		return true
	}
	if d > within || math.Abs(g.truck.Speed) < 20 {
		return false
	}
	hx, hy := math.Cos(g.truck.Angle), math.Sin(g.truck.Angle)
	if g.truck.Speed < 0 {
		hx, hy = -hx, -hy
	}
	return (hx*(n.X-g.truck.X)+hy*(n.Y-g.truck.Y))/d > 0.6
}

// junctionConflict: is there traffic that has priority over car c at its next junction?
func (g *Game) junctionConflict(c *Car, majorOnly bool) bool {
	node := c.to
	for _, o := range g.cars {
		if o == c || o.chase || o.crashed > 0 || o.broken > 0 {
			continue
		}
		if o.to != node {
			continue
		}
		if o.inTurn {
			if o.road == c.road && o.dir == c.dir {
				continue
			}
			return true
		}
		if o.road == c.road {
			continue
		}
		d := g.carL(o) - o.t
		if d > 260 || (o.speed < 8 && d > 40) {
			continue
		}
		if majorOnly {
			if ap := g.apprOf(node, o.road); ap != nil && !ap.Major {
				continue
			}
		}
		return true
	}
	return g.truckApproaching(node, 300)
}

// oncomingConflict: is there oncoming traffic going ahead / left that a right-turner must wait for?
func (g *Game) oncomingConflict(c *Car) bool {
	node := c.to
	in := g.apprOf(node, c.road)
	for _, o := range g.cars {
		if o == c || o.to != node || o.road == c.road || o.chase || o.crashed > 0 {
			continue
		}
		ap := g.apprOf(node, o.road)
		if ap == nil || ap.Tx*in.Tx+ap.Ty*in.Ty > -0.6 || o.turnSign > 0 {
			continue
		}
		if o.inTurn {
			return true
		}
		d := g.carL(o) - o.t
		if d < 240 && o.speed > 5 {
			return true
		}
	}
	if g.truckApproaching(node, 300) {
		hx, hy := math.Cos(g.truck.Angle), math.Sin(g.truck.Angle)
		if hx*in.Tx+hy*in.Ty < -0.6 {
			return true
		}
	}
	return false
}

func (g *Game) exitBlocked(c *Car) bool {
	for _, o := range g.cars {
		if o == c || o.inTurn || o.road != c.nroad || o.from != c.to {
			continue
		}
		if o.t < c.half+o.half+30 && o.speed < 15 {
			return true
		}
	}
	return false
}

// ---------- main traffic update ----------

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
		if c.honkCD > 0 {
			c.honkCD -= dt
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
			c.t += c.speed * dt
			g.carPose(c)
			continue
		}
		if !c.inTurn && g.roadBlocked(c.nroad) {
			g.setNext(c)
		}
		L := g.carL(c)
		motor := g.isMotor(c.road) && !c.inTurn
		node := g.nodes[c.to]
		ap := g.apprOf(c.to, c.road)
		desired := g.carLimit(c)
		turnSpeed := func() float64 {
			switch {
			case c.isEV():
				return 150
			case c.turnSign < 0:
				return 85
			case c.turnSign > 0:
				return 100
			}
			return 260
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
				if along > -10 && along < 300 && math.Abs(lat) < 100 {
					c.pulling = true
				}
			}
		}
		if c.pulling {
			c.pull = math.Min(1, c.pull+dt*1.2)
			if c.pull > 0.8 {
				desired = 0
			} else {
				desired = math.Min(desired, 90)
			}
		} else if c.pull > 0 {
			c.pull = math.Max(0, c.pull-dt*0.8)
			desired = math.Min(desired, 80)
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
					look = 260
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
		holding := false
		if !c.inTurn {
			hold := func(dist float64) {
				desired = math.Min(desired, follow(dist))
				if dist < 30 {
					holding = true
				}
			}
			// temporary lights at lane-closure roadworks
			if w := g.laneWorks(c.road); w != nil && !c.isEV() {
				dist := (w.s-g.sOf(c, c.t))*float64(c.dir) - (zoneHalf + 100) - c.half
				if dist > -2 {
					st := g.tempLight(w, c.dir)
					bd := c.speed * c.speed / (2 * 600)
					if st == 2 || (st == 1 && dist > bd*0.7) {
						hold(dist)
					}
				}
			}
			dist := L - 4 - c.half - c.t
			if dist > -2 && !c.isEV() && !c.cleared {
				// an emergency vehicle about to use the junction: wait
				for _, e := range evs {
					if math.Hypot(e.x-node.X, e.y-node.Y) < 260 {
						hold(dist)
					}
				}
				switch node.Ctrl {
				case ctrlLights:
					st := g.light(c.to, ap.Axis)
					if st != 0 {
						bd := c.speed * c.speed / (2 * 600)
						if !(st == 1 && dist < bd*0.7) {
							hold(dist)
						}
					}
				case ctrlPriority:
					if !ap.Major {
						// give way / stop: slow right down on approach and look both ways
						desired = math.Min(desired, math.Sqrt(55*55+2*400*math.Max(0, dist)))
						if dist < 90 {
							if ap.Stop && c.stopT < 0.8 {
								if dist < 14 && c.speed < 6 {
									c.stopT += dt
								}
								hold(dist)
							} else if g.junctionConflict(c, true) {
								hold(dist)
							} else if dist < 30 {
								c.cleared = true
								c.yielded = true
							}
						}
					}
				}
				if !c.cleared && g.exitBlocked(c) {
					hold(dist)
				}
			}
			if c.turnSign != 0 || len(node.Roads) < 3 {
				ts := turnSpeed()
				desired = math.Min(desired, math.Sqrt(ts*ts+2*350*math.Max(0, L-c.t)))
			}
		} else if c.turnSign > 0 && !c.yielded && !c.isEV() && len(node.Roads) >= 3 {
			// turning right across oncoming traffic: edge out and wait for a gap
			waitAt := c.turnLen * 0.32
			if g.oncomingConflict(c) {
				desired = math.Min(desired, follow(waitAt-c.t))
				holding = true
			} else {
				c.yielded = true
			}
		}
		prev := c.speed
		c.braking = desired < c.speed-2 || c.speed < 2
		if c.speed > desired {
			c.speed = math.Max(desired, c.speed-1000*dt)
		} else {
			c.speed = math.Min(desired, c.speed+150*dt)
		}
		if motor && prev-c.speed > 700*dt && prev > 250 {
			c.hazard = 3
		}
		// deadlock breakers
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
		if holding && c.speed < 3 {
			c.lineWait += dt
			if c.lineWait > 14 {
				c.cleared, c.yielded = true, true
				c.lineWait = 0
			}
		} else if c.speed > 20 {
			c.lineWait = 0
		}
		// honk at a truck that sits in the way on a green light
		if truckBlock && c.speed < 2 && !holding {
			c.truckWait += dt
			if c.truckWait > 4 && c.honkCD <= 0 {
				c.honkCD = 6
				g.honk(c.x, c.y)
			}
		} else {
			c.truckWait = 0
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
			g.setRoad(c, c.nroad, c.to)
			c.inTurn = false
			c.off = c.lane
			g.setNext(c)
		}
		g.carPose(c)
	}
	g.cars = keep

	// keep the M11 busy
	g.motorCheck -= dt
	if g.motorCheck <= 0 {
		g.motorCheck = 1.5
		n := 0
		for _, c := range g.cars {
			if g.isMotor(c.road) {
				n++
			}
		}
		if n < 18*len(g.motorways) {
			mi := g.motorways[g.rng.Intn(len(g.motorways))]
			for _, c := range g.cars {
				if c.isEV() || c.chase || c.kind == kPolice || g.isMotor(c.road) || g.visible(c.x, c.y, 300) {
					continue
				}
				from := g.roads[mi].A
				if g.rng.Intn(2) == 0 {
					from = g.roads[mi].B
				}
				g.setRoad(c, mi, from)
				c.inTurn = false
				c.crashed, c.broken, c.pull = 0, 0, 0
				L := g.carL(c)
				for try := 0; try < 20; try++ {
					c.t = 1000 + g.rng.Float64()*(L-2000)
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
		ri := g.rng.Intn(len(g.roads))
		if g.roadBlocked(ri) {
			continue
		}
		from := g.roads[ri].A
		if g.rng.Intn(2) == 0 {
			from = g.roads[ri].B
		}
		g.setRoad(c, ri, from)
		if g.carL(c) < 200 {
			continue
		}
		c.t = 10
		c.inTurn = false
		c.off = c.lane
		g.carPose(c)
		d := math.Hypot(c.x-g.truck.X, c.y-g.truck.Y)
		if (d > 750 && d < 1600) || try > 70 {
			break
		}
	}
	g.setNext(c)
	c.speed = 150
	g.carPose(c)
	g.cars = append(g.cars, c)
}

// ---------- police chase ----------

func (g *Game) startChase(reason string, amount int) {
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
	g.chaseReason, g.chaseFine = reason, amount
	g.chaseTime = 0
	g.stopTime = 0
	g.flash("POLICE! Pull over and stop - " + reason)
}

func (g *Game) updateChaser(c *Car) {
	td := math.Hypot(g.truck.X-c.x, g.truck.Y-c.y)
	target := math.Min(620, math.Abs(g.truck.Speed)+150)
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

func drawRot(dst, img *ebiten.Image, x, y, a float64) {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(-float64(w)/2, -float64(h)/2)
	op.GeoM.Rotate(a)
	op.GeoM.Translate(x, y)
	dst.DrawImage(img, op)
}

// drawLamps: ind 0 none, -1 left, 1 right, 2 hazards.
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
		fillCircle(screen, x1, y1, 9, color.NRGBA{255, 170, 0, 80})
		fillCircle(screen, x2, y2, 9, color.NRGBA{255, 170, 0, 80})
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
	case c.turnSign != 0 && !c.chase && (c.inTurn || c.t > g.carL(c)-200):
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
	ax, ay := in.tx, in.ty
	bx, by := ay, -ax // left of a->b
	P := func(a, b float64) (float64, float64) {
		return in.mx + ax*a + bx*b - cx, in.my + ay*a + by*b - cy
	}
	cone := func(a, b float64) {
		x, y := P(a, b)
		fillCircle(screen, x, y, 6, rgb(255, 110, 0))
		fillCircle(screen, x, y, 2.5, color.White)
	}
	quad := func(a0, b0, a1, b1 float64, c color.Color) {
		x0, y0 := P(a0, b0)
		x1, y1 := P(a1, b0)
		x2, y2 := P(a1, b1)
		x3, y3 := P(a0, b1)
		fillQuad(screen, [4][2]float64{{x0, y0}, {x1, y1}, {x2, y2}, {x3, y3}}, c)
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
		x1, y1 := P(-18, 12)
		x2, y2 := P(16, -14)
		drawRot(screen, makeCarCached(carColors[r.Intn(len(carColors))], 0), x1, y1, ang+0.5)
		drawRot(screen, makeCarCached(carColors[r.Intn(len(carColors))], 0), x2, y2, ang-1.1)
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
		closed := in.kind == incWorksLight
		side := float64(in.closedDir)
		if closed {
			quad(-zoneHalf+4, side*4, zoneHalf-4, side*(cityHalf-2), rgb(110, 85, 60))
			quad(-zoneHalf+10, side*10, zoneHalf-10, side*(cityHalf-8), rgb(90, 68, 48))
		} else {
			quad(-66, -cityHalf+6, 66, cityHalf-6, rgb(110, 85, 60))
			quad(-58, -cityHalf+14, 58, cityHalf-14, rgb(90, 68, 48))
		}
		bw := 30.0 * side
		if !closed {
			bw = 0
		}
		dx, dy := P(-20, bw+10)
		fillRect(screen, dx-14, dy-10, 28, 20, rgb(240, 190, 20))
		fillRect(screen, dx-4, dy-8, 10, 16, rgb(60, 60, 60))
		ex, ey := P(14, bw)
		strokeLine(screen, dx, dy, ex, ey, 5, rgb(220, 170, 20))
		fillRect(screen, ex-5, ey-5, 10, 10, rgb(80, 80, 80))
		for k := 0; k < 2; k++ {
			wx, wy := P(r.Float64()*60-30, bw+r.Float64()*20-10)
			fillCircle(screen, wx, wy, 7, rgb(255, 120, 0))
			fillCircle(screen, wx, wy, 4, color.White)
		}
		if closed {
			for a := -zoneHalf - 30; a <= zoneHalf+30; a += 16 {
				cone(a, side*2)
			}
			// temporary lights: one for each direction, on its own left
			for _, d := range []int{1, -1} {
				fd := float64(d)
				lx, ly := P(-fd*(zoneHalf+100), fd*78)
				drawTempLight(screen, lx, ly, g.tempLight(in, d))
				s0x, s0y := P(-fd*(zoneHalf+100), fd*4)
				s1x, s1y := P(-fd*(zoneHalf+100), fd*(cityHalf-4))
				strokeLine(screen, s0x, s0y, s1x, s1y, 5, color.White)
			}
			tx, ty := P(0, -side*80)
			drawTextShadow(screen, "ROAD WORKS - TEMPORARY LIGHTS", faceTiny, tx, ty, rgb(255, 210, 60), text.AlignCenter)
		} else {
			for _, end := range []float64{-70, 70} {
				for b := -62.0; b < 62; b += 12 {
					bx1, by1 := P(end, b)
					col := rgb(220, 30, 30)
					if int((b+62)/12)%2 == 0 {
						col = rgb(240, 240, 240)
					}
					fillRect(screen, bx1-4, by1-4, 8, 8, col)
				}
			}
			lx, ly := P(0, -80)
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

func lightColor(st int) color.RGBA {
	switch st {
	case 0:
		return rgb(40, 230, 90)
	case 1:
		return rgb(255, 180, 0)
	}
	return rgb(240, 40, 40)
}

// drawLights draws a 3-lamp signal head beside every stop line, plus a glowing bar on
// the stop line in the colour of THAT approach's light, so it's obvious which light is yours.
func (g *Game) drawLights(screen *ebiten.Image, cx, cy float64) {
	for id, n := range g.nodes {
		if n.Ctrl != ctrlLights || !g.visible(n.X, n.Y, 200) {
			continue
		}
		for _, a := range n.Appr {
			st := g.light(id, a.Axis)
			col := lightColor(st)
			lx, ly := a.Ty, -a.Tx
			p0x, p0y := a.SX+lx*4-cx, a.SY+ly*4-cy
			p1x, p1y := a.SX+lx*(cityHalf-4)-cx, a.SY+ly*(cityHalf-4)-cy
			strokeLine(screen, p0x, p0y, p1x, p1y, 7, color.NRGBA{col.R, col.G, col.B, 200})
			// signal head facing the approaching driver
			hx, hy := a.SignX-cx, a.SignY-cy
			ang := math.Atan2(a.Ty, a.Tx)
			c, s := math.Cos(ang), math.Sin(ang)
			pt := func(u, v float64) (float64, float64) { return hx + c*u - s*v, hy + s*u + c*v }
			x0, y0 := pt(-9, -22)
			x1, y1 := pt(9, -22)
			x2, y2 := pt(9, 22)
			x3, y3 := pt(-9, 22)
			fillQuad(screen, [4][2]float64{{x0, y0}, {x1, y1}, {x2, y2}, {x3, y3}}, rgb(20, 20, 22))
			for k, lamp := range []int{2, 1, 0} {
				lx, ly := pt(0, float64(k-1)*14)
				on := st == lamp || (st == 3 && lamp != 0)
				lc := lightColor(lamp)
				if on {
					fillCircle(screen, lx, ly, 6, lc)
					fillCircle(screen, lx, ly, 11, color.NRGBA{lc.R, lc.G, lc.B, 60})
				} else {
					fillCircle(screen, lx, ly, 5, color.RGBA{lc.R / 5, lc.G / 5, lc.B / 5, 255})
				}
			}
		}
	}
}

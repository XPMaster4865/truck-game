package main

import (
	"container/heap"
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

// ---------- GPS / sat-nav ----------

type pqItem struct {
	node int
	cost float64
}
type pq []pqItem

func (p pq) Len() int            { return len(p) }
func (p pq) Less(i, j int) bool  { return p[i].cost < p[j].cost }
func (p pq) Swap(i, j int)       { p[i], p[j] = p[j], p[i] }
func (p *pq) Push(x interface{}) { *p = append(*p, x.(pqItem)) }
func (p *pq) Pop() interface{} {
	o := *p
	it := o[len(o)-1]
	*p = o[:len(o)-1]
	return it
}

type routeStep struct {
	node int // junction reached at the end of this step
	road int // road taken to get there
}

// gpsTarget is where the sat-nav is taking you: a manual pin, else your current job.
func (g *Game) gpsTarget() (float64, float64, string, bool) {
	if g.gpsOn {
		return g.gpsX, g.gpsY, "Pinned location", true
	}
	if p := g.target(); p != nil {
		x, y := p.bayCenter()
		return x, y, p.Name, true
	}
	return 0, 0, "", false
}

func (g *Game) updateGPS() {
	g.routeTimer -= dt
	if g.routeTimer > 0 && !g.routeDirty {
		return
	}
	g.routeTimer = 0.8
	g.routeDirty = false
	g.route, g.routeSteps = nil, nil
	tx, ty, _, ok := g.gpsTarget()
	if !ok {
		return
	}
	if g.gpsOn && math.Hypot(tx-g.truck.X, ty-g.truck.Y) < 150 {
		g.gpsOn = false
		g.flash("You have reached your destination.")
		return
	}
	sh := g.roadQuery(g.truck.X, g.truck.Y, 700)
	eh := g.roadQuery(tx, ty, 900)
	if sh.Road < 0 || eh.Road < 0 {
		return
	}
	sr, er := g.roads[sh.Road], g.roads[eh.Road]
	// preferred direction on the start road = the way the truck is facing
	hx, hy := math.Cos(g.truck.Angle), math.Sin(g.truck.Angle)
	_, _, rtx, rty := sr.at(sh.S)
	fwd := hx*rtx+hy*rty >= 0

	dist := make([]float64, len(g.nodes))
	prevNode := make([]int, len(g.nodes))
	prevRoad := make([]int, len(g.nodes))
	for i := range dist {
		dist[i] = math.Inf(1)
		prevNode[i], prevRoad[i] = -1, -1
	}
	h := &pq{}
	// leaving along the start road: going backwards costs a U-turn penalty
	costA, costB := sh.S, sr.Len-sh.S
	if fwd {
		costA += 400
	} else {
		costB += 400
	}
	dist[sr.A], dist[sr.B] = costA, costB
	prevRoad[sr.A], prevRoad[sr.B] = sh.Road, sh.Road
	heap.Push(h, pqItem{sr.A, costA})
	heap.Push(h, pqItem{sr.B, costB})
	for h.Len() > 0 {
		it := heap.Pop(h).(pqItem)
		if it.cost > dist[it.node] {
			continue
		}
		for _, ri := range g.nodes[it.node].Roads {
			if g.roadBlocked(ri) || ri == prevRoad[it.node] && prevNode[it.node] >= 0 {
				continue
			}
			rd := g.roads[ri]
			o := g.otherEnd(ri, it.node)
			c := it.cost + rd.Len
			if rd.Class == clMotorway {
				c -= rd.Len * 0.35 // motorways are quicker
			}
			if c < dist[o] {
				dist[o] = c
				prevNode[o], prevRoad[o] = it.node, ri
				heap.Push(h, pqItem{o, c})
			}
		}
	}
	// best end node
	endA, endB := dist[er.A]+eh.S, dist[er.B]+er.Len-eh.S
	last := er.A
	if endB < endA {
		last = er.B
	}
	if math.IsInf(dist[last], 1) {
		return
	}
	// walk back
	var steps []routeStep
	for n := last; n >= 0; n = prevNode[n] {
		steps = append([]routeStep{{node: n, road: prevRoad[n]}}, steps...)
		if prevNode[n] < 0 {
			break
		}
	}
	g.routeSteps = steps
	// build the polyline
	pts := [][2]float64{{g.truck.X, g.truck.Y}}
	addRoad := func(ri int, s0, s1 float64) {
		rd := g.roads[ri]
		step := 40.0
		if s1 >= s0 {
			for s := s0; s < s1; s += step {
				x, y, _, _ := rd.at(s)
				pts = append(pts, [2]float64{x, y})
			}
		} else {
			for s := s0; s > s1; s -= step {
				x, y, _, _ := rd.at(s)
				pts = append(pts, [2]float64{x, y})
			}
		}
		x, y, _, _ := rd.at(s1)
		pts = append(pts, [2]float64{x, y})
	}
	if sh.Road == eh.Road && len(steps) == 1 && steps[0].road == sh.Road && prevNode[steps[0].node] < 0 &&
		((fwd && eh.S >= sh.S) || (!fwd && eh.S <= sh.S)) {
		addRoad(sh.Road, sh.S, eh.S)
	} else {
		first := steps[0].node
		if first == sr.A {
			addRoad(sh.Road, sh.S, 0)
		} else {
			addRoad(sh.Road, sh.S, sr.Len)
		}
		for i := 1; i < len(steps); i++ {
			ri := steps[i].road
			if g.roads[ri].A == steps[i-1].node {
				addRoad(ri, 0, g.roads[ri].Len)
			} else {
				addRoad(ri, g.roads[ri].Len, 0)
			}
		}
		if last == er.A {
			addRoad(eh.Road, 0, eh.S)
		} else {
			addRoad(eh.Road, er.Len, eh.S)
		}
	}
	pts = append(pts, [2]float64{tx, ty})
	g.route = pts
	g.routeLen = 0
	for i := 1; i < len(pts); i++ {
		g.routeLen += math.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1])
	}
	g.computeInstruction()
}

// computeInstruction works out the next turn ("Turn left onto High Street in 200 m").
func (g *Game) computeInstruction() {
	g.gpsText = ""
	if len(g.routeSteps) == 0 {
		return
	}
	_, _, name, _ := g.gpsTarget()
	for i := 0; i < len(g.routeSteps); i++ {
		n := g.routeSteps[i].node
		nd := g.nodes[n]
		d := math.Hypot(nd.X-g.truck.X, nd.Y-g.truck.Y)
		var inRoad int
		if i == 0 {
			h := g.roadQuery(g.truck.X, g.truck.Y, 700)
			inRoad = h.Road
		} else {
			inRoad = g.routeSteps[i].road
		}
		if i+1 >= len(g.routeSteps) {
			g.gpsText = fmt.Sprintf("Continue to %s  (%s)", name, fmtDist(g.routeLen))
			return
		}
		outRoad := g.routeSteps[i+1].road
		if len(nd.Roads) < 3 || inRoad < 0 {
			continue
		}
		in, out := g.apprOf(n, inRoad), g.apprOf(n, outRoad)
		if in == nil || out == nil {
			continue
		}
		cr := in.Tx*out.OutTy - in.Ty*out.OutTx
		dot := in.Tx*out.OutTx + in.Ty*out.OutTy
		verb := "Go straight on"
		switch {
		case dot < -0.7:
			verb = "Make a U-turn"
		case cr > 0.45:
			verb = "Turn right"
		case cr < -0.45:
			verb = "Turn left"
		}
		g.gpsTurn = 0
		if verb == "Turn left" {
			g.gpsTurn = -1
		} else if verb == "Turn right" || verb == "Make a U-turn" {
			g.gpsTurn = 1
		}
		g.gpsText = fmt.Sprintf("%s onto %s in %s", verb, g.roads[outRoad].Name, fmtDist(d))
		return
	}
}

func fmtDist(px float64) string {
	m := px / 10
	if m > 1600 {
		return fmt.Sprintf("%.1f miles", px/pxPerMile)
	}
	return fmt.Sprintf("%d m", int(m/10)*10)
}

func (g *Game) drawRouteWorld(screen *ebiten.Image, cx, cy float64) {
	if len(g.route) < 2 {
		return
	}
	col := color.NRGBA{60, 140, 255, 150}
	for i := 1; i < len(g.route); i++ {
		a, b := g.route[i-1], g.route[i]
		if !g.visible(a[0], a[1], 200) && !g.visible(b[0], b[1], 200) {
			continue
		}
		strokeLine(screen, a[0]-cx, a[1]-cy, b[0]-cx, b[1]-cy, 14, col)
	}
	if x, y, _, ok := g.gpsTarget(); ok && g.gpsOn && g.visible(x, y, 50) {
		g.drawPin(screen, x-cx, y-cy, 1.5)
	}
}

func (g *Game) drawRouteMap(dst *ebiten.Image, ox, oy, s float64) {
	if len(g.route) >= 2 {
		for i := 1; i < len(g.route); i++ {
			a, b := g.route[i-1], g.route[i]
			strokeLine(dst, ox+a[0]*s, oy+a[1]*s, ox+b[0]*s, oy+b[1]*s, 3.5, color.NRGBA{60, 160, 255, 230})
		}
	}
	if g.gpsOn {
		g.drawPin(dst, ox+g.gpsX*s, oy+g.gpsY*s, 0.8)
	}
}

func (g *Game) drawPin(dst *ebiten.Image, x, y, k float64) {
	fillTri(dst, x-7*k, y-14*k, x+7*k, y-14*k, x, y, rgb(230, 40, 60))
	fillCircle(dst, x, y-16*k, 8*k, rgb(230, 40, 60))
	fillCircle(dst, x, y-16*k, 3.5*k, color.White)
}

// drawTurnArrow draws a big sat-nav style arrow for the next manoeuvre.
func drawTurnArrow(dst *ebiten.Image, x, y float64, turn int, c color.RGBA) {
	switch turn {
	case -1:
		fillRect(dst, x-2, y-4, 8, 22, c)
		fillRect(dst, x-14, y-4, 20, 8, c)
		fillTri(dst, x-26, y, x-12, y-12, x-12, y+12, c)
	case 1:
		fillRect(dst, x-6, y-4, 8, 22, c)
		fillRect(dst, x-6, y-4, 20, 8, c)
		fillTri(dst, x+26, y, x+12, y-12, x+12, y+12, c)
	default:
		fillRect(dst, x-4, y-6, 8, 24, c)
		fillTri(dst, x, y-20, x-12, y-6, x+12, y-6, c)
	}
}

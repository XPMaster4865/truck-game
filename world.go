package main

import (
	"fmt"
	"image/color"
	"math"
	"math/rand"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

const (
	screenW = 1280
	screenH = 720
	tile    = 64

	// Manchester (top), M1 down to London, M11 east to Cambridge
	manS             = 768.0
	manCols, manRows = 6, 4
	manOX, manOY     = 384 + 3*896 - 2*manS, 384.0
	m1TopY           = manOY + 3*manS + 700
	m1Len            = 12000.0
	m1BotY           = m1TopY + m1Len
	m1X              = 384 + 3*896
	lonCols, lonRows = 8, 6
	lonS             = 896.0
	lonOX            = 384.0
	lonOY            = m1BotY + 700
	mwY              = lonOY + 2*lonS
	mw0X             = 7400.0
	mw1X             = 23300.0
	camOX            = 23950.0
	camS             = 768.0
	camCols          = 6
	camRows          = 4
	camOY            = mwY - camS
	worldW           = 28672
	worldH           = 20992
	mapW             = worldW / tile
	mapH             = worldH / tile
	cityHalf         = 64.0 // half road width in towns (2 lanes of 64px)

	// motorway cross-section (per carriageway, measured from the centre line)
	mwRes      = 20.0 // half central reservation
	mwLane     = 64.0 // lane width
	mwShoulder = 56.0 // hard shoulder width
	mwHalf     = mwRes + 3*mwLane + mwShoulder
	mwTaper    = 600.0 // length over which the motorway widens from a normal road

	pavement  = 28.0
	pxPerMile = 1600.0
	dt        = 1.0 / 60.0
	chunkSz   = 1024
	mphK      = 0.13 // px/s -> mph
	laneOff   = 32.0
)

// motorway lane centres (offset to the left of travel): lane 1 = slow lane by the hard shoulder
var mwLanes = []float64{mwRes + 2.5*mwLane, mwRes + 1.5*mwLane, mwRes + 0.5*mwLane}

type TileKind uint8

const (
	TRoad TileKind = iota
	TSidewalk
	TBuilding
	TGrass
	TTree
	TWater
	TYard
	TYardBuilding
	TLot
	TField
	THedge
)

func (k TileKind) solid() bool {
	return k == TBuilding || k == TTree || k == TWater || k == TYardBuilding || k == THedge
}

// ---------- roads ----------

const (
	clMinor = iota
	clMain
	clMotorway
	clLink
)

type Road struct {
	A, B   int
	P      [][2]float64
	Cum    []float64
	Len    float64
	Class  int
	Limit  float64
	Name   string
	City   int // -1 countryside
	x0, y0 float64
	x1, y1 float64
}

func (r *Road) seg(s float64) int {
	if s <= 0 {
		return 0
	}
	if s >= r.Len {
		return len(r.P) - 2
	}
	lo, hi := 0, len(r.Cum)-1
	for hi-lo > 1 {
		m := (lo + hi) / 2
		if r.Cum[m] <= s {
			lo = m
		} else {
			hi = m
		}
	}
	return lo
}

// at returns the centre-line point and unit tangent (a->b) at arc length s.
func (r *Road) at(s float64) (x, y, tx, ty float64) {
	s = math.Max(0, math.Min(r.Len, s))
	k := r.seg(s)
	p, q := r.P[k], r.P[k+1]
	l := r.Cum[k+1] - r.Cum[k]
	u := 0.0
	if l > 0 {
		u = (s - r.Cum[k]) / l
	}
	tx, ty = (q[0]-p[0])/l, (q[1]-p[1])/l
	return p[0] + (q[0]-p[0])*u, p[1] + (q[1]-p[1])*u, tx, ty
}

// halfAt is half the paved width at s (motorways taper at each end).
func (r *Road) halfAt(s float64) float64 {
	if r.Class != clMotorway {
		return cityHalf
	}
	k := smooth01(math.Min(s, r.Len-s) / mwTaper)
	return cityHalf + (mwHalf-cityHalf)*k
}

func (r *Road) build() {
	r.Cum = make([]float64, len(r.P))
	for i := 1; i < len(r.P); i++ {
		r.Cum[i] = r.Cum[i-1] + math.Hypot(r.P[i][0]-r.P[i-1][0], r.P[i][1]-r.P[i-1][1])
	}
	r.Len = r.Cum[len(r.Cum)-1]
	r.x0, r.y0, r.x1, r.y1 = 1e9, 1e9, -1e9, -1e9
	for _, p := range r.P {
		r.x0, r.y0 = math.Min(r.x0, p[0]), math.Min(r.y0, p[1])
		r.x1, r.y1 = math.Max(r.x1, p[0]), math.Max(r.y1, p[1])
	}
}

const (
	ctrlNone = iota
	ctrlLights
	ctrlPriority
)

type Approach struct {
	Road   int
	Tx, Ty float64 // direction of travel into the junction
	SX, SY float64 // stop line centre (on road centre line)
	Axis   int
	Major  bool
	Stop   bool
	SignX  float64
	SignY  float64
	LaneX  float64 // centre of the approach lane at the stop line
	LaneY  float64
	OutTx  float64 // direction leaving the junction along this road
	OutTy  float64
}

type Node struct {
	X, Y     float64
	Roads    []int
	City     int
	Ctrl     int
	R        float64
	LightOff float64
	Appr     []Approach
}

func (g *Game) apprOf(node, road int) *Approach {
	for i := range g.nodes[node].Appr {
		if g.nodes[node].Appr[i].Road == road {
			return &g.nodes[node].Appr[i]
		}
	}
	return nil
}

// ---------- places ----------

type Place struct {
	Name      string
	Cargo     string
	DestNames []string
	Dests     []int
	Color     color.RGBA
	Floor     color.RGBA
	Dealer    bool
	Fuel      float64
	Charge    float64
	City      int     // 0 London, 1 Cambridge, 2 Manchester, -1 motorway services
	MW        int     // motorway services: 1 = M11, 2 = M1
	Side      float64 // +1 = left of the motorway's a->b direction
	Frac      float64 // how far along the motorway
	ServS     float64
	TX, TY    int
	Face      int // side the road is on: 0 north, 1 east, 2 south, 3 west
}

func (p *Place) rect() (x, y, w, h float64) {
	return float64(p.TX * tile), float64(p.TY * tile), 6 * tile, 6 * tile
}

// building returns the yard building rectangle (on the side away from the road).
func (p *Place) building() (x, y, w, h float64) {
	ox, oy, _, _ := p.rect()
	switch p.Face {
	case 0:
		return ox + tile + 4, oy + 4*tile + 4, 4*tile - 8, 2*tile - 8
	case 1:
		return ox + 4, oy + tile + 4, 2*tile - 8, 4*tile - 8
	case 3:
		return ox + 4*tile + 4, oy + tile + 4, 2*tile - 8, 4*tile - 8
	}
	return ox + tile + 4, oy + 4, 4*tile - 8, 2*tile - 8
}

func (p *Place) bay() (x, y, w, h float64) {
	ox, oy, _, _ := p.rect()
	switch p.Face {
	case 0:
		return ox + 1.5*tile, oy + 1.8*tile, 3 * tile, 2 * tile
	case 1:
		return ox + 2.2*tile, oy + 1.5*tile, 2 * tile, 3 * tile
	case 3:
		return ox + 1.8*tile, oy + 1.5*tile, 2 * tile, 3 * tile
	}
	return ox + 1.5*tile, oy + 2.2*tile, 3 * tile, 2 * tile
}

func (p *Place) bayCenter() (float64, float64) {
	x, y, w, h := p.bay()
	return x + w/2, y + h/2
}

func rgb(r, g, b uint8) color.RGBA { return color.RGBA{r, g, b, 255} }

var shops = []string{"Teaco Superstore", "Sainsbelly's", "Azda", "Liddle"}

var places = []*Place{
	// ---- London ----
	{Name: "Hillside Sawmill", Cargo: "Timber", Color: rgb(190, 130, 70), Floor: rgb(150, 125, 95),
		DestNames: []string{"B&Queue DIY", "Building Site", "Screwfixx"}},
	{Name: "Meadow Farm", Cargo: "Vegetables", Color: rgb(120, 200, 80), Floor: rgb(140, 115, 75),
		DestNames: append([]string{"McDougal's", "Teaco Express", "Teaco Metro"}, shops...)},
	{Name: "Rocky Top Quarry", Cargo: "Gravel", Color: rgb(170, 170, 170), Floor: rgb(135, 128, 118),
		DestNames: []string{"Building Site", "B&Queue DIY"}},
	{Name: "Brewdawg Brewery", Cargo: "Beer Kegs", Color: rgb(230, 160, 40), Floor: rgb(120, 115, 110),
		DestNames: append([]string{"The Red Lion Pub", "The Eagle Pub"}, shops...)},
	{Name: "Amazin' Warehouse", Cargo: "Parcels", Color: rgb(215, 175, 120), Floor: rgb(125, 125, 130),
		DestNames: []string{"Argoz", "Royal Male Depot", "Teaco Superstore", "Screwfixx", "Grand Arcayde", "Arndayle Centre"}},
	{Name: "Port of Bristow", Cargo: "Containers", Color: rgb(60, 190, 200), Floor: rgb(110, 115, 125),
		DestNames: []string{"Amazin' Warehouse", "Argoz", "Steelworks", "Cambridge Science Park", "Trafferd Park Depot"}},
	{Name: "Steelworks", Cargo: "Machinery", Color: rgb(90, 140, 220), Floor: rgb(115, 118, 125),
		DestNames: []string{"Building Site", "Port of Bristow", "Screwfixx", "Media Citty"}},
	{Name: "Greggz Bakery", Cargo: "Sausage Rolls", Color: rgb(40, 90, 170), Floor: rgb(128, 128, 132),
		DestNames: []string{"Coasta Coffee", "Teaco Superstore", "Liddle", "Azda", "Addenbrookies Hospital", "Old Trafferd Stadium"}},
	{Name: "Royal Male Depot", Cargo: "Post", Color: rgb(220, 40, 40), Floor: rgb(125, 125, 130),
		DestNames: []string{"Amazin' Warehouse", "Argoz", "Sainsbelly's", "King's Collage"}},
	{Name: "Teaco Superstore", Color: rgb(0, 80, 160), Floor: rgb(128, 128, 132)},
	{Name: "Sainsbelly's", Color: rgb(240, 120, 0), Floor: rgb(128, 128, 132)},
	{Name: "Azda", Color: rgb(100, 180, 50), Floor: rgb(128, 128, 132)},
	{Name: "Liddle", Color: rgb(250, 210, 0), Floor: rgb(128, 128, 132)},
	{Name: "B&Queue DIY", Color: rgb(245, 130, 30), Floor: rgb(128, 128, 132)},
	{Name: "Building Site", Color: rgb(250, 200, 40), Floor: rgb(150, 140, 120)},
	{Name: "McDougal's", Color: rgb(220, 30, 30), Floor: rgb(128, 128, 132)},
	{Name: "Coasta Coffee", Color: rgb(130, 20, 50), Floor: rgb(128, 128, 132)},
	{Name: "Argoz", Color: rgb(200, 20, 40), Floor: rgb(128, 128, 132)},
	{Name: "Screwfixx", Color: rgb(20, 60, 140), Floor: rgb(128, 128, 132)},
	{Name: "The Red Lion Pub", Color: rgb(140, 30, 30), Floor: rgb(120, 110, 100)},
	{Name: "PB Fuel", Color: rgb(0, 150, 70), Floor: rgb(120, 122, 128), Fuel: 1.39},
	{Name: "Crab Petrol", Color: rgb(250, 200, 0), Floor: rgb(120, 122, 128), Fuel: 1.49},
	{Name: "Essco", Color: rgb(220, 20, 40), Floor: rgb(120, 122, 128), Fuel: 1.45},
	{Name: "Texacoo", Color: rgb(200, 20, 20), Floor: rgb(120, 122, 128), Fuel: 1.42},
	{Name: "GridSwerve Charging Hub", Color: rgb(0, 200, 180), Floor: rgb(120, 125, 130), Charge: 0.55},
	{Name: "Big Al's Truck Sales", Color: rgb(255, 255, 255), Floor: rgb(120, 125, 135), Dealer: true},
	// ---- Cambridge ----
	{Name: "Fitzbilly's Bakery", City: 1, Cargo: "Chelsea Buns", Color: rgb(120, 60, 140), Floor: rgb(128, 128, 132),
		DestNames: []string{"King's Collage", "Grand Arcayde", "Addenbrookies Hospital", "Coasta Coffee", "The Eagle Pub"}},
	{Name: "Cambridge Science Park", City: 1, Cargo: "Lab Equipment", Color: rgb(0, 170, 200), Floor: rgb(118, 122, 130),
		DestNames: []string{"Addenbrookies Hospital", "King's Collage", "Steelworks", "Port of Bristow"}},
	{Name: "Grantchester Farm", City: 1, Cargo: "Strawberries", Color: rgb(230, 50, 70), Floor: rgb(140, 115, 75),
		DestNames: []string{"Teaco Express", "Sainsbelly's", "Fitzbilly's Bakery", "Teaco Superstore", "Liddle"}},
	{Name: "Addenbrookies Hospital", City: 1, Color: rgb(0, 94, 184), Floor: rgb(128, 128, 132)},
	{Name: "King's Collage", City: 1, Color: rgb(170, 140, 90), Floor: rgb(140, 135, 120)},
	{Name: "Grand Arcayde", City: 1, Color: rgb(80, 80, 90), Floor: rgb(128, 128, 132)},
	{Name: "Teaco Express", City: 1, Color: rgb(0, 80, 160), Floor: rgb(128, 128, 132)},
	{Name: "The Eagle Pub", City: 1, Color: rgb(60, 40, 30), Floor: rgb(120, 110, 100)},
	{Name: "PB Cambridge", City: 1, Color: rgb(0, 150, 70), Floor: rgb(120, 122, 128), Fuel: 1.41},
	{Name: "Ionnity Chargers", City: 1, Color: rgb(90, 40, 160), Floor: rgb(120, 125, 130), Charge: 0.62},
	// ---- M11 services ----
	// ---- Manchester ----
	{Name: "Trafferd Park Depot", City: 2, Cargo: "Containers", Color: rgb(70, 110, 160), Floor: rgb(115, 118, 125),
		DestNames: []string{"Arndayle Centre", "Port of Bristow", "Amazin' Warehouse", "Media Citty"}},
	{Name: "Kelloggz Factory", City: 2, Cargo: "Cereal", Color: rgb(220, 30, 50), Floor: rgb(128, 128, 132),
		DestNames: []string{"Teaco Metro", "Teaco Superstore", "Sainsbelly's", "Teaco Express", "Arndayle Centre"}},
	{Name: "Boddingtunz Brewery", City: 2, Cargo: "Ale Barrels", Color: rgb(230, 170, 30), Floor: rgb(120, 115, 110),
		DestNames: []string{"The Red Lion Pub", "The Eagle Pub", "Old Trafferd Stadium", "Teaco Metro"}},
	{Name: "Arndayle Centre", City: 2, Color: rgb(150, 60, 120), Floor: rgb(128, 128, 132)},
	{Name: "Old Trafferd Stadium", City: 2, Color: rgb(200, 20, 30), Floor: rgb(120, 125, 120)},
	{Name: "Media Citty", City: 2, Color: rgb(60, 60, 70), Floor: rgb(128, 128, 132)},
	{Name: "Teaco Metro", City: 2, Color: rgb(0, 80, 160), Floor: rgb(128, 128, 132)},
	{Name: "Essco Manchester", City: 2, Color: rgb(220, 20, 40), Floor: rgb(120, 122, 128), Fuel: 1.43},
	{Name: "Ionnity Manchester", City: 2, Color: rgb(90, 40, 160), Floor: rgb(120, 125, 130), Charge: 0.6},
	// ---- motorway services ----
	{Name: "Welcome Brake Services", City: -1, MW: 1, Side: 1, Frac: 0.40, Color: rgb(200, 30, 60), Floor: rgb(118, 120, 126), Fuel: 1.69, Charge: 0.79},
	{Name: "Roadchief Services", City: -1, MW: 1, Side: -1, Frac: 0.58, Color: rgb(230, 120, 0), Floor: rgb(118, 120, 126), Fuel: 1.65, Charge: 0.75},
	{Name: "Watferd Gap Services", City: -1, MW: 2, Side: 1, Frac: 0.42, Color: rgb(40, 120, 60), Floor: rgb(118, 120, 126), Fuel: 1.72, Charge: 0.8},
	{Name: "Toddingtun Services", City: -1, MW: 2, Side: -1, Frac: 0.6, Color: rgb(200, 90, 20), Floor: rgb(118, 120, 126), Fuel: 1.68, Charge: 0.77},
}

type Rect struct {
	X, Y, W, H float64
	C          color.RGBA
	House      bool
}

type Camera struct {
	road int
	s    float64
	x, y float64 // road centre point
	bx   float64 // box position (pavement)
	by   float64
	cool float64
}

type cityDef struct {
	Name               string
	OX, OY, S          float64
	Cols, Rows         int
	MainRows, MainCols []int
	RowNames, ColNames []string
	Inner, Ring        float64
}

var cities = []*cityDef{
	{Name: "London", OX: lonOX, OY: lonOY, S: lonS, Cols: lonCols, Rows: lonRows,
		MainRows: []int{0, 2, 5}, MainCols: []int{0, 3, 7},
		RowNames: []string{"North Circular", "Station Road", "High Street", "Church Lane", "Victoria Road", "South Circular"},
		ColNames: []string{"Western Avenue", "Park Lane", "King's Road", "London Road", "Albert Road", "Market Street", "Green Lane", "Eastern Avenue"},
		Inner:    30, Ring: 40},
	{Name: "Cambridge", OX: camOX, OY: camOY, S: camS, Cols: camCols, Rows: camRows,
		MainRows: []int{0, 1, 3}, MainCols: []int{0, 5},
		RowNames: []string{"Huntingdon Road", "Newmarket Road", "Mill Road", "Hills Road"},
		ColNames: []string{"Madingley Road", "Trumpington Street", "King's Parade", "Regent Street", "Hills Avenue", "Cherry Hinton Road"},
		Inner:    20, Ring: 40},
	{Name: "Manchester", OX: manOX, OY: manOY, S: manS, Cols: manCols, Rows: manRows,
		MainRows: []int{0, 2, 3}, MainCols: []int{0, 2, 5},
		RowNames: []string{"Cheetham Hill Road", "Market Street", "Portland Street", "Mancunian Way"},
		ColNames: []string{"Chester Road", "Deansgate", "Princess Street", "Oxford Road", "Upper Brook Street", "Hyde Road"},
		Inner:    30, Ring: 40},
}

func inList(v int, l []int) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func (g *Game) cityAt(x, y float64) int {
	if y > lonOY-300 && x < lonOX+float64(lonCols-1)*lonS+300 {
		return 0
	}
	if x > camOX-300 && y > camOY-300 && y < camOY+float64(camRows-1)*camS+300 {
		return 1
	}
	if y < manOY+float64(manRows-1)*manS+300 && x < manOX+float64(manCols-1)*manS+300 {
		return 2
	}
	return -1
}

func (g *Game) isMotor(r int) bool { return r >= 0 && g.roads[r].Class == clMotorway }

// motorwayDest names where each direction of a motorway is heading.
func (g *Game) motorwayDest(r int, forward bool) string {
	rd := g.roads[r]
	switch rd.Name {
	case "M11":
		if forward {
			return "Cambridge"
		}
		return "London"
	case "M1":
		if forward {
			return "Manchester"
		}
		return "London"
	}
	return ""
}

func (g *Game) addNode(x, y float64, city int) int {
	g.nodes = append(g.nodes, Node{X: x, Y: y, City: city})
	return len(g.nodes) - 1
}

func (g *Game) addRoad(a, b int, pts [][2]float64, class int, limit float64, name string, city int) int {
	r := &Road{A: a, B: b, P: pts, Class: class, Limit: limit, Name: name, City: city}
	r.build()
	g.roads = append(g.roads, r)
	id := len(g.roads) - 1
	g.nodes[a].Roads = append(g.nodes[a].Roads, id)
	g.nodes[b].Roads = append(g.nodes[b].Roads, id)
	return id
}

// curve samples a road between two points, optionally bowed sideways.
func curve(ax, ay, bx, by, amp float64, sShape bool) [][2]float64 {
	l := math.Hypot(bx-ax, by-ay)
	n := int(l/24) + 2
	px, py := -(by-ay)/l, (bx-ax)/l
	var pts [][2]float64
	for i := 0; i <= n; i++ {
		u := float64(i) / float64(n)
		o := amp * math.Sin(math.Pi*u)
		if sShape {
			o = amp * math.Sin(2*math.Pi*u) * 0.8
		}
		pts = append(pts, [2]float64{ax + (bx-ax)*u + px*o, ay + (by-ay)*u + py*o})
	}
	return pts
}

func (g *Game) genCity(r *rand.Rand, ci int) [][]int {
	c := cities[ci]
	ids := make([][]int, c.Cols)
	for i := range ids {
		ids[i] = make([]int, c.Rows)
		for j := range ids[i] {
			x := c.OX + float64(i)*c.S
			y := c.OY + float64(j)*c.S
			if !inList(i, c.MainCols) {
				x += (r.Float64()*2 - 1) * c.S * 0.16
			}
			if !inList(j, c.MainRows) {
				y += (r.Float64()*2 - 1) * c.S * 0.16
			}
			ids[i][j] = g.addNode(x, y, ci)
		}
	}
	type edge struct {
		i, j, ni, nj int
		main         bool
		on           bool
	}
	var edges []*edge
	for j := 0; j < c.Rows; j++ {
		for i := 0; i < c.Cols; i++ {
			if i+1 < c.Cols {
				edges = append(edges, &edge{i, j, i + 1, j, inList(j, c.MainRows), true})
			}
			if j+1 < c.Rows {
				edges = append(edges, &edge{i, j, i, j + 1, inList(i, c.MainCols), true})
			}
		}
	}
	deg := map[[2]int]int{}
	for _, e := range edges {
		deg[[2]int{e.i, e.j}]++
		deg[[2]int{e.ni, e.nj}]++
	}
	connected := func() bool {
		seen := map[[2]int]bool{{0, 0}: true}
		stack := [][2]int{{0, 0}}
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, e := range edges {
				if !e.on {
					continue
				}
				var q [2]int
				switch p {
				case [2]int{e.i, e.j}:
					q = [2]int{e.ni, e.nj}
				case [2]int{e.ni, e.nj}:
					q = [2]int{e.i, e.j}
				default:
					continue
				}
				if !seen[q] {
					seen[q] = true
					stack = append(stack, q)
				}
			}
		}
		return len(seen) == c.Cols*c.Rows
	}
	// knock out ~40% of the minor streets so junctions are further apart
	order := r.Perm(len(edges))
	removed, target := 0, len(edges)*2/5
	for _, k := range order {
		e := edges[k]
		if e.main || removed >= target {
			continue
		}
		a, b := [2]int{e.i, e.j}, [2]int{e.ni, e.nj}
		if deg[a] <= 2 || deg[b] <= 2 {
			continue
		}
		e.on = false
		if !connected() {
			e.on = true
			continue
		}
		deg[a]--
		deg[b]--
		removed++
	}
	amps := []float64{0, 90, 150, 210, 260}
	for _, e := range edges {
		if !e.on {
			continue
		}
		a, b := ids[e.i][e.j], ids[e.ni][e.nj]
		na, nb := g.nodes[a], g.nodes[b]
		amp, sShape := 0.0, false
		if !e.main {
			amp = amps[r.Intn(len(amps))]
			if r.Intn(2) == 0 {
				amp = -amp
			}
			sShape = r.Float64() < 0.3
		}
		horiz := e.j == e.nj
		name := c.ColNames[e.i]
		if horiz {
			name = c.RowNames[e.j]
		}
		ring := (horiz && (e.j == 0 || e.j == c.Rows-1)) || (!horiz && (e.i == 0 || e.i == c.Cols-1))
		lim := c.Inner
		if ring {
			lim = c.Ring
		}
		cl := clMinor
		if e.main {
			cl = clMain
		}
		g.addRoad(a, b, curve(na.X, na.Y, nb.X, nb.Y, amp, sShape), cl, lim, name, ci)
	}
	return ids
}

func (g *Game) generate() {
	r := rand.New(rand.NewSource(17))
	g.nodes, g.roads, g.buildings, g.cameras = nil, nil, nil, nil
	byName := map[string]int{}
	for i, p := range places {
		if _, dup := byName[p.Name]; dup {
			panic("duplicate place " + p.Name)
		}
		byName[p.Name] = i
	}
	for _, p := range places {
		p.Dests = nil
		for _, n := range p.DestNames {
			idx, ok := byName[n]
			if !ok {
				panic("unknown place " + n)
			}
			p.Dests = append(p.Dests, idx)
		}
	}
	stations = nil
	for _, p := range places {
		if p.Fuel > 0 || p.Charge > 0 {
			stations = append(stations, Station{p: p, price: p.Fuel})
		}
	}

	lon := g.genCity(r, 0)
	cam := g.genCity(r, 1)
	man := g.genCity(r, 2)
	g.motorways = nil

	// M11: London -> link road -> motorway -> link road -> Cambridge
	la := lon[lonCols-1][2]
	m0 := g.addNode(mw0X, mwY, -1)
	m1 := g.addNode(mw1X, mwY, -1)
	cb := cam[0][1]
	g.addRoad(la, m0, curve(g.nodes[la].X, g.nodes[la].Y, mw0X, mwY, 0, false), clLink, 50, "M11 Link Road", -1)
	var mp [][2]float64
	span := mw1X - mw0X
	n := int(span) / 32
	for i := 0; i <= n; i++ {
		u := float64(i) / float64(n)
		mp = append(mp, [2]float64{mw0X + (mw1X-mw0X)*u, mwY + 380*math.Sin(2*math.Pi*u)*math.Sin(math.Pi*u)})
	}
	g.motorways = append(g.motorways, g.addRoad(m0, m1, mp, clMotorway, 70, "M11", -1))
	g.addRoad(m1, cb, curve(mw1X, mwY, g.nodes[cb].X, g.nodes[cb].Y, 0, false), clLink, 50, "M11 Link Road", -1)

	// M1: London (north side) -> motorway north -> Manchester
	ln := lon[3][0]
	s0 := g.addNode(m1X, m1BotY, -1)
	s1 := g.addNode(m1X, m1TopY, -1)
	mn := man[2][manRows-1]
	g.addRoad(ln, s0, curve(g.nodes[ln].X, g.nodes[ln].Y, m1X, m1BotY, 0, false), clLink, 50, "M1 Link Road", -1)
	mp = nil
	n = int(m1Len) / 32
	for i := 0; i <= n; i++ {
		u := float64(i) / float64(n)
		mp = append(mp, [2]float64{m1X + 420*math.Sin(2*math.Pi*u)*math.Sin(math.Pi*u), m1BotY - m1Len*u})
	}
	g.motorways = append(g.motorways, g.addRoad(s0, s1, mp, clMotorway, 70, "M1", -1))
	g.addRoad(s1, mn, curve(m1X, m1TopY, g.nodes[mn].X, g.nodes[mn].Y, 0, false), clLink, 50, "M1 Link Road", -1)

	g.buildSegIndex()
	g.setupJunctions(r)
	g.classifyTiles(r)
	g.placeYards(r)
	g.fillLots(r)

	// speed cameras on straight-ish town roads
	for tries := 0; len(g.cameras) < 16 && tries < 500; tries++ {
		ri := r.Intn(len(g.roads))
		rd := g.roads[ri]
		if rd.City < 0 || rd.Len < 600 {
			continue
		}
		dup := false
		for _, c := range g.cameras {
			if c.road == ri {
				dup = true
			}
		}
		if dup {
			continue
		}
		s := rd.Len / 2
		x, y, tx, ty := rd.at(s)
		lx, ly := ty, -tx
		bx, by := x+lx*(cityHalf+16), y+ly*(cityHalf+16)
		if k, _ := g.tileAt(bx, by); k.solid() || k == TYard {
			continue
		}
		g.cameras = append(g.cameras, &Camera{road: ri, s: s, x: x, y: y, bx: bx, by: by})
	}
}

// ---------- spatial index ----------

const bucketSz = 256

type segRef struct{ road, k int }

func bucketKey(bx, by int) int { return bx*4096 + by }

func (g *Game) buildSegIndex() {
	g.segIdx = map[int][]segRef{}
	for ri, rd := range g.roads {
		for k := 0; k+1 < len(rd.P); k++ {
			p, q := rd.P[k], rd.P[k+1]
			x0, x1 := math.Min(p[0], q[0]), math.Max(p[0], q[0])
			y0, y1 := math.Min(p[1], q[1]), math.Max(p[1], q[1])
			for bx := int(x0) / bucketSz; bx <= int(x1)/bucketSz; bx++ {
				for by := int(y0) / bucketSz; by <= int(y1)/bucketSz; by++ {
					key := bucketKey(bx, by)
					g.segIdx[key] = append(g.segIdx[key], segRef{ri, k})
				}
			}
		}
	}
}

type RoadHit struct {
	Road int
	S    float64
	Lat  float64 // signed: + is left of a->b
	D    float64 // distance from centre line
	Edge float64 // distance beyond the paved edge (negative = on the road)
}

// roadQuery finds the nearest road centre line within maxD.
func (g *Game) roadQuery(x, y, maxD float64) RoadHit {
	best := RoadHit{Road: -1, D: 1e9, Edge: 1e9}
	rb := int(maxD/bucketSz) + 1
	cx, cy := int(x)/bucketSz, int(y)/bucketSz
	for bx := cx - rb; bx <= cx+rb; bx++ {
		for by := cy - rb; by <= cy+rb; by++ {
			for _, sr := range g.segIdx[bucketKey(bx, by)] {
				rd := g.roads[sr.road]
				p, q := rd.P[sr.k], rd.P[sr.k+1]
				dx, dy := q[0]-p[0], q[1]-p[1]
				l2 := dx*dx + dy*dy
				u := 0.0
				if l2 > 0 {
					u = math.Max(0, math.Min(1, ((x-p[0])*dx+(y-p[1])*dy)/l2))
				}
				px, py := p[0]+dx*u, p[1]+dy*u
				d := math.Hypot(x-px, y-py)
				s := rd.Cum[sr.k] + u*math.Sqrt(l2)
				edge := d - rd.halfAt(s)
				if edge < best.Edge {
					l := math.Sqrt(l2)
					lat := ((x-px)*dy - (y-py)*dx) / l
					best = RoadHit{Road: sr.road, S: s, Lat: lat, D: d, Edge: edge}
				}
			}
		}
	}
	return best
}

// ---------- junctions ----------

func (g *Game) roadEndDir(road, node int) (inTx, inTy, sx, sy float64) {
	rd := g.roads[road]
	R := g.nodes[node].R
	if rd.A == node {
		_, _, tx, ty := rd.at(math.Min(R+20, rd.Len/2))
		x, y, _, _ := rd.at(math.Min(R+6, rd.Len/2))
		return -tx, -ty, x, y
	}
	_, _, tx, ty := rd.at(math.Max(rd.Len-R-20, rd.Len/2))
	x, y, _, _ := rd.at(math.Max(rd.Len-R-6, rd.Len/2))
	return tx, ty, x, y
}

func (g *Game) setupJunctions(r *rand.Rand) {
	for id := range g.nodes {
		n := &g.nodes[id]
		if len(n.Roads) >= 3 {
			n.R = 78
		} else {
			n.R = 26
		}
	}
	for id := range g.nodes {
		n := &g.nodes[id]
		n.LightOff = r.Float64() * 24
		n.Appr = nil
		for _, ri := range n.Roads {
			tx, ty, sx, sy := g.roadEndDir(ri, id)
			lx, ly := ty, -tx
			ax := 0
			if math.Abs(ty) > math.Abs(tx) {
				ax = 1
			}
			n.Appr = append(n.Appr, Approach{Road: ri, Tx: tx, Ty: ty, SX: sx, SY: sy, Axis: ax,
				SignX: sx + lx*(cityHalf+18) - tx*10, SignY: sy + ly*(cityHalf+18) - ty*10,
				LaneX: sx + lx*laneOff, LaneY: sy + ly*laneOff, OutTx: -tx, OutTy: -ty})
		}
		if len(n.Roads) < 3 {
			n.Ctrl = ctrlNone
			continue
		}
		mains := 0
		for _, ri := range n.Roads {
			if g.roads[ri].Class != clMinor {
				mains++
			}
		}
		switch {
		case mains >= 2 && len(n.Roads) == 4:
			n.Ctrl = ctrlLights
		case mains >= 2 && len(n.Roads) == 3 && r.Float64() < 0.5:
			n.Ctrl = ctrlLights
		case mains == 0 && len(n.Roads) == 4 && r.Float64() < 0.35:
			n.Ctrl = ctrlLights
		default:
			n.Ctrl = ctrlPriority
		}
		if n.Ctrl != ctrlPriority {
			continue
		}
		// pick the priority (major) road pair: main roads, else the straightest pair
		if mains >= 2 {
			for i := range n.Appr {
				n.Appr[i].Major = g.roads[n.Appr[i].Road].Class != clMinor
			}
		} else {
			bi, bj, best := 0, 1, 2.0
			for i := range n.Appr {
				for j := i + 1; j < len(n.Appr); j++ {
					d := n.Appr[i].Tx*n.Appr[j].Tx + n.Appr[i].Ty*n.Appr[j].Ty
					if d < best {
						best, bi, bj = d, i, j
					}
				}
			}
			n.Appr[bi].Major, n.Appr[bj].Major = true, true
		}
		for i := range n.Appr {
			if !n.Appr[i].Major && r.Float64() < 0.3 {
				n.Appr[i].Stop = true
			}
		}
	}
}

// ---------- terrain ----------

func (g *Game) classifyTiles(r *rand.Rand) {
	for x := 0; x < mapW; x++ {
		for y := 0; y < mapH; y++ {
			cx, cy := float64(x*tile+32), float64(y*tile+32)
			h := g.roadQuery(cx, cy, 420)
			g.tileEdge[x][y] = float32(h.Edge)
			city := g.cityAt(cx, cy)
			switch {
			case h.Edge < 0:
				g.tiles[x][y] = TRoad
			case city >= 0 && h.Edge < pavement:
				g.tiles[x][y] = TSidewalk
			case city >= 0:
				g.tiles[x][y] = TLot
			case h.Edge < 40:
				g.tiles[x][y] = TGrass
			default:
				g.tiles[x][y] = TField
			}
		}
	}
}

func (g *Game) placeYards(r *rand.Rand) {
	for i := range g.yardOf {
		for j := range g.yardOf[i] {
			g.yardOf[i][j] = 0
		}
	}
	taken := func(tx, ty int) bool {
		for x := tx - 1; x < tx+7; x++ {
			for y := ty - 1; y < ty+7; y++ {
				if x < 0 || y < 0 || x >= mapW || y >= mapH {
					return true
				}
				if g.yardOf[x][y] != 0 {
					return true
				}
			}
		}
		return false
	}
	for pi, p := range places {
		placed := false
		for tries := 0; tries < 4000 && !placed; tries++ {
			ri := r.Intn(len(g.roads))
			rd := g.roads[ri]
			if p.MW > 0 {
				ri = g.motorways[p.MW-1]
				rd = g.roads[ri]
			} else if rd.City != p.City {
				continue
			}
			margin := 330.0
			if rd.Len < 2*margin+50 {
				continue
			}
			s := margin + r.Float64()*(rd.Len-2*margin)
			if p.MW > 0 {
				s = rd.Len * (p.Frac + r.Float64()*0.08)
			}
			x, y, tx, ty := rd.at(s)
			// only beside straight, axis-aligned stretches
			if math.Abs(tx) < 0.97-float64(tries)/20000 && math.Abs(ty) < 0.97-float64(tries)/20000 {
				continue
			}
			side := 1.0
			if r.Intn(2) == 0 {
				side = -1
			}
			if p.MW > 0 {
				side = p.Side
				p.ServS = s
			}
			lx, ly := ty*side, -tx*side
			off := rd.halfAt(s) + pavement + 4
			var ttx, tty, face int
			if math.Abs(lx) > math.Abs(ly) { // yard east or west of road
				if lx > 0 {
					face = 3
					ttx = int(math.Ceil((x + off) / tile))
				} else {
					face = 1
					ttx = int(math.Floor((x-off)/tile)) - 6
				}
				tty = int(y/tile) - 3
			} else {
				if ly > 0 {
					face = 0
					tty = int(math.Ceil((y + off) / tile))
				} else {
					face = 2
					tty = int(math.Floor((y-off)/tile)) - 6
				}
				ttx = int(x/tile) - 3
			}
			if ttx < 1 || tty < 1 || ttx+7 >= mapW || tty+7 >= mapH || taken(ttx, tty) {
				continue
			}
			ok := true
			for xx := ttx; xx < ttx+6 && ok; xx++ {
				for yy := tty; yy < tty+6; yy++ {
					if g.tileEdge[xx][yy] < pavement+8 {
						ok = false
						break
					}
				}
			}
			// keep clear of junctions
			for _, nd := range g.nodes {
				if math.Hypot(nd.X-float64(ttx*tile+192), nd.Y-float64(tty*tile+192)) < 520-float64(tries)/25 {
					ok = false
				}
			}
			if !ok {
				continue
			}
			p.TX, p.TY, p.Face = ttx, tty, face
			for xx := ttx; xx < ttx+6; xx++ {
				for yy := tty; yy < tty+6; yy++ {
					g.tiles[xx][yy] = TYard
					g.yardOf[xx][yy] = int16(pi + 1)
				}
			}
			bx, by, bw, bh := p.building()
			for xx := int(bx / tile); xx <= int((bx+bw-1)/tile); xx++ {
				for yy := int(by / tile); yy <= int((by+bh-1)/tile); yy++ {
					g.tiles[xx][yy] = TYardBuilding
				}
			}
			// driveway from the yard to the road
			dxs, dys := []int{}, []int{}
			switch face {
			case 0:
				for yy := tty - 1; yy >= tty-5 && yy >= 0 && g.tileEdge[ttx][yy] >= -8; yy-- {
					dys = append(dys, yy)
				}
				for xx := ttx + 1; xx < ttx+5; xx++ {
					for _, yy := range dys {
						g.drive(xx, yy, pi)
					}
				}
			case 2:
				for yy := tty + 6; yy <= tty+10 && yy < mapH && g.tileEdge[ttx][yy] >= -8; yy++ {
					dys = append(dys, yy)
				}
				for xx := ttx + 1; xx < ttx+5; xx++ {
					for _, yy := range dys {
						g.drive(xx, yy, pi)
					}
				}
			case 3:
				for xx := ttx - 1; xx >= ttx-5 && xx >= 0 && g.tileEdge[xx][tty] >= -8; xx-- {
					dxs = append(dxs, xx)
				}
				for yy := tty + 1; yy < tty+5; yy++ {
					for _, xx := range dxs {
						g.drive(xx, yy, pi)
					}
				}
			case 1:
				for xx := ttx + 6; xx <= ttx+10 && xx < mapW && g.tileEdge[xx][tty] >= -8; xx++ {
					dxs = append(dxs, xx)
				}
				for yy := tty + 1; yy < tty+5; yy++ {
					for _, xx := range dxs {
						g.drive(xx, yy, pi)
					}
				}
			}
			placed = true
		}
		if !placed {
			panic("could not place " + p.Name)
		}
	}
}

func (g *Game) drive(x, y, pi int) {
	if g.tiles[x][y] == TRoad {
		return
	}
	g.tiles[x][y] = TYard
	g.yardOf[x][y] = int16(pi + 1)
}

var palette = []color.RGBA{rgb(170, 95, 85), rgb(125, 125, 145), rgb(190, 170, 125), rgb(95, 115, 135),
	rgb(160, 140, 170), rgb(115, 145, 115), rgb(200, 125, 85), rgb(150, 150, 150)}
var roofs = []color.RGBA{rgb(150, 70, 55), rgb(110, 60, 50), rgb(90, 90, 100), rgb(170, 90, 60), rgb(70, 70, 80)}

func hash2(x, y, salt int) float64 {
	h := uint32(x*374761393 + y*668265263 + salt*1442695041)
	h = (h ^ (h >> 13)) * 1274126177
	h ^= h >> 16
	return float64(h%10000) / 10000
}

func (g *Game) fillLots(r *rand.Rand) {
	// district type per 4x4 tile cell
	district := func(x, y int) int {
		v := hash2(x/4, y/4, 7)
		switch {
		case v < 0.5:
			return 0 // offices / shops
		case v < 0.8:
			return 1 // houses
		}
		return 2 // park
	}
	buildable := func(x, y int) bool {
		return x >= 0 && y >= 0 && x < mapW && y < mapH && g.tiles[x][y] == TLot && g.tileEdge[x][y] > pavement+40
	}
	for x := 0; x < mapW; x++ {
		for y := 0; y < mapH; y++ {
			if g.tiles[x][y] != TLot {
				continue
			}
			if !buildable(x, y) {
				g.tiles[x][y] = TSidewalk
				continue
			}
			switch district(x, y) {
			case 0:
				if x%2 == 0 && y%2 == 0 && buildable(x+1, y) && buildable(x, y+1) && buildable(x+1, y+1) {
					c := palette[r.Intn(len(palette))]
					if g.cityAt(float64(x*tile), float64(y*tile)) == 1 && r.Float64() < 0.6 {
						c = []color.RGBA{rgb(200, 185, 150), rgb(185, 170, 135), rgb(170, 160, 140)}[r.Intn(3)]
					}
					g.buildings = append(g.buildings, Rect{X: float64(x*tile) + 5, Y: float64(y*tile) + 5, W: 2*tile - 10, H: 2*tile - 10, C: c})
					for _, d := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
						g.tiles[x+d[0]][y+d[1]] = TBuilding
					}
				}
			case 1:
				g.buildings = append(g.buildings, Rect{X: float64(x*tile) + 8, Y: float64(y*tile) + 6, W: tile - 16, H: tile - 22, C: roofs[r.Intn(len(roofs))], House: true})
				g.tiles[x][y] = TBuilding
			case 2:
				g.tiles[x][y] = TGrass
				v := hash2(x, y, 3)
				if v < 0.25 {
					g.tiles[x][y] = TTree
				} else if hash2(x/3, y/3, 9) < 0.12 {
					g.tiles[x][y] = TWater
				}
			}
		}
	}
	// any lot tiles left over (odd edges of office blocks) become small car parks / grass
	for x := 0; x < mapW; x++ {
		for y := 0; y < mapH; y++ {
			if g.tiles[x][y] == TLot {
				if hash2(x, y, 5) < 0.5 {
					g.tiles[x][y] = TGrass
				} else {
					g.tiles[x][y] = TSidewalk
				}
			}
			if g.tiles[x][y] == TField && g.tileEdge[x][y] > 90 && (x%15 == 0 || y%12 == 0) && hash2(x, y, 1) < 0.85 {
				g.tiles[x][y] = THedge
			}
			// motorway fence line
			if g.tiles[x][y] == TField && g.tileEdge[x][y] > 70 && g.tileEdge[x][y] < 134 {
				h := g.roadQuery(float64(x*tile+32), float64(y*tile+32), 500)
				if h.Road >= 0 && g.roads[h.Road].Class == clMotorway {
					g.tiles[x][y] = THedge
				}
			}
		}
	}
}

func (g *Game) tileAt(x, y float64) (TileKind, bool) {
	if x < 0 || y < 0 || x >= worldW || y >= worldH {
		return TBuilding, false
	}
	return g.tiles[int(x/tile)][int(y/tile)], true
}

func (g *Game) solidAt(x, y float64) bool {
	k, ok := g.tileAt(x, y)
	if !ok || k.solid() {
		return true
	}
	// central reservation barrier on the motorway
	if g.cityAt(x, y) < 0 {
		h := g.roadQuery(x, y, 300)
		if h.Road >= 0 && g.roads[h.Road].Class == clMotorway && math.Abs(h.Lat) < 8 &&
			h.S > mwTaper && h.S < g.roads[h.Road].Len-mwTaper {
			return true
		}
	}
	for _, in := range g.incidents {
		if in.contains(g, x, y) {
			return true
		}
	}
	return false
}

// roadAt returns the town, street name and speed limit at a world position.
func (g *Game) roadAt(x, y float64) (string, string, float64) {
	town := "Countryside"
	if c := g.cityAt(x, y); c >= 0 {
		town = cities[c].Name
	}
	h := g.roadQuery(x, y, 300)
	if h.Road < 0 || h.Edge > 4 {
		lim := 30.0
		if town == "Cambridge" {
			lim = 20
		}
		return town, "", lim
	}
	rd := g.roads[h.Road]
	name := rd.Name
	if rd.Class == clMotorway {
		town = rd.Name
		if math.Abs(h.Lat) > mwRes+3*mwLane {
			name = rd.Name + " (hard shoulder)"
		} else {
			name = rd.Name + " motorway"
		}
	}
	return town, name, rd.Limit
}

// ---------- static world rendering (chunked, lazily) ----------

func (g *Game) chunk(cx, cy int) *ebiten.Image {
	k := [2]int{cx, cy}
	if img, ok := g.chunkCache[k]; ok {
		g.chunkUsed[k] = g.frameNo
		return img
	}
	if len(g.chunkCache) >= 24 {
		var oldest [2]int
		best := int64(math.MaxInt64)
		for kk, t := range g.chunkUsed {
			if t < best {
				best, oldest = t, kk
			}
		}
		g.chunkCache[oldest].Deallocate()
		delete(g.chunkCache, oldest)
		delete(g.chunkUsed, oldest)
	}
	img := ebiten.NewImage(chunkSz, chunkSz)
	g.drawChunk(img, float64(cx*chunkSz), float64(cy*chunkSz))
	g.chunkCache[k] = img
	g.chunkUsed[k] = g.frameNo
	return img
}

func (g *Game) buildWorld() {
	g.chunkCache = map[[2]int]*ebiten.Image{}
	g.chunkUsed = map[[2]int]int64{}
	g.bms = 0.11
	k := g.bms
	g.bigMap = ebiten.NewImage(int(worldW*k)+1, int(worldH*k)+1)
	bm := g.bigMap
	bm.Fill(rgb(80, 130, 60))
	ts := tile * k
	for x := 0; x < mapW; x++ {
		for y := 0; y < mapH; y++ {
			var c color.RGBA
			switch g.tiles[x][y] {
			case TField:
				c = fieldCols[(x/15*7+y/12*3)%len(fieldCols)]
			case THedge:
				c = rgb(50, 100, 50)
			case TSidewalk, TRoad:
				if g.cityAt(float64(x*tile), float64(y*tile)) >= 0 {
					c = rgb(150, 150, 148)
				} else {
					continue
				}
			case TBuilding:
				c = rgb(90, 145, 70)
			case TGrass:
				c = rgb(72, 140, 62)
			case TTree:
				c = rgb(40, 100, 45)
			case TWater:
				c = rgb(60, 120, 190)
			case TYard, TYardBuilding:
				c = rgb(120, 120, 125)
			default:
				continue
			}
			fillRect(bm, float64(x)*ts, float64(y)*ts, ts+0.5, ts+0.5, c)
		}
	}
	for _, b := range g.buildings {
		fillRect(bm, b.X*k, b.Y*k, b.W*k, b.H*k, b.C)
	}
	for _, p := range places {
		x, y, w, h := p.building()
		fillRect(bm, x*k, y*k, w*k, h*k, rgb(90, 90, 95))
	}
	for _, rd := range g.roads {
		w := cityHalf * 2 * k
		col := color.Color(rgb(58, 58, 64))
		if rd.Class == clMotorway {
			w = mwHalf * 2 * k
			col = rgb(70, 90, 140)
		}
		for i := 1; i < len(rd.P); i++ {
			a, b := rd.P[i-1], rd.P[i]
			strokeLine(bm, a[0]*k, a[1]*k, b[0]*k, b[1]*k, w, col)
		}
	}
	for _, n := range g.nodes {
		fillCircle(bm, n.X*k, n.Y*k, cityHalf*k*1.1, rgb(58, 58, 64))
	}
	g.mini = ebiten.NewImage(280, 190)
}

var fieldCols = []color.RGBA{rgb(110, 150, 60), rgb(200, 190, 70), rgb(140, 110, 70), rgb(90, 145, 70), rgb(160, 170, 80)}

func fillQuad(dst *ebiten.Image, pts [4][2]float64, c color.Color) {
	cr, cg, cb, ca := c.RGBA()
	fr, fg, fb, fa := float32(cr)/0xffff, float32(cg)/0xffff, float32(cb)/0xffff, float32(ca)/0xffff
	vs := make([]ebiten.Vertex, 4)
	for i, p := range pts {
		vs[i] = ebiten.Vertex{DstX: float32(p[0]), DstY: float32(p[1]), SrcX: 1, SrcY: 1, ColorR: fr, ColorG: fg, ColorB: fb, ColorA: fa}
	}
	op := &ebiten.DrawTrianglesOptions{AntiAlias: true, ColorScaleMode: ebiten.ColorScaleModePremultipliedAlpha}
	dst.DrawTriangles(vs, []uint16{0, 1, 2, 0, 2, 3}, whiteSub, op)
}

// band fills the strip between lateral offsets lo..hi (left of a->b) along s0..s1 of a road.
func (g *Game) band(img *ebiten.Image, rd *Road, s0, s1, lo, hi float64, c color.Color, ox, oy float64, widthFn func(s float64) (float64, float64)) {
	s0, s1 = math.Max(0, s0), math.Min(rd.Len, s1)
	step := 24.0
	for s := s0; s < s1; s += step {
		e := math.Min(s+step+1, s1)
		x0, y0, tx0, ty0 := rd.at(s)
		x1, y1, tx1, ty1 := rd.at(e)
		a, b := lo, hi
		a2, b2 := lo, hi
		if widthFn != nil {
			a, b = widthFn(s)
			a2, b2 = widthFn(e)
		}
		l0x, l0y := ty0, -tx0
		l1x, l1y := ty1, -tx1
		fillQuad(img, [4][2]float64{
			{x0 + l0x*a - ox, y0 + l0y*a - oy}, {x1 + l1x*a2 - ox, y1 + l1y*a2 - oy},
			{x1 + l1x*b2 - ox, y1 + l1y*b2 - oy}, {x0 + l0x*b - ox, y0 + l0y*b - oy}}, c)
	}
}

func (g *Game) dashes(img *ebiten.Image, rd *Road, s0, s1, lat, on, off, w float64, c color.Color, ox, oy float64) {
	for s := s0; s < s1; s += on + off {
		e := math.Min(s+on, s1)
		g.band(img, rd, s, e, lat-w/2, lat+w/2, c, ox, oy, nil)
	}
}

func (g *Game) drawChunk(img *ebiten.Image, ox, oy float64) {
	R := func(x, y, w, h float64, c color.Color) { fillRect(img, x-ox, y-oy, w, h, c) }
	C := func(x, y, r float64, c color.Color) { fillCircle(img, x-ox, y-oy, r, c) }
	T := func(s string, f *text.GoTextFace, x, y float64, c color.Color) {
		drawText(img, s, f, x-ox, y-oy, c, text.AlignCenter)
	}
	in := func(x, y, m float64) bool {
		return x > ox-m && y > oy-m && x < ox+chunkSz+m && y < oy+chunkSz+m
	}
	img.Fill(rgb(80, 130, 60))
	tx0, ty0 := int(ox/tile)-1, int(oy/tile)-1
	tx1, ty1 := tx0+chunkSz/tile+3, ty0+chunkSz/tile+3
	clampI := func(v, hi int) int { return max(0, min(v, hi)) }
	tx0, ty0, tx1, ty1 = clampI(tx0, mapW), clampI(ty0, mapH), clampI(tx1, mapW), clampI(ty1, mapH)
	white := rgb(230, 230, 230)
	asphalt := rgb(58, 58, 64)
	paveCol := rgb(150, 150, 148)

	// ground
	for x := tx0; x < tx1; x++ {
		for y := ty0; y < ty1; y++ {
			r := rand.New(rand.NewSource(int64(x*7919 + y*104729)))
			px, py := float64(x*tile), float64(y*tile)
			switch g.tiles[x][y] {
			case TField:
				fc := fieldCols[(x/15*7+y/12*3)%len(fieldCols)]
				R(px, py, tile, tile, fc)
				dark := color.RGBA{fc.R * 7 / 8, fc.G * 7 / 8, fc.B * 7 / 8, 255}
				for k := 0.0; k < tile; k += 16 {
					if (x/15+y/12)%2 == 0 {
						R(px, py+k, tile, 6, dark)
					} else {
						R(px+k, py, 6, tile, dark)
					}
				}
			case THedge:
				R(px, py, tile, tile, rgb(80, 130, 60))
				for k := 0; k < 3; k++ {
					C(px+12+float64(k)*20+r.Float64()*6, py+32+r.Float64()*10-5, 16, rgb(45, 95, 45))
				}
			case TSidewalk, TRoad:
				if g.cityAt(px, py) >= 0 {
					R(px, py, tile, tile, paveCol)
					R(px, py, tile, 1, rgb(138, 138, 136))
					R(px, py, 1, tile, rgb(138, 138, 136))
				} else {
					R(px, py, tile, tile, rgb(80, 130, 60))
				}
			case TBuilding:
				R(px, py, tile, tile, rgb(90, 145, 70))
			case TGrass, TTree:
				R(px, py, tile, tile, rgb(72, 140, 62))
				for i := 0; i < 6; i++ {
					R(px+r.Float64()*60, py+r.Float64()*60, 3, 3, rgb(85, 158, 72))
				}
			case TWater:
				R(px, py, tile, tile, rgb(60, 120, 190))
				R(px+10+r.Float64()*30, py+15+r.Float64()*30, 16, 2, rgb(110, 165, 220))
			case TYard, TYardBuilding:
				fl := rgb(120, 120, 120)
				if pi := g.yardOf[x][y]; pi > 0 {
					fl = places[pi-1].Floor
				}
				R(px, py, tile, tile, fl)
				R(px, py, tile, 1, color.RGBA{0, 0, 0, 25})
				R(px, py, 1, tile, color.RGBA{0, 0, 0, 25})
			}
		}
	}

	// roads: pavements, then asphalt, then markings
	var vis []int
	for ri, rd := range g.roads {
		m := mwHalf + 100
		if rd.x1+m < ox || rd.y1+m < oy || rd.x0-m > ox+chunkSz || rd.y0-m > oy+chunkSz {
			continue
		}
		vis = append(vis, ri)
	}
	var visN []int
	for id, n := range g.nodes {
		if in(n.X, n.Y, 200) {
			visN = append(visN, id)
		}
	}
	for _, ri := range vis {
		rd := g.roads[ri]
		if rd.City >= 0 {
			hw := cityHalf + pavement
			g.band(img, rd, 0, rd.Len, -hw, hw, paveCol, ox, oy, nil)
			g.band(img, rd, 0, rd.Len, -cityHalf-4, cityHalf+4, rgb(120, 120, 118), ox, oy, nil) // kerb
		} else {
			g.band(img, rd, 0, rd.Len, -1, 1, asphalt, ox, oy, func(s float64) (float64, float64) {
				h := rd.halfAt(s) + 10
				return -h, h
			})
		}
	}
	for _, id := range visN {
		n := g.nodes[id]
		if n.City >= 0 {
			C(n.X, n.Y, n.R+pavement+8, paveCol)
		}
	}
	for _, ri := range vis {
		rd := g.roads[ri]
		g.band(img, rd, 0, rd.Len, 0, 0, asphalt, ox, oy, func(s float64) (float64, float64) {
			h := rd.halfAt(s)
			return -h, h
		})
	}
	for _, id := range visN {
		n := g.nodes[id]
		r := cityHalf + 6
		if len(n.Roads) >= 3 {
			r = n.R + 4
		}
		C(n.X, n.Y, r, asphalt)
	}
	// markings
	for _, ri := range vis {
		rd := g.roads[ri]
		ra, rb := g.nodes[rd.A].R+14, rd.Len-g.nodes[rd.B].R-14
		switch rd.Class {
		case clMotorway:
			s0, s1 := mwTaper, rd.Len-mwTaper
			// central reservation + barrier
			g.band(img, rd, s0, s1, -mwRes, mwRes, rgb(120, 140, 100), ox, oy, nil)
			g.band(img, rd, s0, s1, -3, 3, rgb(170, 170, 175), ox, oy, nil)
			g.band(img, rd, s0, s1, -1, 1, rgb(90, 90, 95), ox, oy, nil)
			for _, sgn := range []float64{1, -1} {
				// solid line next to reservation and next to hard shoulder
				g.band(img, rd, s0, s1, sgn*(mwRes+2), sgn*(mwRes+6), white, ox, oy, nil)
				edge := mwRes + 3*mwLane
				g.band(img, rd, s0, s1, sgn*(edge-2), sgn*(edge+3), white, ox, oy, nil)
				g.band(img, rd, s0, s1, sgn*(edge+3), sgn*(mwHalf), rgb(70, 70, 76), ox, oy, nil)
				for k := 1; k <= 2; k++ {
					g.dashes(img, rd, s0, s1, sgn*(mwRes+float64(k)*mwLane), 40, 50, 4, white, ox, oy)
				}
			}
			// taper zones: single centre line
			g.dashes(img, rd, 0, s0, 0, 30, 30, 4, white, ox, oy)
			g.dashes(img, rd, s1, rd.Len, 0, 30, 30, 4, white, ox, oy)
		default:
			g.dashes(img, rd, ra, rb, 0, 22, 26, 4, white, ox, oy)
			if rd.Class == clMain || rd.Class == clLink {
				g.band(img, rd, ra, rb, cityHalf-10, cityHalf-7, white, ox, oy, nil)
				g.band(img, rd, ra, rb, -cityHalf+7, -cityHalf+10, white, ox, oy, nil)
			}
		}
	}
	// stop / give way lines and signs
	for _, id := range visN {
		n := g.nodes[id]
		if n.Ctrl == ctrlNone {
			continue
		}
		for _, a := range n.Appr {
			rd := g.roads[a.Road]
			// line across the approach lane (left half of the road for traffic coming in)
			lx, ly := a.Ty, -a.Tx // left of travel
			p0x, p0y := a.SX+lx*2, a.SY+ly*2
			p1x, p1y := a.SX+lx*(cityHalf-2), a.SY+ly*(cityHalf-2)
			thick := 6.0
			if n.Ctrl == ctrlLights || a.Stop {
				strokeLine(img, p0x-ox, p0y-oy, p1x-ox, p1y-oy, thick, white)
			} else if !a.Major {
				// give way: double dashed line + triangle marking
				for k := 0.0; k < 1; k += 0.2 {
					for _, back := range []float64{0, 10} {
						ax, ay := p0x+(p1x-p0x)*k-a.Tx*back, p0y+(p1y-p0y)*k-a.Ty*back
						bx, by := p0x+(p1x-p0x)*(k+0.12)-a.Tx*back, p0y+(p1y-p0y)*(k+0.12)-a.Ty*back
						strokeLine(img, ax-ox, ay-oy, bx-ox, by-oy, 4, white)
					}
				}
				mx, my := a.LaneX-a.Tx*60, a.LaneY-a.Ty*60
				tip := [2]float64{mx + a.Tx*20, my + a.Ty*20}
				fillTri(img, mx-a.Tx*20+lx*14-ox, my-a.Ty*20+ly*14-oy, mx-a.Tx*20-lx*14-ox, my-a.Ty*20-ly*14-oy, tip[0]-ox, tip[1]-oy, rgb(230, 230, 230))
			}
			if a.Stop {
				mx, my := a.LaneX-a.Tx*40, a.LaneY-a.Ty*40
				T("STOP", faceSign, mx, my-8, white)
			}
			_ = rd
			// signs on the pavement
			if n.Ctrl == ctrlPriority && !a.Major {
				sx, sy := a.SignX, a.SignY
				if a.Stop {
					C(sx+2, sy+2, 15, color.RGBA{0, 0, 0, 90})
					drawOct(img, sx-ox, sy-oy, 15, rgb(200, 20, 20))
					drawOct(img, sx-ox, sy-oy, 12, rgb(230, 230, 230))
					drawOct(img, sx-ox, sy-oy, 10.5, rgb(200, 20, 20))
					T("STOP", faceTiny, sx, sy-8, color.White)
				} else {
					fillTri(img, sx-17-ox, sy-12-oy, sx+17-ox, sy-12-oy, sx-ox, sy+16-oy, rgb(210, 20, 20))
					fillTri(img, sx-11-ox, sy-8-oy, sx+11-ox, sy-8-oy, sx-ox, sy+9-oy, rgb(240, 240, 240))
				}
			}
		}
	}
	// speed limit signs at the start of each road
	for _, ri := range vis {
		rd := g.roads[ri]
		if rd.Class == clMotorway {
			continue
		}
		for _, fwd := range []bool{true, false} {
			var s float64
			var x, y, tx, ty float64
			if fwd {
				s = g.nodes[rd.A].R + 50
				x, y, tx, ty = rd.at(s)
			} else {
				s = rd.Len - g.nodes[rd.B].R - 50
				x, y, tx, ty = rd.at(s)
				tx, ty = -tx, -ty
			}
			lx, ly := ty, -tx
			sx, sy := x+lx*(cityHalf+16), y+ly*(cityHalf+16)
			if !in(sx, sy, 30) {
				continue
			}
			C(sx+2, sy+2, 14, color.RGBA{0, 0, 0, 90})
			C(sx, sy, 14, rgb(210, 20, 20))
			C(sx, sy, 10.5, color.White)
			T(fmt.Sprintf("%d", int(rd.Limit)), faceSign, sx, sy-8, rgb(10, 10, 10))
		}
	}
	// trees
	for x := tx0; x < tx1; x++ {
		for y := ty0; y < ty1; y++ {
			if g.tiles[x][y] == TTree {
				cx, cy := float64(x*tile+32)+hash2(x, y, 2)*8-4, float64(y*tile+32)+hash2(x, y, 4)*8-4
				C(cx+6, cy+6, 26, color.RGBA{0, 0, 0, 70})
				C(cx, cy, 26, rgb(40, 100, 45))
				C(cx-6, cy-6, 15, rgb(60, 130, 60))
			}
		}
	}
	// buildings
	for bi, b := range g.buildings {
		if b.X > ox+chunkSz+20 || b.Y > oy+chunkSz+20 || b.X+b.W < ox-20 || b.Y+b.H < oy-20 {
			continue
		}
		R(b.X+6, b.Y+6, b.W, b.H, color.RGBA{0, 0, 0, 90})
		if b.House {
			dark := color.RGBA{b.C.R * 3 / 4, b.C.G * 3 / 4, b.C.B * 3 / 4, 255}
			R(b.X, b.Y, b.W, b.H/2, b.C)
			R(b.X, b.Y+b.H/2, b.W, b.H/2, dark)
			R(b.X+b.W-14, b.Y+6, 7, 9, rgb(120, 70, 60))
			continue
		}
		R(b.X, b.Y, b.W, b.H, b.C)
		dark := color.RGBA{b.C.R * 4 / 5, b.C.G * 4 / 5, b.C.B * 4 / 5, 255}
		R(b.X+8, b.Y+8, b.W-16, b.H-16, dark)
		R(b.X+12, b.Y+12, b.W-24, b.H-24, b.C)
		ax, ay := b.X+20+hash2(bi, 1, 1)*(b.W-60), b.Y+20+hash2(bi, 2, 1)*(b.H-60)
		R(ax, ay, 18, 14, rgb(185, 185, 190))
		R(ax+4, ay+3, 10, 8, rgb(120, 120, 125))
	}
	// yards
	for _, p := range places {
		pox, poy, pw, ph := p.rect()
		if !in(pox+pw/2, poy+ph/2, pw) {
			continue
		}
		bx, by, bw, bh := p.building()
		R(bx+8, by+8, bw, bh, color.RGBA{0, 0, 0, 90})
		R(bx, by, bw, bh, rgb(90, 90, 95))
		if bw > bh {
			for i := 0.0; i < bw; i += 16 {
				R(bx+i, by, 8, bh, rgb(100, 100, 106))
			}
		} else {
			for i := 0.0; i < bh; i += 16 {
				R(bx, by+i, bw, 8, rgb(100, 100, 106))
			}
		}
		// coloured fascia on the side facing the yard
		switch p.Face {
		case 0:
			R(bx, by, bw, 18, p.Color)
		case 1:
			R(bx+bw-18, by, 18, bh, p.Color)
		case 3:
			R(bx, by, 18, bh, p.Color)
		default:
			R(bx, by+bh-18, bw, 18, p.Color)
		}
		if p.Dealer {
			R(bx+18, by+18, bw-36, bh-36, rgb(40, 60, 110))
		}
		// name (wrapped when the building is tall and thin)
		lines := []string{p.Name}
		f := faceMed
		if bw < bh {
			lines = strings.Fields(p.Name)
			f = faceSign
		}
		sub := ""
		switch {
		case p.Cargo != "":
			sub = "Collect: " + p.Cargo
		case p.Dealer:
			sub = "Truck sales"
		case p.Fuel > 0 && p.Charge > 0:
			sub = fmt.Sprintf("Diesel %.0fp  ·  EV %.0fp", p.Fuel*100, p.Charge*100)
		case p.Fuel > 0:
			sub = fmt.Sprintf("Diesel %.0fp / litre", p.Fuel*100)
		case p.Charge > 0:
			sub = fmt.Sprintf("EV %.0fp / kWh", p.Charge*100)
		}
		lh := f.Size * 1.3
		ty := by + bh/2 - float64(len(lines))*lh/2 - 8
		for i, l := range lines {
			T(l, f, bx+bw/2, ty+float64(i)*lh, color.White)
		}
		if sub != "" {
			sf := faceSmall
			if bw < bh {
				sf = faceTiny
			}
			T(sub, sf, bx+bw/2, ty+float64(len(lines))*lh+2, color.RGBA{230, 230, 230, 255})
		}
		x, y, w, h := p.bay()
		c := p.Color
		if p.Dealer {
			c = rgb(255, 220, 60)
		}
		label := "LOADING BAY"
		switch {
		case p.Dealer:
			label = "SHOWROOM"
		case p.Fuel > 0 && p.Charge > 0:
			label = "PUMPS+EV"
		case p.Fuel > 0:
			label = "PUMPS"
		case p.Charge > 0:
			label = "CHARGERS"
		}
		for i := 0.0; i < w; i += 24 {
			R(x+i, y, 12, 4, c)
			R(x+i, y+h-4, 12, 4, c)
		}
		for i := 0.0; i < h; i += 24 {
			R(x, y+i, 4, 12, c)
			R(x+w-4, y+i, 4, 12, c)
		}
		if p.Fuel > 0 || p.Charge > 0 {
			for k := 0; k < 3; k++ {
				col := p.Color
				if p.Charge > 0 && (p.Fuel == 0 || k == 2) {
					col = rgb(0, 200, 180)
				}
				px, py := x+w/2+float64(k-1)*40, y+h/2+24
				R(px-7, py-6, 14, 12, rgb(40, 40, 40))
				R(px-5, py-4, 10, 8, col)
			}
		}
		T(label, faceSmall, x+w/2, y+h/2-10, color.NRGBA{255, 255, 255, 110})
	}
	// motorway signs (blue boards on the left of each carriageway)
	sign := func(x, y float64, l1, l2 string) {
		if !in(x, y, 120) {
			return
		}
		R(x-88, y-28, 176, 56, rgb(240, 240, 240))
		R(x-86, y-26, 172, 52, rgb(20, 70, 170))
		T(l1, faceSign, x, y-20, color.White)
		T(l2, faceSign, x, y, color.White)
	}
	for _, mi := range g.motorways {
		mr := g.roads[mi]
		for s := mwTaper + 400; s < mr.Len-mwTaper-200; s += 2600 {
			x, y, tx, ty := mr.at(s)
			lx, ly := ty, -tx
			sign(x+lx*(mwHalf+80), y+ly*(mwHalf+80), mr.Name+"  "+g.motorwayDest(mi, true), fmt.Sprintf("%.0f miles", (mr.Len-s)/pxPerMile))
			x2, y2, _, _ := mr.at(mr.Len - s)
			sign(x2-lx*(mwHalf+80), y2-ly*(mwHalf+80), mr.Name+"  "+g.motorwayDest(mi, false), fmt.Sprintf("%.0f miles", s/pxPerMile))
		}
	}
	for _, p := range places {
		if p.MW == 0 {
			continue
		}
		mr := g.roads[g.motorways[p.MW-1]]
		for _, back := range []float64{1600, 800} {
			ss := p.ServS - back*p.Side
			x, y, tx, ty := mr.at(ss)
			lx, ly := ty*p.Side, -tx*p.Side
			sign(x+lx*(mwHalf+80), y+ly*(mwHalf+80), "Services: "+strings.Fields(p.Name)[0], fmt.Sprintf("%.1f miles", back/pxPerMile))
		}
	}
	// speed cameras
	for _, cam := range g.cameras {
		if !in(cam.x, cam.y, 200) {
			continue
		}
		rd := g.roads[cam.road]
		g.dashes(img, rd, cam.s-60, cam.s+60, cityHalf/2, 4, 20, cityHalf-6, color.NRGBA{230, 230, 230, 200}, ox, oy)
		g.dashes(img, rd, cam.s-60, cam.s+60, -cityHalf/2, 4, 20, cityHalf-6, color.NRGBA{230, 230, 230, 200}, ox, oy)
		R(cam.bx-9, cam.by-9, 18, 18, rgb(30, 30, 30))
		R(cam.bx-8, cam.by-8, 16, 16, rgb(250, 200, 0))
		C(cam.bx, cam.by, 4, rgb(20, 20, 20))
	}
}

func drawOct(dst *ebiten.Image, x, y, r float64, c color.RGBA) {
	for k := 0; k < 8; k++ {
		a0 := math.Pi/8 + float64(k)*math.Pi/4
		a1 := a0 + math.Pi/4
		fillTri(dst, x, y, x+math.Cos(a0)*r, y+math.Sin(a0)*r, x+math.Cos(a1)*r, y+math.Sin(a1)*r, c)
	}
}

package main

import (
	"fmt"
	"image/color"
	"math"
	"math/rand"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

const (
	screenW = 1280
	screenH = 720
	tile    = 64
	period  = 8

	lonBX      = 10
	lonBY      = 7
	camBX      = 6
	camBY      = 4
	lonW       = lonBX*period + 2 // 82 tiles
	motorTiles = 240
	camTX      = lonW + motorTiles
	camTY      = 8
	mapW       = camTX + camBX*period + 2
	mapH       = lonBY*period + 2
	worldW     = mapW * tile
	worldH     = mapH * tile

	// motorway (M11) rows: 22 hard shoulder, 23-24 eastbound, 25-26 westbound, 27 hard shoulder
	mwRow        = 24
	mwFull0      = lonW + 6
	mwFull1      = camTX - 7
	barrierY     = float64(25 * tile)
	pxPerMile    = 1600.0
	dt           = 1.0 / 60.0
	chunkSz      = 1024
	mphK         = 0.13 // px/s -> mph
	laneOff      = 30.0
	motorLaneOff = 94.0
)

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
	TMotor
	TShoulder
	TField
	THedge
)

func (k TileKind) solid() bool {
	return k == TBuilding || k == TTree || k == TWater || k == TYardBuilding || k == THedge
}

type City struct {
	Name           string
	TX, TY         int
	BX, BY         int
	HNames, VNames []string
	Inner, Ring    float64 // speed limits
	Layout         [][]int
	NodeBase       int
}

func (c *City) nodeID(i, j int) int { return c.NodeBase + j*(c.BX+1) + i }

var cities = []*City{
	{Name: "London", TX: 0, TY: 0, BX: lonBX, BY: lonBY, Inner: 30, Ring: 40,
		HNames: []string{"Northern Bypass", "Station Road", "High Street", "Church Lane", "Victoria Road", "Mill Lane", "London Road", "Southern Bypass"},
		VNames: []string{"Western Bypass", "Park Avenue", "King's Road", "Bridge Street", "Albert Road", "Market Street", "Green Lane", "Manor Road", "York Road", "Chapel Street", "Eastern Bypass"}},
	{Name: "Cambridge", TX: camTX, TY: camTY, BX: camBX, BY: camBY, Inner: 20, Ring: 40,
		HNames: []string{"Huntingdon Road", "Chesterton Road", "Newmarket Road", "Mill Road", "Hills Road"},
		VNames: []string{"Madingley Road", "Barton Road", "Trumpington Street", "King's Parade", "Regent Street", "Hills Avenue", "Cherry Hinton Road"}},
}

type Place struct {
	Name      string
	Cargo     string
	DestNames []string
	Dests     []int
	Color     color.RGBA
	Floor     color.RGBA
	Dealer    bool
	Fuel      float64
	Charge    float64 // electricity price per kWh (charging points)
	City      int
	Custom    bool
	TX, TY    int // tile origin for custom places
	BX, BY    int
}

func (p *Place) origin() (float64, float64) {
	if p.Custom {
		return float64(p.TX * tile), float64(p.TY * tile)
	}
	c := cities[p.City]
	return float64((c.TX + p.BX*period + 2) * tile), float64((c.TY + p.BY*period + 2) * tile)
}

func (p *Place) bay() (x, y, w, h float64) {
	ox, oy := p.origin()
	return ox + 1.5*tile, oy + 3*tile, 3 * tile, 2 * tile
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
		DestNames: append([]string{"McDougal's", "Teaco Express"}, shops...)},
	{Name: "Rocky Top Quarry", Cargo: "Gravel", Color: rgb(170, 170, 170), Floor: rgb(135, 128, 118),
		DestNames: []string{"Building Site", "B&Queue DIY"}},
	{Name: "Brewdawg Brewery", Cargo: "Beer Kegs", Color: rgb(230, 160, 40), Floor: rgb(120, 115, 110),
		DestNames: append([]string{"The Red Lion Pub", "The Eagle Pub"}, shops...)},
	{Name: "Amazin' Warehouse", Cargo: "Parcels", Color: rgb(215, 175, 120), Floor: rgb(125, 125, 130),
		DestNames: []string{"Argoz", "Royal Male Depot", "Teaco Superstore", "Screwfixx", "Grand Arcayde"}},
	{Name: "Port of Bristow", Cargo: "Containers", Color: rgb(60, 190, 200), Floor: rgb(110, 115, 125),
		DestNames: []string{"Amazin' Warehouse", "Argoz", "Steelworks", "Cambridge Science Park"}},
	{Name: "Steelworks", Cargo: "Machinery", Color: rgb(90, 140, 220), Floor: rgb(115, 118, 125),
		DestNames: []string{"Building Site", "Port of Bristow", "Screwfixx"}},
	{Name: "Greggz Bakery", Cargo: "Sausage Rolls", Color: rgb(40, 90, 170), Floor: rgb(128, 128, 132),
		DestNames: []string{"Coasta Coffee", "Teaco Superstore", "Liddle", "Azda", "Addenbrookies Hospital"}},
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
	{Name: "Welcome Brake Services", Custom: true, TX: lonW + 110, TY: mwRow - 8, Color: rgb(200, 30, 60), Floor: rgb(118, 120, 126), Fuel: 1.69, Charge: 0.79},
	{Name: "Roadchief Services", Custom: true, TX: lonW + 140, TY: mwRow + 4, Color: rgb(230, 120, 0), Floor: rgb(118, 120, 126), Fuel: 1.65, Charge: 0.75},
}

type Rect struct {
	X, Y, W, H float64
	C          color.RGBA
	House      bool
}

type Camera struct {
	a, b  int
	x, y  float64
	horiz bool
	cool  float64
}

type Node struct {
	X, Y     float64
	Nb       []int
	City     int
	I, J     int
	LightOff float64
}

const (
	bkBuildings = -1
	bkPark      = -2
	bkHouses    = -3
)

func (g *Game) isMotor(a, b int) bool {
	return (a == g.motorA && b == g.motorB) || (a == g.motorB && b == g.motorA)
}

func (g *Game) edgeLen(a, b int) float64 {
	return math.Hypot(g.nodes[b].X-g.nodes[a].X, g.nodes[b].Y-g.nodes[a].Y) - 2*tile
}

func (g *Game) edgeDir(a, b int) (float64, float64) {
	dx, dy := g.nodes[b].X-g.nodes[a].X, g.nodes[b].Y-g.nodes[a].Y
	l := math.Hypot(dx, dy)
	return dx / l, dy / l
}

func (g *Game) edgeLimitMph(a, b int) float64 {
	if g.isMotor(a, b) {
		return 70
	}
	na, nb := g.nodes[a], g.nodes[b]
	c := cities[na.City]
	if na.J == nb.J && (na.J == 0 || na.J == c.BY) {
		return c.Ring
	}
	if na.I == nb.I && (na.I == 0 || na.I == c.BX) {
		return c.Ring
	}
	return c.Inner
}

func (g *Game) edgeName(a, b int) string {
	if g.isMotor(a, b) {
		return "M11 motorway"
	}
	na, nb := g.nodes[a], g.nodes[b]
	c := cities[na.City]
	if na.J == nb.J {
		return c.HNames[na.J]
	}
	return c.VNames[na.I]
}

func motorFull(tx int) bool { return tx >= mwFull0 && tx <= mwFull1 }

func (g *Game) generate() {
	r := rand.New(rand.NewSource(11))
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
	for x := 0; x < mapW; x++ {
		for y := 0; y < mapH; y++ {
			g.tiles[x][y] = TField
			g.tileCity[x][y] = -1
		}
	}
	stations = nil
	for _, p := range places {
		if p.Fuel > 0 || p.Charge > 0 {
			stations = append(stations, Station{p: p, price: p.Fuel})
		}
	}

	// ---- cities ----
	palette := []color.RGBA{rgb(170, 95, 85), rgb(125, 125, 145), rgb(190, 170, 125), rgb(95, 115, 135),
		rgb(160, 140, 170), rgb(115, 145, 115), rgb(200, 125, 85), rgb(150, 150, 150)}
	roofs := []color.RGBA{rgb(150, 70, 55), rgb(110, 60, 50), rgb(90, 90, 100), rgb(170, 90, 60), rgb(70, 70, 80)}
	for ci, c := range cities {
		c.Layout = make([][]int, c.BX)
		for i := range c.Layout {
			c.Layout[i] = make([]int, c.BY)
		}
		for lx := 0; lx < c.BX*period+2; lx++ {
			for ly := 0; ly < c.BY*period+2; ly++ {
				g.tiles[c.TX+lx][c.TY+ly] = TRoad
				g.tileCity[c.TX+lx][c.TY+ly] = int8(ci)
			}
		}
		var pos [][2]int
		for bj := 0; bj < c.BY; bj++ {
			for bi := 0; bi < c.BX; bi++ {
				pos = append(pos, [2]int{bi, bj})
			}
		}
		r.Shuffle(len(pos), func(a, b int) { pos[a], pos[b] = pos[b], pos[a] })
		k := 0
		for idx, p := range places {
			if p.City != ci || p.Custom {
				continue
			}
			p.BX, p.BY = pos[k][0], pos[k][1]
			c.Layout[p.BX][p.BY] = idx
			k++
		}
		nParks, nHouses := 12, 14
		if ci == 1 {
			nParks, nHouses = 5, 6
		}
		for n := 0; n < nParks; n++ {
			c.Layout[pos[k][0]][pos[k][1]] = bkPark
			k++
		}
		for n := 0; n < nHouses; n++ {
			c.Layout[pos[k][0]][pos[k][1]] = bkHouses
			k++
		}
		for ; k < len(pos); k++ {
			c.Layout[pos[k][0]][pos[k][1]] = bkBuildings
		}
		parkN := 0
		for bj := 0; bj < c.BY; bj++ {
			for bi := 0; bi < c.BX; bi++ {
				ox, oy := c.TX+bi*period+2, c.TY+bj*period+2
				set := func(rx, ry int, k TileKind) { g.tiles[ox+rx][oy+ry] = k }
				kind := c.Layout[bi][bj]
				ring := func(inner TileKind) {
					for rx := 0; rx < 6; rx++ {
						for ry := 0; ry < 6; ry++ {
							if rx == 0 || ry == 0 || rx == 5 || ry == 5 {
								set(rx, ry, TSidewalk)
							} else {
								set(rx, ry, inner)
							}
						}
					}
				}
				switch {
				case kind >= 0:
					g.makeYard(places[kind], kind)
				case kind == bkPark:
					for rx := 0; rx < 6; rx++ {
						for ry := 0; ry < 6; ry++ {
							set(rx, ry, TGrass)
						}
					}
					if parkN%2 == 0 {
						set(2, 2, TWater)
						set(3, 2, TWater)
						set(2, 3, TWater)
						set(3, 3, TWater)
					}
					for rx := 1; rx <= 4; rx++ {
						for ry := 1; ry <= 4; ry++ {
							if g.tiles[ox+rx][oy+ry] == TGrass && r.Float64() < 0.3 {
								set(rx, ry, TTree)
							}
						}
					}
					parkN++
				case kind == bkHouses:
					ring(TBuilding)
					x0, y0 := float64((ox+1)*tile), float64((oy+1)*tile)
					for row := 0; row < 2; row++ {
						hy := y0 + 4
						if row == 1 {
							hy = y0 + 256 - 4 - 70
						}
						col := roofs[r.Intn(len(roofs))]
						for h := 0; h < 4; h++ {
							if r.Float64() < 0.25 {
								col = roofs[r.Intn(len(roofs))]
							}
							g.buildings = append(g.buildings, Rect{X: x0 + 2 + float64(h*64), Y: hy, W: 60, H: 70, C: col, House: true})
						}
					}
				default:
					ring(TBuilding)
					var rects [][4]int
					switch r.Intn(5) {
					case 0:
						rects = [][4]int{{0, 0, 4, 4}}
					case 1:
						rects = [][4]int{{0, 0, 2, 4}, {2, 0, 2, 4}}
					case 2:
						rects = [][4]int{{0, 0, 4, 2}, {0, 2, 4, 2}}
					case 3:
						rects = [][4]int{{0, 0, 2, 2}, {2, 0, 2, 2}, {0, 2, 2, 2}, {2, 2, 2, 2}}
					default:
						rects = [][4]int{{0, 0, 4, 2}, {0, 2, 2, 2}, {2, 2, 2, 2}}
					}
					bc := palette[r.Intn(len(palette))]
					if ci == 1 && r.Float64() < 0.6 { // Cambridge stone colleges
						bc = []color.RGBA{rgb(200, 185, 150), rgb(185, 170, 135), rgb(170, 160, 140)}[r.Intn(3)]
					}
					for _, rc := range rects {
						g.buildings = append(g.buildings, Rect{
							X: float64((ox+1+rc[0])*tile) + 5, Y: float64((oy+1+rc[1])*tile) + 5,
							W: float64(rc[2]*tile) - 10, H: float64(rc[3]*tile) - 10, C: bc,
						})
						if r.Float64() < 0.5 {
							bc = palette[r.Intn(len(palette))]
						}
					}
				}
			}
		}
	}

	// ---- M11 motorway ----
	for x := lonW; x < camTX; x++ {
		for y := mwRow - 4; y <= mwRow+5; y++ {
			g.tiles[x][y] = TGrass
		}
		if motorFull(x) {
			g.tiles[x][mwRow-4] = THedge
			g.tiles[x][mwRow+5] = THedge
			g.tiles[x][mwRow-2] = TShoulder
			g.tiles[x][mwRow+3] = TShoulder
			for y := mwRow - 1; y <= mwRow+2; y++ {
				g.tiles[x][y] = TMotor
			}
		} else {
			g.tiles[x][mwRow-2] = THedge
			g.tiles[x][mwRow+3] = THedge
			g.tiles[x][mwRow] = TMotor
			g.tiles[x][mwRow+1] = TMotor
		}
	}
	for idx, p := range places {
		if !p.Custom {
			continue
		}
		g.makeYard(p, idx)
		// open the verge between services and the hard shoulder
		if p.TY < mwRow {
			for x := p.TX; x < p.TX+6; x++ {
				for y := p.TY + 6; y < mwRow-2; y++ {
					g.tiles[x][y] = TYard
					g.yardOf[x][y] = int16(idx + 1)
				}
			}
		} else {
			for x := p.TX; x < p.TX+6; x++ {
				for y := mwRow + 4; y < p.TY; y++ {
					g.tiles[x][y] = TYard
					g.yardOf[x][y] = int16(idx + 1)
				}
			}
		}
	}
	// hedgerows between farm fields for scenery
	for x := 0; x < mapW; x++ {
		for y := 0; y < mapH; y++ {
			if g.tiles[x][y] == TField && (x%14 == 0 || y%11 == 0) && r.Float64() < 0.85 {
				g.tiles[x][y] = THedge
			}
		}
	}

	// ---- road graph ----
	g.nodes = nil
	for ci, c := range cities {
		c.NodeBase = len(g.nodes)
		for j := 0; j <= c.BY; j++ {
			for i := 0; i <= c.BX; i++ {
				g.nodes = append(g.nodes, Node{
					X: float64((c.TX + i*period + 1) * tile), Y: float64((c.TY + j*period + 1) * tile),
					City: ci, I: i, J: j, LightOff: r.Float64() * 22,
				})
			}
		}
		for j := 0; j <= c.BY; j++ {
			for i := 0; i <= c.BX; i++ {
				id := c.nodeID(i, j)
				for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					ni, nj := i+d[0], j+d[1]
					if ni >= 0 && nj >= 0 && ni <= c.BX && nj <= c.BY {
						g.nodes[id].Nb = append(g.nodes[id].Nb, c.nodeID(ni, nj))
					}
				}
			}
		}
	}
	g.motorA = cities[0].nodeID(lonBX, 3)
	g.motorB = cities[1].nodeID(0, 2)
	g.nodes[g.motorA].Nb = append(g.nodes[g.motorA].Nb, g.motorB)
	g.nodes[g.motorB].Nb = append(g.nodes[g.motorB].Nb, g.motorA)

	// ---- speed cameras on random city edges ----
	g.cameras = nil
	used := map[[2]int]bool{}
	for len(g.cameras) < 19 {
		a := r.Intn(len(g.nodes))
		b := g.nodes[a].Nb[r.Intn(len(g.nodes[a].Nb))]
		if g.isMotor(a, b) {
			continue
		}
		if len(g.cameras) < 14 && g.nodes[a].City != 0 {
			continue
		}
		if len(g.cameras) >= 14 && g.nodes[a].City == 0 {
			continue
		}
		if used[[2]int{a, b}] || used[[2]int{b, a}] {
			continue
		}
		used[[2]int{a, b}] = true
		na, nb := g.nodes[a], g.nodes[b]
		g.cameras = append(g.cameras, &Camera{a: a, b: b, x: (na.X + nb.X) / 2, y: (na.Y + nb.Y) / 2, horiz: na.J == nb.J})
	}
}

func (g *Game) makeYard(p *Place, idx int) {
	ox, oy := p.origin()
	tx, ty := int(ox/tile), int(oy/tile)
	for rx := 0; rx < 6; rx++ {
		for ry := 0; ry < 6; ry++ {
			k := TYard
			if rx >= 1 && rx <= 4 && ry <= 1 {
				k = TYardBuilding
			}
			g.tiles[tx+rx][ty+ry] = k
			g.yardOf[tx+rx][ty+ry] = int16(idx + 1)
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
	if motorFull(int(x/tile)) && math.Abs(y-barrierY) < 7 {
		return true
	}
	for _, in := range g.incidents {
		x0, y0, x1, y1 := in.rect()
		if x > x0 && x < x1 && y > y0 && y < y1 {
			return true
		}
	}
	return false
}

// cityLocal returns the city and city-local tile coords for a tile, or nil.
func (g *Game) cityLocal(tx, ty int) (*City, int, int) {
	if tx < 0 || ty < 0 || tx >= mapW || ty >= mapH {
		return nil, 0, 0
	}
	ci := g.tileCity[tx][ty]
	if ci < 0 {
		return nil, 0, 0
	}
	c := cities[ci]
	return c, tx - c.TX, ty - c.TY
}

// roadAt returns the town, street name and speed limit at a world position.
func (g *Game) roadAt(x, y float64) (string, string, float64) {
	tx, ty := int(x/tile), int(y/tile)
	if tx < 0 || ty < 0 || tx >= mapW || ty >= mapH {
		return "", "", 30
	}
	k := g.tiles[tx][ty]
	if k == TMotor || k == TShoulder {
		if k == TShoulder {
			return "M11", "M11 (hard shoulder)", 70
		}
		return "M11", "M11 motorway", 70
	}
	c, lx, ly := g.cityLocal(tx, ty)
	if c == nil {
		return "Countryside", "", 30
	}
	inV, inH := lx%period < 2, ly%period < 2
	i, j := lx/period, ly/period
	switch {
	case inH && inV:
		lim := c.Inner
		if i == 0 || i == c.BX || j == 0 || j == c.BY {
			lim = c.Ring
		}
		return c.Name, c.HNames[j] + " / " + c.VNames[i], lim
	case inH:
		lim := c.Inner
		if j == 0 || j == c.BY {
			lim = c.Ring
		}
		return c.Name, c.HNames[j], lim
	case inV:
		lim := c.Inner
		if i == 0 || i == c.BX {
			lim = c.Ring
		}
		return c.Name, c.VNames[i], lim
	}
	return c.Name, "", c.Inner
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
	g.bms = 600.0 / worldH
	g.bigMap = ebiten.NewImage(int(worldW*g.bms)+1, int(worldH*g.bms)+1)
	tmp := ebiten.NewImage(chunkSz, chunkSz)
	for cx := 0; cx*chunkSz < worldW; cx++ {
		for cy := 0; cy*chunkSz < worldH; cy++ {
			tmp.Clear()
			g.drawChunk(tmp, float64(cx*chunkSz), float64(cy*chunkSz))
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(float64(cx*chunkSz), float64(cy*chunkSz))
			op.GeoM.Scale(g.bms, g.bms)
			op.Filter = ebiten.FilterLinear
			g.bigMap.DrawImage(tmp, op)
		}
	}
	tmp.Deallocate()
	g.mini = ebiten.NewImage(280, 190)
}

var fieldCols = []color.RGBA{rgb(110, 150, 60), rgb(200, 190, 70), rgb(140, 110, 70), rgb(90, 145, 70), rgb(160, 170, 80)}

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
	clampI := func(v, hi int) int {
		if v < 0 {
			return 0
		}
		if v > hi {
			return hi
		}
		return v
	}
	tx0, ty0, tx1, ty1 = clampI(tx0, mapW), clampI(ty0, mapH), clampI(tx1, mapW), clampI(ty1, mapH)
	white := rgb(225, 225, 225)
	yellow := rgb(235, 200, 60)
	asphalt := rgb(58, 58, 64)

	for x := tx0; x < tx1; x++ {
		for y := ty0; y < ty1; y++ {
			r := rand.New(rand.NewSource(int64(x*7919 + y*104729)))
			px, py := float64(x*tile), float64(y*tile)
			c, lx, ly := g.cityLocal(x, y)
			switch g.tiles[x][y] {
			case TField:
				fc := fieldCols[(x/14*7+y/11*3)%len(fieldCols)]
				R(px, py, tile, tile, fc)
				dark := color.RGBA{fc.R * 7 / 8, fc.G * 7 / 8, fc.B * 7 / 8, 255}
				for k := 0.0; k < tile; k += 16 {
					if (x/14+y/11)%2 == 0 {
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
			case TSidewalk:
				R(px, py, tile, tile, rgb(150, 150, 148))
				R(px, py, tile, 1, rgb(135, 135, 133))
				R(px, py, 1, tile, rgb(135, 135, 133))
				R(px+32, py, 1, tile, rgb(138, 138, 136))
				R(px, py+32, tile, 1, rgb(138, 138, 136))
			case TBuilding:
				if c != nil && c.Layout[(lx-2)/period][(ly-2)/period] == bkHouses {
					R(px, py, tile, tile, rgb(90, 150, 70))
					if ly%period == 4 {
						R(px, py+30, tile, 3, rgb(120, 95, 70))
					}
					if lx%period != 2 {
						R(px, py+8, 2, tile-16, rgb(120, 95, 70))
					}
				} else {
					R(px, py, tile, tile, rgb(80, 80, 82))
				}
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
			case TShoulder:
				R(px, py, tile, tile, rgb(66, 66, 72))
				if y < mwRow {
					R(px, py+tile-6, tile, 4, white)
				} else {
					R(px, py+2, tile, 4, white)
				}
			case TMotor:
				R(px, py, tile, tile, asphalt)
				if motorFull(x) {
					if y == mwRow-1 || y == mwRow+1 {
						R(px+4, py+tile-2, 34, 4, white)
					}
					if y == mwRow {
						R(px, py+tile-6, tile, 12, rgb(150, 150, 155))
						R(px, py+tile-2, tile, 4, rgb(90, 90, 95))
						R(px, py+tile-10, tile, 3, rgb(240, 240, 240))
						R(px, py+tile+7, tile, 3, rgb(240, 240, 240))
						if x%3 == 0 {
							R(px+30, py+tile-7, 4, 14, rgb(70, 70, 75))
						}
					}
				} else if y == mwRow {
					R(px+8, py+tile-2, 18, 4, yellow)
					R(px+40, py+tile-2, 18, 4, yellow)
				}
			case TRoad:
				R(px, py, tile, tile, asphalt)
				if c == nil {
					break
				}
				horiz, vert := ly%period < 2, lx%period < 2
				if horiz && !vert && ly%period == 1 {
					R(px+8, py-2, 18, 4, yellow)
					R(px+40, py-2, 18, 4, yellow)
				}
				if vert && !horiz && lx%period == 1 {
					R(px-2, py+8, 4, 18, yellow)
					R(px-2, py+40, 4, 18, yellow)
				}
				if horiz && !vert {
					xm := lx % period
					if ly%period == 0 && (xm == 2 || xm == 7) {
						sx := px + 6
						if xm == 7 {
							sx = px + tile - 18
						}
						for k := 0; k < 8; k++ {
							R(sx, py+float64(k*16)+3, 12, 10, white)
						}
					}
					if xm == 7 && ly%period == 0 {
						R(px+tile-28, py+2, 4, tile-2, white)
					}
					if xm == 2 && ly%period == 1 {
						R(px+24, py, 4, tile-2, white)
					}
				}
				if vert && !horiz {
					ym := ly % period
					if lx%period == 0 && (ym == 2 || ym == 7) {
						sy := py + 6
						if ym == 7 {
							sy = py + tile - 18
						}
						for k := 0; k < 8; k++ {
							R(px+float64(k*16)+3, sy, 10, 12, white)
						}
					}
					if ym == 7 && lx%period == 1 {
						R(px, py+tile-28, tile-2, 4, white)
					}
					if ym == 2 && lx%period == 0 {
						R(px+2, py+24, tile-2, 4, white)
					}
				}
			}
		}
	}
	// motorway approach roads meet the city junctions: crossings + stop lines
	for _, ex := range []int{lonW, camTX - 1} {
		px := float64(ex * tile)
		if !in(px, float64(mwRow*tile), 200) {
			continue
		}
		if ex == lonW {
			for k := 0; k < 8; k++ {
				R(px+6, float64(mwRow*tile)+float64(k*16)+3, 12, 10, white)
			}
			R(px+24, float64((mwRow+1)*tile), 4, tile-2, white)
		} else {
			for k := 0; k < 8; k++ {
				R(px+tile-18, float64(mwRow*tile)+float64(k*16)+3, 12, 10, white)
			}
			R(px+tile-28, float64(mwRow*tile)+2, 4, tile-2, white)
		}
	}
	// trees
	for x := tx0; x < tx1; x++ {
		for y := ty0; y < ty1; y++ {
			if g.tiles[x][y] == TTree {
				r := rand.New(rand.NewSource(int64(x*31 + y*17)))
				cx, cy := float64(x*tile+32)+r.Float64()*8-4, float64(y*tile+32)+r.Float64()*8-4
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
		r := rand.New(rand.NewSource(int64(bi)))
		R(b.X+6, b.Y+6, b.W, b.H, color.RGBA{0, 0, 0, 90})
		if b.House {
			dark := color.RGBA{b.C.R * 3 / 4, b.C.G * 3 / 4, b.C.B * 3 / 4, 255}
			R(b.X, b.Y, b.W, b.H/2, b.C)
			R(b.X, b.Y+b.H/2, b.W, b.H/2, dark)
			R(b.X, b.Y+b.H/2-1, b.W, 2, color.RGBA{0, 0, 0, 80})
			R(b.X+b.W-16, b.Y+8, 8, 10, rgb(120, 70, 60))
			continue
		}
		R(b.X, b.Y, b.W, b.H, b.C)
		dark := color.RGBA{b.C.R * 4 / 5, b.C.G * 4 / 5, b.C.B * 4 / 5, 255}
		R(b.X+8, b.Y+8, b.W-16, b.H-16, dark)
		R(b.X+12, b.Y+12, b.W-24, b.H-24, b.C)
		for i := 0; i < 2; i++ {
			ax, ay := b.X+20+r.Float64()*(b.W-60), b.Y+20+r.Float64()*(b.H-60)
			R(ax, ay, 18, 14, rgb(185, 185, 190))
			R(ax+4, ay+3, 10, 8, rgb(120, 120, 125))
		}
	}
	// yards
	for _, p := range places {
		pox, poy := p.origin()
		if !in(pox, poy, 6*tile+20) {
			continue
		}
		bx, by := pox+1*tile+4, poy+4
		bw, bh := 4.0*tile-8, 2.0*tile-8
		R(bx+8, by+8, bw, bh, color.RGBA{0, 0, 0, 90})
		R(bx, by, bw, bh, rgb(90, 90, 95))
		for i := 0.0; i < bw; i += 16 {
			R(bx+i, by, 8, bh, rgb(100, 100, 106))
		}
		R(bx, by+bh-22, bw, 22, p.Color)
		if p.Dealer {
			for i := 0.0; i < bw; i += 20 {
				R(bx+i, by+bh-22, 10, 22, rgb(220, 30, 30))
			}
			R(bx, by, bw, bh-22, rgb(40, 60, 110))
		}
		T(p.Name, faceMed, bx+bw/2, by+bh/2-26, color.White)
		sub := ""
		switch {
		case p.Cargo != "":
			sub = "Collect: " + p.Cargo
		case p.Dealer:
			sub = "Stop in the bay to shop"
		case p.Fuel > 0 && p.Charge > 0:
			sub = fmt.Sprintf("Diesel %.0fp/L  ·  EV %.0fp/kWh", p.Fuel*100, p.Charge*100)
		case p.Fuel > 0:
			sub = fmt.Sprintf("Diesel %.0fp / litre", p.Fuel*100)
		case p.Charge > 0:
			sub = fmt.Sprintf("EV charging %.0fp / kWh", p.Charge*100)
		}
		if sub != "" {
			T(sub, faceSmall, bx+bw/2, by+bh/2+2, color.RGBA{230, 230, 230, 255})
		}
		for k := 0; k < 4; k++ {
			R(pox+8, poy+2.5*tile+float64(k)*40, 60, 3, color.NRGBA{230, 230, 230, 160})
			R(pox+6*tile-68, poy+2.5*tile+float64(k)*40, 60, 3, color.NRGBA{230, 230, 230, 160})
		}
		x, y, w, h := p.bay()
		c := p.Color
		if p.Dealer {
			c = rgb(255, 220, 60)
		}
		label := "LOADING BAY"
		if p.Dealer {
			label = "SHOWROOM"
		}
		if p.Fuel > 0 || p.Charge > 0 {
			label = "PUMPS"
			if p.Fuel == 0 {
				label = "CHARGERS"
			} else if p.Charge > 0 {
				label = "PUMPS + CHARGERS"
			}
			for k := 0; k < 3; k++ {
				px := x + 30 + float64(k)*66
				col := p.Color
				if p.Charge > 0 && (p.Fuel == 0 || k == 2) {
					col = rgb(0, 200, 180)
				}
				R(px-8, y-14, 16, 12, rgb(40, 40, 40))
				R(px-6, y-12, 12, 8, col)
			}
		}
		for i := 0.0; i < w; i += 24 {
			R(x+i, y, 12, 4, c)
			R(x+i, y+h-4, 12, 4, c)
		}
		for i := 0.0; i < h; i += 24 {
			R(x, y+i, 4, 12, c)
			R(x+w-4, y+i, 4, 12, c)
		}
		T(label, faceSmall, x+w/2, y+h/2-10, color.NRGBA{255, 255, 255, 110})
	}
	// speed limit signs just after each junction, on the left
	for a, n := range g.nodes {
		if !in(n.X, n.Y, 300) {
			continue
		}
		for _, b := range n.Nb {
			dx, dy := g.edgeDir(a, b)
			lx, ly := dy, -dx
			sx, sy := n.X+dx*(64+40)+lx*(64+16), n.Y+dy*(64+40)+ly*(64+16)
			if g.isMotor(a, b) {
				R(sx-26, sy-16, 52, 32, rgb(255, 255, 255))
				R(sx-24, sy-14, 48, 28, rgb(20, 70, 170))
				T("M11", faceSign, sx, sy-8, color.White)
				continue
			}
			C(sx+2, sy+2, 14, color.RGBA{0, 0, 0, 90})
			C(sx, sy, 14, rgb(210, 20, 20))
			C(sx, sy, 10.5, color.White)
			T(fmt.Sprintf("%d", int(g.edgeLimitMph(a, b))), faceSign, sx, sy-8, rgb(10, 10, 10))
		}
	}
	// motorway gantry signs
	for x := mwFull0 + 4; x < mwFull1-4; x += 30 {
		px := float64(x * tile)
		if !in(px, barrierY, 300) {
			continue
		}
		gy := float64((mwRow - 2) * tile)
		R(px-4, gy-6, 8, 6*tile+12, rgb(80, 80, 85))
		R(px+8, gy-2, 150, 50, rgb(20, 70, 170))
		T("M11  Cambridge", faceSign, px+83, gy+6, color.White)
		T(fmt.Sprintf("%.0f miles", (float64(camTX*tile)-px)/pxPerMile), faceSign, px+83, gy+24, color.White)
		wy := float64((mwRow+4)*tile) - 50
		R(px-158, wy-2, 150, 50, rgb(20, 70, 170))
		T("M11  London", faceSign, px-83, wy+6, color.White)
		T(fmt.Sprintf("%.0f miles", (px-float64(lonW*tile))/pxPerMile), faceSign, px-83, wy+24, color.White)
	}
	for _, p := range places {
		if !p.Custom {
			continue
		}
		pox, _ := p.origin()
		for _, back := range []float64{1600, 800} {
			var sx, sy float64
			if p.TY < mwRow {
				sx, sy = pox-back, float64((mwRow-3)*tile)+8
			} else {
				sx, sy = pox+6*tile+back, float64((mwRow+4)*tile)+8
			}
			if in(sx, sy, 200) {
				R(sx-90, sy-4, 180, 48, rgb(20, 70, 170))
				T("Services: "+p.Name[:len(p.Name)-9], faceSign, sx, sy, color.White)
				T(fmt.Sprintf("%.1f miles", (back+tile)/pxPerMile), faceSign, sx, sy+18, color.White)
			}
		}
	}
	// town welcome signs
	for _, ws := range []struct {
		x, y float64
		s    string
	}{
		{float64((lonW + 12) * tile), float64((mwRow-3)*tile) + 12, "Thank you for visiting London"},
		{float64((lonW + 12) * tile), float64((mwRow+4)*tile) + 12, "Welcome to London"},
		{float64((camTX - 12) * tile), float64((mwRow-3)*tile) + 12, "Welcome to Cambridge"},
		{float64((camTX - 12) * tile), float64((mwRow+4)*tile) + 12, "Thank you for visiting Cambridge"},
	} {
		if in(ws.x, ws.y, 300) {
			R(ws.x-130, ws.y-4, 260, 30, rgb(250, 250, 250))
			R(ws.x-128, ws.y-2, 256, 26, rgb(30, 90, 50))
			T(ws.s, faceSign, ws.x, ws.y+2, color.White)
		}
	}
	// speed cameras
	for _, cam := range g.cameras {
		if !in(cam.x, cam.y, 200) {
			continue
		}
		if cam.horiz {
			for k := 0; k < 5; k++ {
				R(cam.x-60+float64(k*24), cam.y-60, 4, 120, color.NRGBA{230, 230, 230, 200})
			}
			R(cam.x-8, cam.y-64-26, 16, 18, rgb(30, 30, 30))
			R(cam.x-7, cam.y-64-25, 14, 16, rgb(250, 200, 0))
			C(cam.x, cam.y-64-17, 4, rgb(20, 20, 20))
		} else {
			for k := 0; k < 5; k++ {
				R(cam.x-60, cam.y-60+float64(k*24), 120, 4, color.NRGBA{230, 230, 230, 200})
			}
			R(cam.x+64+8, cam.y-8, 18, 16, rgb(30, 30, 30))
			R(cam.x+64+9, cam.y-7, 16, 14, rgb(250, 200, 0))
			C(cam.x+64+17, cam.y, 4, rgb(20, 20, 20))
		}
	}
}

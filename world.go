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
	blocksX = 10
	blocksY = 7
	period  = 8
	mapW    = blocksX*period + 2
	mapH    = blocksY*period + 2
	worldW  = mapW * tile
	worldH  = mapH * tile
	nodesX  = blocksX + 1
	nodesY  = blocksY + 1
	dt      = 1.0 / 60.0
	chunkSz = 1024
	mphK    = 0.13 // px/s -> mph
	edgeLen = float64(period*tile - 2*tile)
	laneOff = 30.0
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
)

func (k TileKind) solid() bool {
	return k == TBuilding || k == TTree || k == TWater || k == TYardBuilding
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
	BX, BY    int
}

func (p *Place) origin() (float64, float64) {
	return float64((p.BX*period + 2) * tile), float64((p.BY*period + 2) * tile)
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
	{Name: "Hillside Sawmill", Cargo: "Timber", Color: rgb(190, 130, 70), Floor: rgb(150, 125, 95),
		DestNames: []string{"B&Queue DIY", "Building Site", "Screwfixx"}},
	{Name: "Meadow Farm", Cargo: "Vegetables", Color: rgb(120, 200, 80), Floor: rgb(140, 115, 75),
		DestNames: append([]string{"McDougal's"}, shops...)},
	{Name: "Rocky Top Quarry", Cargo: "Gravel", Color: rgb(170, 170, 170), Floor: rgb(135, 128, 118),
		DestNames: []string{"Building Site", "B&Queue DIY"}},
	{Name: "Brewdawg Brewery", Cargo: "Beer Kegs", Color: rgb(230, 160, 40), Floor: rgb(120, 115, 110),
		DestNames: append([]string{"The Red Lion Pub"}, shops...)},
	{Name: "Amazin' Warehouse", Cargo: "Parcels", Color: rgb(215, 175, 120), Floor: rgb(125, 125, 130),
		DestNames: []string{"Argoz", "Royal Male Depot", "Teaco Superstore", "Screwfixx"}},
	{Name: "Port of Bristow", Cargo: "Containers", Color: rgb(60, 190, 200), Floor: rgb(110, 115, 125),
		DestNames: []string{"Amazin' Warehouse", "Argoz", "Steelworks"}},
	{Name: "Steelworks", Cargo: "Machinery", Color: rgb(90, 140, 220), Floor: rgb(115, 118, 125),
		DestNames: []string{"Building Site", "Port of Bristow", "Screwfixx"}},
	{Name: "Greggz Bakery", Cargo: "Sausage Rolls", Color: rgb(40, 90, 170), Floor: rgb(128, 128, 132),
		DestNames: []string{"Coasta Coffee", "Teaco Superstore", "Liddle", "Azda"}},
	{Name: "Royal Male Depot", Cargo: "Post", Color: rgb(220, 40, 40), Floor: rgb(125, 125, 130),
		DestNames: []string{"Amazin' Warehouse", "Argoz", "Sainsbelly's"}},
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
	{Name: "Big Al's Truck Sales", Color: rgb(255, 255, 255), Floor: rgb(120, 125, 135), Dealer: true},
}

var hNames = []string{"Northern Bypass", "Station Road", "High Street", "Church Lane", "Victoria Road", "Mill Lane", "London Road", "Southern Bypass"}
var vNames = []string{"Western Bypass", "Park Avenue", "King's Road", "Bridge Street", "Albert Road", "Market Street", "Green Lane", "Manor Road", "York Road", "Chapel Street", "Eastern Bypass"}

type Rect struct {
	X, Y, W, H float64
	C          color.RGBA
	House      bool
}

type Camera struct {
	i, j, ni, nj int
	x, y         float64
	horiz        bool
	cool         float64
}

const (
	bkBuildings = -1
	bkPark      = -2
	bkHouses    = -3
)

func nodePos(i, j int) (float64, float64) {
	return float64((i*period + 1) * tile), float64((j*period + 1) * tile)
}

func isRing(i, j, ni, nj int) bool {
	if j == nj {
		return j == 0 || j == blocksY
	}
	return i == 0 || i == blocksX
}

func edgeLimitMph(i, j, ni, nj int) float64 {
	if isRing(i, j, ni, nj) {
		return 40
	}
	return 30
}

func edgeName(i, j, ni, nj int) string {
	if j == nj {
		return hNames[j]
	}
	return vNames[i]
}

func (g *Game) generate() {
	r := rand.New(rand.NewSource(11))
	byName := map[string]int{}
	for i, p := range places {
		byName[p.Name] = i
	}
	for _, p := range places {
		for _, n := range p.DestNames {
			idx, ok := byName[n]
			if !ok {
				panic("unknown place " + n)
			}
			p.Dests = append(p.Dests, idx)
		}
	}
	var pos [][2]int
	for bj := 0; bj < blocksY; bj++ {
		for bi := 0; bi < blocksX; bi++ {
			pos = append(pos, [2]int{bi, bj})
		}
	}
	r.Shuffle(len(pos), func(a, b int) { pos[a], pos[b] = pos[b], pos[a] })
	k := 0
	for idx, p := range places {
		p.BX, p.BY = pos[k][0], pos[k][1]
		g.layout[p.BX][p.BY] = idx
		k++
	}
	for n := 0; n < 12; n++ {
		g.layout[pos[k][0]][pos[k][1]] = bkPark
		k++
	}
	stations = nil
	for _, p := range places {
		if p.Fuel > 0 {
			stations = append(stations, Station{p: p, price: p.Fuel})
		}
	}
	for n := 0; n < 14; n++ {
		g.layout[pos[k][0]][pos[k][1]] = bkHouses
		k++
	}
	for ; k < len(pos); k++ {
		g.layout[pos[k][0]][pos[k][1]] = bkBuildings
	}

	for x := 0; x < mapW; x++ {
		for y := 0; y < mapH; y++ {
			g.tiles[x][y] = TRoad
		}
	}
	palette := []color.RGBA{rgb(170, 95, 85), rgb(125, 125, 145), rgb(190, 170, 125), rgb(95, 115, 135),
		rgb(160, 140, 170), rgb(115, 145, 115), rgb(200, 125, 85), rgb(150, 150, 150)}
	roofs := []color.RGBA{rgb(150, 70, 55), rgb(110, 60, 50), rgb(90, 90, 100), rgb(170, 90, 60), rgb(70, 70, 80)}
	parkN := 0
	for bj := 0; bj < blocksY; bj++ {
		for bi := 0; bi < blocksX; bi++ {
			ox, oy := bi*period+2, bj*period+2
			set := func(rx, ry int, k TileKind) { g.tiles[ox+rx][oy+ry] = k }
			kind := g.layout[bi][bj]
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
				for rx := 0; rx < 6; rx++ {
					for ry := 0; ry < 6; ry++ {
						set(rx, ry, TYard)
					}
				}
				for rx := 1; rx <= 4; rx++ {
					for ry := 0; ry <= 1; ry++ {
						set(rx, ry, TYardBuilding)
					}
				}
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
					c := roofs[r.Intn(len(roofs))]
					for h := 0; h < 4; h++ {
						if r.Float64() < 0.25 {
							c = roofs[r.Intn(len(roofs))]
						}
						g.buildings = append(g.buildings, Rect{X: x0 + 2 + float64(h*64), Y: hy, W: 60, H: 70, C: c, House: true})
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
				for _, rc := range rects {
					g.buildings = append(g.buildings, Rect{
						X: float64((ox+1+rc[0])*tile) + 5, Y: float64((oy+1+rc[1])*tile) + 5,
						W: float64(rc[2]*tile) - 10, H: float64(rc[3]*tile) - 10,
						C: palette[r.Intn(len(palette))],
					})
				}
			}
		}
	}

	// speed cameras on random non-ring edges
	used := map[[4]int]bool{}
	for len(g.cameras) < 14 {
		i, j := r.Intn(nodesX), r.Intn(nodesY)
		ni, nj := i+1, j
		if r.Intn(2) == 0 {
			ni, nj = i, j+1
		}
		if ni >= nodesX || nj >= nodesY {
			continue
		}
		key := [4]int{i, j, ni, nj}
		if used[key] {
			continue
		}
		used[key] = true
		ax, ay := nodePos(i, j)
		bx, by := nodePos(ni, nj)
		g.cameras = append(g.cameras, &Camera{i: i, j: j, ni: ni, nj: nj, x: (ax + bx) / 2, y: (ay + by) / 2, horiz: j == nj})
	}
	for i := range g.lightOff {
		for j := range g.lightOff[i] {
			g.lightOff[i][j] = r.Float64() * 22
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
	for _, in := range g.incidents {
		x0, y0, x1, y1 := in.rect()
		if x > x0 && x < x1 && y > y0 && y < y1 {
			return true
		}
	}
	return false
}

// roadAt returns the street name and speed limit at a world position.
func roadAt(x, y float64) (string, float64) {
	tx, ty := int(x/tile), int(y/tile)
	if tx < 0 || ty < 0 || tx >= mapW || ty >= mapH {
		return "", 30
	}
	inV, inH := tx%period < 2, ty%period < 2
	i, j := tx/period, ty/period
	lim := 30.0
	switch {
	case inH && inV:
		if i == 0 || i == blocksX || j == 0 || j == blocksY {
			lim = 40
		}
		return hNames[j] + " / " + vNames[i], lim
	case inH:
		if j == 0 || j == blocksY {
			lim = 40
		}
		return hNames[j], lim
	case inV:
		if i == 0 || i == blocksX {
			lim = 40
		}
		return vNames[i], lim
	}
	return "", 30
}

// ---------- static world rendering (chunked) ----------

func (g *Game) buildWorld() {
	nx := (worldW + chunkSz - 1) / chunkSz
	ny := (worldH + chunkSz - 1) / chunkSz
	g.chunks = make([][]*ebiten.Image, nx)
	for cx := 0; cx < nx; cx++ {
		g.chunks[cx] = make([]*ebiten.Image, ny)
		for cy := 0; cy < ny; cy++ {
			img := ebiten.NewImage(chunkSz, chunkSz)
			g.drawChunk(img, float64(cx*chunkSz), float64(cy*chunkSz))
			g.chunks[cx][cy] = img
		}
	}
	g.bms = math.Min(1180.0/worldW, 600.0/worldH)
	g.bigMap = ebiten.NewImage(int(worldW*g.bms)+1, int(worldH*g.bms)+1)
	for cx := range g.chunks {
		for cy := range g.chunks[cx] {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(float64(cx*chunkSz), float64(cy*chunkSz))
			op.GeoM.Scale(g.bms, g.bms)
			op.Filter = ebiten.FilterLinear
			g.bigMap.DrawImage(g.chunks[cx][cy], op)
		}
	}
	g.mini = ebiten.NewImage(280, 190)
}

func (g *Game) drawChunk(img *ebiten.Image, ox, oy float64) {
	R := func(x, y, w, h float64, c color.Color) { fillRect(img, x-ox, y-oy, w, h, c) }
	C := func(x, y, r float64, c color.Color) { fillCircle(img, x-ox, y-oy, r, c) }
	T := func(s string, f *text.GoTextFace, x, y float64, c color.Color) {
		drawText(img, s, f, x-ox, y-oy, c, text.AlignCenter)
	}
	img.Fill(rgb(58, 58, 64))
	R(worldW, -100, 9999, 9999, rgb(80, 130, 60))
	R(-100, worldH, 9999, 9999, rgb(80, 130, 60))

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

	for x := tx0; x < tx1; x++ {
		for y := ty0; y < ty1; y++ {
			r := rand.New(rand.NewSource(int64(x*7919 + y*104729)))
			px, py := float64(x*tile), float64(y*tile)
			bx, by := (x-2)/period, (y-2)/period
			switch g.tiles[x][y] {
			case TSidewalk:
				R(px, py, tile, tile, rgb(150, 150, 148))
				R(px, py, tile, 1, rgb(135, 135, 133))
				R(px, py, 1, tile, rgb(135, 135, 133))
				R(px+32, py, 1, tile, rgb(138, 138, 136))
				R(px, py+32, tile, 1, rgb(138, 138, 136))
			case TBuilding:
				if bx >= 0 && by >= 0 && bx < blocksX && by < blocksY && g.layout[bx][by] == bkHouses {
					R(px, py, tile, tile, rgb(90, 150, 70))
					if y%period == 4 {
						R(px, py+30, tile, 3, rgb(120, 95, 70))
					}
					if x%period != 2 {
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
				if bx >= 0 && by >= 0 && bx < blocksX && by < blocksY && g.layout[bx][by] >= 0 {
					fl = places[g.layout[bx][by]].Floor
				}
				R(px, py, tile, tile, fl)
				R(px, py, tile, 1, color.RGBA{0, 0, 0, 25})
				R(px, py, 1, tile, color.RGBA{0, 0, 0, 25})
			case TRoad:
				horiz, vert := y%period < 2, x%period < 2
				if horiz && !vert && y%period == 1 {
					R(px+8, py-2, 18, 4, yellow)
					R(px+40, py-2, 18, 4, yellow)
				}
				if vert && !horiz && x%period == 1 {
					R(px-2, py+8, 4, 18, yellow)
					R(px-2, py+40, 4, 18, yellow)
				}
				if horiz && !vert {
					xm := x % period
					if y%period == 0 && (xm == 2 || xm == 7) {
						sx := px + 6
						if xm == 7 {
							sx = px + tile - 18
						}
						for k := 0; k < 8; k++ {
							R(sx, py+float64(k*16)+3, 12, 10, white)
						}
					}
					// stop lines (drive on the left)
					if xm == 7 && y%period == 0 {
						R(px+tile-28, py+2, 4, tile-2, white)
					}
					if xm == 2 && y%period == 1 {
						R(px+24, py, 4, tile-2, white)
					}
				}
				if vert && !horiz {
					ym := y % period
					if x%period == 0 && (ym == 2 || ym == 7) {
						sy := py + 6
						if ym == 7 {
							sy = py + tile - 18
						}
						for k := 0; k < 8; k++ {
							R(px+float64(k*16)+3, sy, 10, 12, white)
						}
					}
					if ym == 7 && x%period == 1 {
						R(px, py+tile-28, tile-2, 4, white)
					}
					if ym == 2 && x%period == 0 {
						R(px+2, py+24, tile-2, 4, white)
					}
				}
			}
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
		if pox > ox+chunkSz+20 || poy > oy+chunkSz+20 || pox+6*tile < ox-20 || poy+6*tile < oy-20 {
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
		if p.Cargo != "" {
			T("Collect: "+p.Cargo, faceSmall, bx+bw/2, by+bh/2+2, color.RGBA{230, 230, 230, 255})
		} else if p.Dealer {
			T("Stop in the bay to shop", faceSmall, bx+bw/2, by+bh/2+2, color.RGBA{230, 230, 230, 255})
		} else if p.Fuel > 0 {
			T(fmt.Sprintf("Diesel %.0fp / litre", p.Fuel*100), faceSmall, bx+bw/2, by+bh/2+2, color.RGBA{230, 230, 230, 255})
		}
		// parking bays on the side
		for k := 0; k < 4; k++ {
			R(pox+8, poy+2.5*tile+float64(k)*40, 60, 3, color.NRGBA{230, 230, 230, 160})
			R(pox+6*tile-68, poy+2.5*tile+float64(k)*40, 60, 3, color.NRGBA{230, 230, 230, 160})
		}
		x, y, w, h := p.bay()
		c := p.Color
		if p.Dealer {
			c = rgb(255, 220, 60)
		}
		for i := 0.0; i < w; i += 24 {
			R(x+i, y, 12, 4, c)
			R(x+i, y+h-4, 12, 4, c)
		}
		for i := 0.0; i < h; i += 24 {
			R(x, y+i, 4, 12, c)
			R(x+w-4, y+i, 4, 12, c)
		}
		label := "LOADING BAY"
		if p.Dealer {
			label = "SHOWROOM"
		}
		if p.Fuel > 0 {
			label = "PUMPS"
			for k := 0; k < 3; k++ {
				px := x + 30 + float64(k)*66
				R(px-8, y-14, 16, 12, rgb(40, 40, 40))
				R(px-6, y-12, 12, 8, p.Color)
			}
		}
		T(label, faceSmall, x+w/2, y+h/2-10, color.NRGBA{255, 255, 255, 110})
	}
	// speed limit signs just after each junction, on the left
	for i := 0; i < nodesX; i++ {
		for j := 0; j < nodesY; j++ {
			nx, ny := nodePos(i, j)
			if nx < ox-300 || ny < oy-300 || nx > ox+chunkSz+300 || ny > oy+chunkSz+300 {
				continue
			}
			for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				ti, tj := i+d[0], j+d[1]
				if ti < 0 || tj < 0 || ti >= nodesX || tj >= nodesY {
					continue
				}
				dx, dy := float64(d[0]), float64(d[1])
				lx, ly := dy, -dx
				sx, sy := nx+dx*(64+40)+lx*(64+16), ny+dy*(64+40)+ly*(64+16)
				C(sx+2, sy+2, 14, color.RGBA{0, 0, 0, 90})
				C(sx, sy, 14, rgb(210, 20, 20))
				C(sx, sy, 10.5, color.White)
				T(fmt.Sprintf("%d", int(edgeLimitMph(i, j, ti, tj))), faceSign, sx, sy-8, rgb(10, 10, 10))
			}
		}
	}
	// speed cameras
	for _, cam := range g.cameras {
		if cam.x < ox-200 || cam.y < oy-200 || cam.x > ox+chunkSz+200 || cam.y > oy+chunkSz+200 {
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

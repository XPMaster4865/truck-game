package main

import (
	"fmt"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

type TruckModel struct {
	Name     string
	Price    int
	MaxSpeed float64 // px/s
	Accel    float64
	Steer    float64
	Brake    float64
	Protect  float64 // cargo damage multiplier (lower = better)
	Tank     float64 // litres
	Economy  float64 // litres per 1000px
	Color    color.RGBA
	Blurb    string
	Electric bool // Tank = battery kWh, Economy = kWh per 1000px
}

var models = []TruckModel{
	{"Bedfud TK (old banger)", 0, 330, 170, 2.2, 550, 1.3, 250, 7.5, rgb(200, 60, 40), "Rusty, slow and leaks oil. It's a start.", false},
	{"Velvo PO 6", 4000, 380, 215, 2.5, 660, 1.0, 300, 6.8, rgb(40, 90, 170), "Solid Swedish workhorse. Comfy cab.", false},
	{"Sconia D", 9000, 430, 255, 2.75, 760, 0.8, 350, 6.2, rgb(235, 235, 235), "The King of the Road. V8 rumble included.", false},
	{"PUF NGD", 16000, 470, 290, 3.0, 860, 0.6, 400, 5.6, rgb(25, 40, 90), "Dutch precision. Steers like it's on rails.", false},
	{"Marcedes Actrus", 25000, 510, 330, 3.2, 960, 0.45, 450, 5.2, rgb(50, 50, 55), "Top of the range. Air suspension, cargo barely moves.", false},
	{"Velvo PO Electric", 12000, 400, 310, 2.6, 720, 0.85, 300, 9.0, rgb(60, 160, 110), "Silent, instant torque. Needs a charging point.", true},
	{"Teslo Simi", 30000, 520, 400, 3.1, 990, 0.5, 500, 7.5, rgb(235, 235, 240), "Futuristic electric beast. 0-60 before you blink.", true},
}

var paints = []struct {
	Name string
	C    color.RGBA
}{
	{"Factory", color.RGBA{}},
	{"Racing Red", rgb(210, 30, 30)},
	{"Royal Blue", rgb(30, 70, 190)},
	{"British Racing Green", rgb(20, 90, 50)},
	{"Sunshine Yellow", rgb(250, 200, 20)},
	{"Tangerine", rgb(250, 120, 20)},
	{"Arctic White", rgb(240, 240, 240)},
	{"Midnight Black", rgb(30, 30, 35)},
	{"Hot Pink", rgb(240, 80, 160)},
	{"Purple Haze", rgb(110, 50, 170)},
	{"Gunmetal", rgb(90, 95, 105)},
	{"Sky Blue", rgb(100, 180, 240)},
}

const paintPrice = 250

type Truck struct {
	X, Y, Angle, Speed float64
}

func (g *Game) model() *TruckModel { return &models[g.cur] }

func (g *Game) cabColor(m int) color.RGBA {
	if g.paint[m] > 0 {
		return paints[g.paint[m]].C
	}
	return models[m].Color
}

func (g *Game) truckImg() *ebiten.Image {
	src := -1
	if g.phase == 2 {
		src = g.job.Src
	}
	key := [3]int{g.cur, g.paint[g.cur], src}
	if img, ok := g.truckImgs[key]; ok {
		return img
	}
	var cargo *color.RGBA
	if src >= 0 {
		c := places[src].Color
		cargo = &c
	}
	img := makeTruckImg(g.cabColor(g.cur), cargo, g.cur)
	g.truckImgs[key] = img
	return img
}

func makeTruckImg(cab color.RGBA, cargo *color.RGBA, model int) *ebiten.Image {
	img := ebiten.NewImage(90, 36)
	wheel := rgb(25, 25, 25)
	for _, wx := range []float64{8, 22, 68} {
		fillRect(img, wx, 0, 12, 5, wheel)
		fillRect(img, wx, 31, 12, 5, wheel)
	}
	if cargo == nil {
		fillRect(img, 2, 4, 58, 28, rgb(110, 110, 115))
		for i := 6.0; i < 58; i += 10 {
			fillRect(img, i, 5, 2, 26, rgb(90, 90, 95))
		}
		fillRect(img, 2, 4, 58, 2, rgb(80, 80, 85))
		fillRect(img, 2, 30, 58, 2, rgb(80, 80, 85))
	} else {
		c := *cargo
		dark := color.RGBA{c.R / 2, c.G / 2, c.B / 2, 255}
		fillRect(img, 2, 3, 58, 30, dark)
		fillRect(img, 4, 5, 54, 26, c)
		for i := 12.0; i < 58; i += 12 {
			fillRect(img, i, 5, 2, 26, dark)
		}
	}
	light := color.RGBA{uint8(math.Min(255, float64(cab.R)*1.2+20)), uint8(math.Min(255, float64(cab.G)*1.2+20)), uint8(math.Min(255, float64(cab.B)*1.2+20)), 255}
	fillRect(img, 60, 14, 5, 8, rgb(40, 40, 40))
	fillRect(img, 64, 4, 24, 28, cab)
	fillRect(img, 66, 7, 12, 22, light)
	if model >= 2 { // spoiler / light bar on posh trucks
		fillRect(img, 64, 6, 3, 24, rgb(60, 60, 60))
		for y := 8.0; y < 28; y += 5 {
			fillRect(img, 68, y, 3, 3, rgb(255, 230, 140))
		}
	}
	fillRect(img, 80, 7, 5, 22, rgb(150, 210, 240))
	fillRect(img, 87, 5, 3, 5, rgb(255, 240, 150))
	fillRect(img, 87, 26, 3, 5, rgb(255, 240, 150))
	return img
}

func (g *Game) setIndicator(v int) {
	if g.ind == v {
		g.ind = 0
		return
	}
	g.ind = v
	g.indAngle = g.truck.Angle
}

func (g *Game) truckCircles() [3][2]float64 {
	t := g.truck
	cs, sn := math.Cos(t.Angle), math.Sin(t.Angle)
	var out [3][2]float64
	for i, off := range []float64{-28, 0, 28} {
		out[i] = [2]float64{t.X + cs*off, t.Y + sn*off}
	}
	return out
}

func (g *Game) truckHits(x, y, a float64) bool {
	cs, sn := math.Cos(a), math.Sin(a)
	for _, lx := range []float64{-43, -14, 14, 43} {
		for _, ly := range []float64{-16, 16} {
			px := x + cs*lx - sn*ly
			py := y + sn*lx + cs*ly
			if g.solidAt(px, py) {
				return true
			}
		}
	}
	return false
}

func pressed(keys ...ebiten.Key) bool {
	for _, k := range keys {
		if ebiten.IsKeyPressed(k) {
			return true
		}
	}
	return false
}

func (g *Game) crash(impact float64) {
	if g.hitCool > 0 {
		return
	}
	g.hitCool = 0.4
	g.shake = math.Min(14, impact/25)
	if g.phase == 2 && impact > 90 {
		dmg := (impact - 60) / 25 * g.model().Protect
		g.condition = math.Max(0, g.condition-dmg)
		g.flash(fmt.Sprintf("Crash! Cargo damaged (-%.0f%%)", dmg))
	}
}

func (g *Game) updateTruck() {
	t := &g.truck
	m := g.model()
	up := pressed(ebiten.KeyW, ebiten.KeyArrowUp)
	down := pressed(ebiten.KeyS, ebiten.KeyArrowDown)
	left := pressed(ebiten.KeyA, ebiten.KeyArrowLeft)
	right := pressed(ebiten.KeyD, ebiten.KeyArrowRight)
	hand := pressed(ebiten.KeySpace)
	if inpututil.IsKeyJustPressed(ebiten.KeyQ) {
		g.setIndicator(-1)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyE) {
		g.setIndicator(1)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyX) {
		g.setIndicator(2)
	}
	g.hornOn = pressed(ebiten.KeyH)
	if g.ind == -1 || g.ind == 1 {
		turned := math.Abs(math.Remainder(t.Angle-g.indAngle, 2*math.Pi))
		if turned > 0.9 && !left && !right {
			g.ind = 0 // self-cancelling indicator
		}
	}
	if g.fuel[g.cur] <= 0 {
		up, down = false, false
	}

	maxF := m.MaxSpeed
	if k, _ := g.tileAt(t.X, t.Y); (k == TGrass || k == TField) && g.roadQuery(t.X, t.Y, 200).Edge > 0 {
		maxF = 150
	}
	switch {
	case up && t.Speed < 0:
		t.Speed += m.Brake * dt
	case up:
		t.Speed += m.Accel * (1 - 0.5*t.Speed/m.MaxSpeed) * dt
	case down && t.Speed > 0:
		t.Speed -= m.Brake * dt
	case down:
		t.Speed -= 180 * dt
	default:
		t.Speed -= t.Speed * 0.6 * dt
		if math.Abs(t.Speed) < 4 {
			t.Speed = 0
		}
	}
	if hand {
		if t.Speed > 0 {
			t.Speed = math.Max(0, t.Speed-m.Brake*1.3*dt)
		} else {
			t.Speed = math.Min(0, t.Speed+m.Brake*1.3*dt)
		}
	}
	if t.Speed > maxF {
		t.Speed -= (t.Speed - maxF) * 4 * dt
	}
	if t.Speed < -140 {
		t.Speed = -140
	}
	steer := 0.0
	if left {
		steer -= 1
	}
	if right {
		steer += 1
	}
	as := math.Abs(t.Speed)
	rate := m.Steer / (1 + as/450) * math.Min(1, as/90)
	if t.Speed < 0 {
		rate = -rate
	}
	na := t.Angle + steer*rate*dt
	nx := t.X + math.Cos(na)*t.Speed*dt
	ny := t.Y + math.Sin(na)*t.Speed*dt

	ox, oy := t.X, t.Y
	stuck := g.truckHits(t.X, t.Y, t.Angle)
	if stuck || !g.truckHits(nx, ny, na) {
		t.X, t.Y, t.Angle = nx, ny, na
	} else if !g.truckHits(nx, t.Y, na) {
		t.X, t.Angle = nx, na
		t.Speed *= 0.97
	} else if !g.truckHits(t.X, ny, na) {
		t.Y, t.Angle = ny, na
		t.Speed *= 0.97
	} else {
		if !g.truckHits(t.X, t.Y, na) {
			t.Angle = na
		}
		g.crash(as)
		t.Speed = -t.Speed * 0.25
	}

	// collisions with traffic
	tc := g.truckCircles()
	for _, c := range g.cars {
		if c.chase {
			continue
		}
		hit := false
		for _, p := range tc {
			for _, q := range c.circles() {
				if math.Hypot(p[0]-q[0], p[1]-q[1]) < 28 {
					hit = true
				}
			}
		}
		if !hit {
			continue
		}
		fx, fy := math.Cos(t.Angle), math.Sin(t.Angle)
		toward := ((c.x-t.X)*fx+(c.y-t.Y)*fy)*t.Speed > 0
		if toward {
			impact := as
			g.crash(impact + 40)
			px, py := t.X-fx*t.Speed*dt*2, t.Y-fy*t.Speed*dt*2
			if !g.truckHits(px, py, t.Angle) {
				t.X, t.Y = px, py
			}
			t.Speed = -t.Speed * 0.3
			if impact > 70 && c.crashed <= 0 && !c.isEV() {
				c.crashed = 15
				if !g.offence("careless", "Careless driving (collision)", 200, true) {
					g.flash("You hit a car! Drive carefully...")
				}
			}
		}
		c.speed = 0
	}

	// fuel
	moved := math.Hypot(t.X-ox, t.Y-oy)
	burn := moved / 1000 * m.Economy
	if up {
		burn *= 1.3
	}
	burn += 0.02 * dt // idling
	g.fuel[g.cur] = math.Max(0, g.fuel[g.cur]-burn)

	// trail for police
	if len(g.trail) == 0 || math.Hypot(t.X-g.trail[len(g.trail)-1][0], t.Y-g.trail[len(g.trail)-1][1]) > 8 {
		g.trail = append(g.trail, [2]float64{t.X, t.Y})
		if len(g.trail) > 600 {
			cut := len(g.trail) - 600
			g.trail = g.trail[cut:]
			if g.chaser != nil {
				g.chaser.trailIdx = max(0, g.chaser.trailIdx-cut)
			}
		}
	}
}

// ---------- law & order ----------

func (g *Game) issueFine(reason string, amount int) {
	g.money -= amount
	g.finesTotal += amount
	g.fineTitle = reason
	g.fineSub = fmt.Sprintf("Fine: -£%d", amount)
	g.fineTimer = 4.5
}

func (g *Game) policeNear(r float64) bool {
	for _, c := range g.cars {
		if (c.kind == kPolice || c.kind == kPoliceEV) && math.Hypot(c.x-g.truck.X, c.y-g.truck.Y) < r {
			return true
		}
	}
	return false
}

// offence: needPolice means it only counts if a police car sees it (and pulls you over).
func (g *Game) offence(key, reason string, amount int, needPolice bool) bool {
	if g.cool[key] > 0 {
		return false
	}
	if needPolice {
		if g.chaser != nil || !g.policeNear(380) {
			return false
		}
		g.startChase(reason, amount)
		if g.chaser != nil {
			g.cool[key] = 10
		}
		return g.chaser != nil
	}
	g.cool[key] = 8
	g.issueFine(reason, amount)
	return true
}

func (g *Game) updateLaw() {
	for k, v := range g.cool {
		if v > 0 {
			g.cool[k] = v - dt
		}
	}
	t := &g.truck
	town, name, lim := g.roadAt(t.X, t.Y)
	g.town, g.roadName, g.limit = town, name, lim
	mph := math.Abs(t.Speed) * mphK

	// speed cameras
	for _, cam := range g.cameras {
		if cam.cool > 0 {
			cam.cool -= dt
		}
		if math.Hypot(cam.x-t.X, cam.y-t.Y) < 80 && cam.cool <= 0 {
			cl := g.roads[cam.road].Limit
			if mph > cl*1.1+2 {
				cam.cool = 6
				g.issueFine(fmt.Sprintf("SPEED CAMERA: %d mph in a %d zone", int(mph), int(cl)), 100)
			}
		}
	}
	if name != "" && mph > lim*1.1+2 {
		g.offence("speed", fmt.Sprintf("Speeding (%d mph in a %d)", int(mph), int(lim)), 100, true)
	}

	// wrong side of the road (drive on the left!)
	h := g.roadQuery(t.X, t.Y, 300)
	fx, fy := math.Cos(t.Angle), math.Sin(t.Angle)
	if t.Speed < 0 {
		fx, fy = -fx, -fy
	}
	wrong := false
	nearWorks := false
	for _, in := range g.incidents {
		if in.kind == incWorksLight && math.Hypot(in.mx-t.X, in.my-t.Y) < 330 {
			nearWorks = true
		}
	}
	if h.Road >= 0 && h.Edge < 0 && math.Abs(t.Speed) > 50 && !nearWorks {
		rd := g.roads[h.Road]
		inJunction := h.S < g.nodes[rd.A].R+20 || h.S > rd.Len-g.nodes[rd.B].R-20
		_, _, rtx, rty := rd.at(h.S)
		d := fx*rtx + fy*rty
		if !inJunction && math.Abs(d) > 0.6 {
			dir := 1.0
			if d < 0 {
				dir = -1
			}
			if h.Lat*dir < -8 {
				wrong = true
			}
		}
	}
	if wrong {
		g.wrongTimer += dt
		if g.wrongTimer > 2 {
			g.offence("wrong", "Driving on the wrong side of the road", 100, true)
		}
	} else {
		g.wrongTimer = 0
	}
	// pavement / verge / hard shoulder
	k, _ := g.tileAt(t.X, t.Y)
	offRoad := h.Edge > 6 && g.cityAt(t.X, t.Y) >= 0 && (k == TSidewalk || k == TGrass)
	shoulder := h.Road >= 0 && g.roads[h.Road].Class == clMotorway && h.Edge < 0 && math.Abs(h.Lat) > mwRes+3*mwLane+4
	if (offRoad || shoulder) && math.Abs(t.Speed) > 30 {
		g.paveTimer += dt
		if g.paveTimer > 1.5 {
			if shoulder {
				g.offence("pave", "Driving on the hard shoulder", 100, true)
			} else {
				g.offence("pave", "Driving on the pavement / verge", 100, true)
			}
		}
	} else {
		g.paveTimer = 0
	}

	// emergency vehicles behind you
	g.evBehind = false
	for _, c := range g.cars {
		if !c.isEV() {
			continue
		}
		ox, oy := t.X-c.x, t.Y-c.y
		along := ox*c.hx + oy*c.hy
		lat := ox*-c.hy + oy*c.hx
		if along > 0 && along < 320 && math.Abs(lat) < 100 {
			g.evBehind = true
		}
	}
	if g.evBehind && math.Abs(t.Speed) > 110 {
		g.evIgnore += dt
	} else if !g.evBehind {
		g.evIgnore = 0
	}
	if g.evIgnore > 6 || g.evBlockedTime > 5 {
		g.offence("ev", "Failed to give way to an emergency vehicle", 100, false)
		g.evIgnore, g.evBlockedTime = 0, 0
	}
	if !g.evBehind {
		g.evBlockedTime = 0
	}

	// police chase / pull over
	if g.chaser != nil {
		g.chaseTime += dt
		d := math.Hypot(g.chaser.x-t.X, g.chaser.y-t.Y)
		if d < 220 && math.Abs(t.Speed) < 12 {
			g.stopTime += dt
			if g.stopTime > 1.5 {
				g.issueFine("POLICE: "+g.chaseReason, g.chaseFine)
				g.flash("The officer gives you a ticket and a stern look.")
				g.endChase()
			}
		} else {
			g.stopTime = 0
		}
		if g.chaser != nil && g.chaseTime > 30 {
			g.issueFine("POLICE: Failing to stop for police", 1000)
			g.endChase()
		}
	}
}

// ---------- jobs, fuel stations, dealer ----------

func (g *Game) makeOffers() {
	g.offers = nil
	for tries := 0; len(g.offers) < 3 && tries < 200; tries++ {
		src := g.rng.Intn(len(places))
		p := places[src]
		if p.Cargo == "" {
			continue
		}
		dst := p.Dests[g.rng.Intn(len(p.Dests))]
		dup := false
		for _, o := range g.offers {
			if o.Src == src || (o.Dst == dst && o.Src == src) {
				dup = true
			}
		}
		if dup {
			continue
		}
		g.offers = append(g.offers, jobFor(src, dst, p.Cargo))
	}
}

type Job struct {
	Src, Dst  int
	Pay       int
	TimeLimit float64
	Cargo     string
	Custom    bool
}

// jobFor prices a job by how far apart the two places are.
func jobFor(src, dst int, cargo string) Job {
	sx, sy := places[src].bayCenter()
	dx, dy := places[dst].bayCenter()
	dist := math.Abs(sx-dx) + math.Abs(sy-dy)
	pay := int((250+dist*0.33)/10) * 10
	return Job{Src: src, Dst: dst, Pay: pay, TimeLimit: math.Round(dist/120 + 35), Cargo: cargo}
}

var cargoTypes = []string{"Parcels", "Timber", "Vegetables", "Gravel", "Beer Kegs", "Containers", "Machinery",
	"Sausage Rolls", "Post", "Chelsea Buns", "Lab Equipment", "Strawberries", "Cereal", "Ale Barrels", "Furniture", "Pies", "Fuel Drums", "Bricks"}

// jobPlaces is everything you can drive a custom job between (not fuel stations or the dealer).
func jobPlaces() []int {
	var out []int
	for i, p := range places {
		if !p.Dealer && p.Fuel == 0 && p.Charge == 0 {
			out = append(out, i)
		}
	}
	return out
}

func (g *Game) updateJobMaker() {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) || inpututil.IsKeyJustPressed(ebiten.KeyJ) {
		g.state = stPlay
		return
	}
	jp := jobPlaces()
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowLeft) || inpututil.IsKeyJustPressed(ebiten.KeyA) {
		g.jmCol = (g.jmCol + 2) % 3
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowRight) || inpututil.IsKeyJustPressed(ebiten.KeyD) || inpututil.IsKeyJustPressed(ebiten.KeyTab) {
		g.jmCol = (g.jmCol + 1) % 3
	}
	d := 0
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowUp) || inpututil.IsKeyJustPressed(ebiten.KeyW) {
		d = -1
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowDown) || inpututil.IsKeyJustPressed(ebiten.KeyS) {
		d = 1
	}
	switch g.jmCol {
	case 0:
		g.jmFrom = (g.jmFrom + d + len(jp)) % len(jp)
		if p := places[jp[g.jmFrom]]; d != 0 && p.Cargo != "" {
			for i, c := range cargoTypes {
				if c == p.Cargo {
					g.jmCargo = i
				}
			}
		}
	case 1:
		g.jmTo = (g.jmTo + d + len(jp)) % len(jp)
	case 2:
		g.jmCargo = (g.jmCargo + d + len(cargoTypes)) % len(cargoTypes)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
		if jp[g.jmFrom] == jp[g.jmTo] {
			g.jmMsg = "Pick-up and drop-off must be different places."
			return
		}
		j := jobFor(jp[g.jmFrom], jp[g.jmTo], cargoTypes[g.jmCargo])
		j.Pay = j.Pay * 9 / 10 / 10 * 10 // custom jobs pay a little less
		j.Custom = true
		g.job = j
		g.phase = 1
		g.loadTimer = 0
		g.routeDirty = true
		g.state = stPlay
		g.flash("Custom job created! Head to " + places[j.Src].Name)
	}
}

func (g *Game) target() *Place {
	switch g.phase {
	case 1:
		return places[g.job.Src]
	case 2:
		return places[g.job.Dst]
	}
	return nil
}

func (g *Game) inBay(p *Place) bool {
	x, y, w, h := p.bay()
	return g.truck.X > x && g.truck.X < x+w && g.truck.Y > y && g.truck.Y < y+h
}

func (g *Game) updateJobs() {
	if g.phase == 0 {
		for i, k := range []ebiten.Key{ebiten.KeyDigit1, ebiten.KeyDigit2, ebiten.KeyDigit3} {
			if i < len(g.offers) && (inpututil.IsKeyJustPressed(k) || inpututil.IsKeyJustPressed(ebiten.KeyNumpad1+ebiten.Key(i))) {
				g.job = g.offers[i]
				g.phase = 1
				g.loadTimer = 0
				g.flash("Job accepted! Head to " + places[g.job.Src].Name)
				g.routeDirty = true
			}
		}
	} else if inpututil.IsKeyJustPressed(ebiten.KeyC) {
		if g.phase == 2 {
			g.money -= 100
			g.flash("Job cancelled. -£100 fee")
		} else {
			g.flash("Job cancelled")
		}
		g.phase = 0
		g.makeOffers()
	}
	if g.phase == 2 {
		g.timeLeft -= dt
	}
	stopped := math.Abs(g.truck.Speed) < 20

	// fuel stations
	g.fuelling = false
	g.wrongPump = ""
	for _, fs := range stations {
		m := g.model()
		for _, ln := range fs.p.lanes() {
			t := g.truck
			if t.X < ln.X || t.X > ln.X+ln.W || t.Y < ln.Y || t.Y > ln.Y+ln.H || !stopped {
				continue
			}
			if ln.Electric != m.Electric {
				if m.Electric {
					g.wrongPump = "That's a diesel pump - park in an EV CHARGING bay"
				} else {
					g.wrongPump = "That's an EV charger - park at a DIESEL pump"
				}
				continue
			}
			price, rate := fs.p.Fuel, 25.0
			if m.Electric {
				price, rate = fs.p.Charge, m.Tank/120 // 0-100% in two minutes
			}
			if g.fuel[g.cur] < m.Tank-0.01 && g.money > 0 {
				add := math.Min(rate*dt, m.Tank-g.fuel[g.cur])
				g.fuel[g.cur] += add
				g.fuelBill += add * price
				g.fuelling = true
				g.money -= int(g.fuelBill)
				g.fuelBill -= float64(int(g.fuelBill))
			}
		}
	}

	// dealer
	dealerHere := false
	for _, p := range places {
		if p.Dealer && g.inBay(p) {
			dealerHere = true
			if stopped && !g.shopShown {
				g.shopShown = true
				g.state = stShop
				g.shopSel = g.cur
				g.shopPaint = g.paint[g.cur]
			}
		}
	}
	if !dealerHere {
		g.shopShown = false
	}

	p := g.target()
	if p == nil {
		return
	}
	if g.inBay(p) && stopped {
		g.loadTimer += dt
		if g.loadTimer >= 1.5 {
			g.loadTimer = 0
			if g.phase == 1 {
				g.phase = 2
				g.timeLeft = g.job.TimeLimit
				g.condition = 100
				g.flash(fmt.Sprintf("Loaded %s! Deliver to %s", g.job.Cargo, places[g.job.Dst].Name))
			} else {
				pay := float64(g.job.Pay) * g.condition / 100
				late := 0
				if g.timeLeft < 0 {
					late = int(-g.timeLeft) * 8
				}
				final := int(math.Max(25, pay-float64(late)))
				g.money += final
				g.deliveries++
				s := fmt.Sprintf("Delivered! +£%d", final)
				if late > 0 {
					s += fmt.Sprintf("  (late -£%d)", late)
				}
				if g.condition < 100 {
					s += fmt.Sprintf("  (cargo %.0f%%)", g.condition)
				}
				g.flash(s)
				g.phase = 0
				g.makeOffers()
				g.save()
			}
		}
	} else {
		g.loadTimer = 0
	}
}

func (g *Game) updateShop() {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) || inpututil.IsKeyJustPressed(ebiten.KeyBackspace) {
		g.state = stPlay
		return
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowUp) || inpututil.IsKeyJustPressed(ebiten.KeyW) {
		g.shopSel = (g.shopSel + len(models) - 1) % len(models)
		g.shopPaint = g.paint[g.shopSel]
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowDown) || inpututil.IsKeyJustPressed(ebiten.KeyS) {
		g.shopSel = (g.shopSel + 1) % len(models)
		g.shopPaint = g.paint[g.shopSel]
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowLeft) || inpututil.IsKeyJustPressed(ebiten.KeyA) {
		g.shopPaint = (g.shopPaint + len(paints) - 1) % len(paints)
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowRight) || inpututil.IsKeyJustPressed(ebiten.KeyD) {
		g.shopPaint = (g.shopPaint + 1) % len(paints)
	}
	m := &models[g.shopSel]
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
		switch {
		case !g.owned[g.shopSel]:
			if g.money >= m.Price {
				g.money -= m.Price
				g.owned[g.shopSel] = true
				g.paint[g.shopSel] = g.shopPaint
				g.fuel[g.shopSel] = m.Tank
				if g.phase != 2 {
					g.cur = g.shopSel
				}
				g.shopMsg = "Bought the " + m.Name + "! Full tank included."
				g.save()
			} else {
				g.shopMsg = fmt.Sprintf("You need £%d more for that one.", m.Price-g.money)
			}
		case g.phase == 2 && g.shopSel != g.cur:
			g.shopMsg = "Deliver your cargo before swapping trucks."
		default:
			g.cur = g.shopSel
			g.shopMsg = "You're now driving the " + m.Name + "."
		}
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyR) {
		switch {
		case !g.owned[g.shopSel]:
			g.shopMsg = "Buy it first, then you can paint it."
		case g.shopPaint == g.paint[g.shopSel]:
			g.shopMsg = "It's already that colour!"
		case g.money < paintPrice:
			g.shopMsg = "Not enough money for a respray."
		default:
			g.money -= paintPrice
			g.paint[g.shopSel] = g.shopPaint
			g.shopMsg = "Resprayed in " + paints[g.shopPaint].Name + "!"
			g.save()
		}
	}
}

type Station struct {
	p     *Place
	price float64
}

var stations []Station

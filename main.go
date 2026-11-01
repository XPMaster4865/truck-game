package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"math/rand"
	"strings"
	_ "time/tzdata" // UK time zone (GMT/BST) built in, so it works on any PC

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

const (
	stTitle = iota
	stPlay
	stPause
	stGameOver
	stShop
	stMap
	stJobMaker
	stOnline
)

type Game struct {
	state            int
	tiles            [mapW][mapH]TileKind
	tileEdge         [mapW][mapH]float32
	yardOf           [mapW][mapH]int16
	nodes            []Node
	roads            []*Road
	motorways        []int
	slips            []*Road
	slipMW           []int
	slipZones        map[int][]slipZone
	slipTile         [mapW][mapH]bool
	mwDest           map[int][2]string
	segIdx           map[int][]segRef
	hornOn           bool
	aiHonk           float64
	aiHonkVol        float64
	gpsOn            bool
	gpsX, gpsY       float64
	route            [][2]float64
	routeSteps       []routeStep
	routeLen         float64
	routeTimer       float64
	routeDirty       bool
	gpsText          string
	gpsTurn          int
	mapOX, mapOY     float64
	mapPanY          float64
	carGrid          map[int][]*Car
	carsTo           map[int][]*Car
	carsRoad         map[int][]*Car
	jmCol            int
	jmFrom, jmTo     int
	jmCargo          int
	jmMsg            string
	buildings        []Rect
	cameras          []*Camera
	chunkCache       map[[2]int]*ebiten.Image
	chunkUsed        map[[2]int]int64
	frameNo          int64
	nextBreakdown    float64
	town             string
	ind              int
	indAngle         float64
	wrongPump        string
	hasSave          bool
	saveTimer        float64
	mapPan           float64
	sound            *Sound
	noSave           bool
	motorCheck       float64
	lamps            [][4]float64
	yardLamps        [][2]float64
	fixedHour        float64
	realClock        bool
	escape           float64
	net              *Net
	netTick          int
	typing           bool
	chatBuf          string
	olServer, olName string
	olField          int
	olMsg            string
	bigMap           *ebiten.Image
	bms              float64
	mini             *ebiten.Image
	truckImgs        map[[3]int]*ebiten.Image

	truck              Truck
	prevX, prevY       float64
	prevInBox          bool
	trail              [][2]float64
	cars               []*Car
	incidents          []*Incident
	nextIncident       float64
	nextEV             float64
	offers             []Job
	job                Job
	phase              int
	timeLeft           float64
	condition          float64
	loadTimer          float64
	money, deliveries  int
	finesTotal         int
	cool               map[string]float64
	wrongTimer         float64
	paveTimer          float64
	evBehind           bool
	evIgnore           float64
	evBlockedTime      float64
	chaser             *Car
	chaseReason        string
	chaseFine          int
	chaseTime          float64
	stopTime           float64
	roadName           string
	limit              float64
	fuelling           bool
	fuelBill           float64
	owned              []bool
	paint              []int
	fuel               []float64
	cur                int
	shopSel, shopPaint int
	shopShown          bool
	shopMsg            string

	msg, fineTitle, fineSub, news  string
	msgTimer, fineTimer, newsTimer float64
	camX, camY, shake, hitCool     float64
	clock                          float64
	rng                            *rand.Rand
}

var (
	faceTiny, faceSign, faceSmall, faceMed, faceBig, faceTitle *text.GoTextFace
	whiteImg                                                   = ebiten.NewImage(3, 3)
	whiteSub                                                   *ebiten.Image
)

func init() {
	whiteImg.Fill(color.White)
	whiteSub = whiteImg.SubImage(image.Rect(1, 1, 2, 2)).(*ebiten.Image)
	reg, err := text.NewGoTextFaceSource(bytes.NewReader(goregular.TTF))
	if err != nil {
		log.Fatal(err)
	}
	bold, err := text.NewGoTextFaceSource(bytes.NewReader(gobold.TTF))
	if err != nil {
		log.Fatal(err)
	}
	faceTiny = &text.GoTextFace{Source: reg, Size: 13}
	faceSign = &text.GoTextFace{Source: bold, Size: 12}
	faceSmall = &text.GoTextFace{Source: reg, Size: 16}
	faceMed = &text.GoTextFace{Source: bold, Size: 20}
	faceBig = &text.GoTextFace{Source: bold, Size: 30}
	faceTitle = &text.GoTextFace{Source: bold, Size: 84}
}

func NewGame() *Game {
	g := &Game{rng: rand.New(rand.NewSource(42)), truckImgs: map[[3]int]*ebiten.Image{}}
	g.fixedHour = -1
	g.generate()
	g.reset()
	g.hasSave = saveExists()
	return g
}

func (g *Game) reset() {
	var start *Place
	for _, p := range places {
		if p.Name == "Amazin' Warehouse" {
			start = p
		}
	}
	x, y := start.bayCenter()
	g.truck = Truck{X: x, Y: y}
	g.money, g.deliveries, g.finesTotal, g.phase = 500, 0, 0, 0
	g.gpsOn = false
	g.route = nil
	g.routeDirty = true
	g.cool = map[string]float64{}
	g.condition = 100
	g.limit = 30
	g.owned = make([]bool, len(models))
	g.paint = make([]int, len(models))
	g.fuel = make([]float64, len(models))
	g.owned[0] = true
	g.cur = 0
	g.fuel[0] = models[0].Tank * 0.7
	g.trail = nil
	g.chaser = nil
	g.incidents = nil
	g.nextIncident = 20
	g.nextEV = 25
	g.makeOffers()
	g.cars = nil
	g.camX, g.camY = g.truck.X-screenW/2, g.truck.Y-screenH/2
	for n := 0; n < 95; n++ {
		k := kCar
		switch r := g.rng.Float64(); {
		case r < 0.06:
			k = kBus
		case r < 0.20:
			k = kVan
		case r < 0.27:
			k = kLorry
		case r < 0.33:
			k = kPolice
		}
		g.cars = append(g.cars, g.newCar(k))
	}
	// some traffic already on the motorway
	for n := 0; n < 36; n++ {
		k := kCar
		if n%3 == 0 {
			k = kLorry
		}
		c := g.makeCar(k)
		mi := g.motorways[n%len(g.motorways)]
		mr := g.roads[mi]
		if (n/2)%2 == 0 {
			g.setRoad(c, mi, mr.A)
		} else {
			g.setRoad(c, mi, mr.B)
		}
		c.t = 900 + float64(n/4)*1300 + g.rng.Float64()*300
		c.off = g.laneTarget(c)
		c.inTurn = false
		g.setNext(c)
		c.speed = g.carLimit(c)
		g.carPose(c)
		g.cars = append(g.cars, c)
	}
	g.ind = 0
	g.nextBreakdown = 15
	g.msgTimer, g.fineTimer, g.newsTimer = 0, 0, 0
	g.shopShown = true
}

func (g *Game) flash(s string)     { g.msg, g.msgTimer = s, 3.5 }
func (g *Game) newsFlash(s string) { g.news, g.newsTimer = s, 9 }

// ---------- drawing helpers ----------

func fillRect(dst *ebiten.Image, x, y, w, h float64, c color.Color) {
	vector.DrawFilledRect(dst, float32(x), float32(y), float32(w), float32(h), c, false)
}

func fillCircle(dst *ebiten.Image, x, y, r float64, c color.Color) {
	vector.DrawFilledCircle(dst, float32(x), float32(y), float32(r), c, smoothShapes)
}

func strokeLine(dst *ebiten.Image, x1, y1, x2, y2, w float64, c color.Color) {
	vector.StrokeLine(dst, float32(x1), float32(y1), float32(x2), float32(y2), float32(w), c, smoothShapes)
}

func fillTri(dst *ebiten.Image, x1, y1, x2, y2, x3, y3 float64, c color.RGBA) {
	cr, cg, cb, ca := float32(c.R)/255, float32(c.G)/255, float32(c.B)/255, float32(c.A)/255
	v := func(x, y float64) ebiten.Vertex {
		return ebiten.Vertex{DstX: float32(x), DstY: float32(y), SrcX: 1, SrcY: 1, ColorR: cr, ColorG: cg, ColorB: cb, ColorA: ca}
	}
	dst.DrawTriangles([]ebiten.Vertex{v(x1, y1), v(x2, y2), v(x3, y3)}, []uint16{0, 1, 2}, whiteSub, &ebiten.DrawTrianglesOptions{AntiAlias: smoothShapes})
}

func drawText(dst *ebiten.Image, s string, f *text.GoTextFace, x, y float64, c color.Color, align text.Align) {
	op := &text.DrawOptions{}
	op.GeoM.Translate(x, y)
	op.ColorScale.ScaleWithColor(c)
	op.PrimaryAlign = align
	op.LineSpacing = f.Size * 1.35
	text.Draw(dst, s, f, op)
}

func drawTextShadow(dst *ebiten.Image, s string, f *text.GoTextFace, x, y float64, c color.Color, align text.Align) {
	drawText(dst, s, f, x+2, y+2, color.RGBA{0, 0, 0, 200}, align)
	drawText(dst, s, f, x, y, c, align)
}

func panel(dst *ebiten.Image, x, y, w, h float64) {
	fillRect(dst, x, y, w, h, color.RGBA{10, 12, 20, 200})
	fillRect(dst, x, y, w, 2, color.NRGBA{255, 255, 255, 40})
}

func pounds(n int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(ch)
	}
	if neg {
		return "-£" + b.String()
	}
	return "£" + b.String()
}

// ---------- update ----------

func (g *Game) Update() error {
	if g.chunkCache == nil {
		g.buildWorld()
	}
	g.frameNo++
	g.prefetchChunk()
	g.netUpdate()
	if g.state == stOnline {
		g.clock += dt
		g.updateOnlineMenu()
		return nil
	}
	if !g.typing && (inpututil.IsKeyJustPressed(ebiten.KeyF) || inpututil.IsKeyJustPressed(ebiten.KeyF11)) {
		ebiten.SetFullscreen(!ebiten.IsFullscreen())
	}
	switch g.state {
	case stTitle:
		g.clock += dt
		g.updateCars()
		g.camX = worldW/2 - screenW/2 + math.Sin(g.clock*0.08)*1800
		g.camY = worldH/2 - screenH/2 + math.Sin(g.clock*0.06)*1200
		if g.sound == nil && !g.noSave {
			g.sound = newSound()
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeySpace) {
			if !g.hasSave || !g.load() {
				g.reset()
			} else {
				g.flash("Welcome back! Save loaded.")
			}
			g.state = stPlay
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyN) {
			g.reset()
			g.save()
			g.state = stPlay
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyO) {
			g.openOnlineMenu()
		}
		g.updateSound()
		return nil
	case stPause:
		if inpututil.IsKeyJustPressed(ebiten.KeyP) || inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
			g.state = stPlay
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyQ) {
			g.save()
			g.state = stTitle
		}
		g.updateSound()
		return nil
	case stMap:
		if inpututil.IsKeyJustPressed(ebiten.KeyM) || inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
			g.state = stPlay
		}
		if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonRight) {
			mx, my := ebiten.CursorPosition()
			wx, wy := (float64(mx)-g.mapOX)/g.bms, (float64(my)-g.mapOY)/g.bms
			if wx > 0 && wy > 0 && wx < worldW && wy < worldH {
				g.gpsOn, g.gpsX, g.gpsY = true, wx, wy
				g.routeDirty = true
				g.updateGPS()
				if len(g.route) == 0 {
					g.gpsOn = false
					g.flash("Can't find a route there.")
				} else {
					g.flash("Sat-nav: route set (" + fmtDist(g.routeLen) + "). Press G to clear.")
				}
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyG) {
			g.gpsOn = false
			g.routeDirty = true
		}
		g.updateSound()
		return nil
	case stShop:
		g.updateShop()
		return nil
	case stJobMaker:
		g.updateJobMaker()
		return nil
	case stGameOver:
		if g.hasSave {
			deleteSave()
			g.hasSave = false
		}
		g.updateSound()
		if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
			g.state = stTitle
		}
		return nil
	}
	if g.typing {
		g.updateChatInput()
	} else if g.net != nil && g.net.online && inpututil.IsKeyJustPressed(ebiten.KeyT) {
		g.typing, g.chatBuf = true, ""
		ebiten.AppendInputChars(nil)
	}
	if !g.typing && (inpututil.IsKeyJustPressed(ebiten.KeyP) || inpututil.IsKeyJustPressed(ebiten.KeyEscape)) {
		g.state = stPause
		return nil
	}
	if !g.typing && inpututil.IsKeyJustPressed(ebiten.KeyK) {
		g.realClock = !g.realClock
		if g.realClock {
			g.flash("Clock: real time (matches your PC)")
		} else {
			g.flash("Clock: fast time (a day lasts 24 minutes)")
		}
	}
	if !g.typing && inpututil.IsKeyJustPressed(ebiten.KeyM) {
		g.state = stMap
		g.mapPan, g.mapPanY = 0, 0
		return nil
	}
	if !g.typing && inpututil.IsKeyJustPressed(ebiten.KeyJ) && g.phase == 0 {
		g.state = stJobMaker
		g.jmMsg = ""
		return nil
	}
	if !g.typing && inpututil.IsKeyJustPressed(ebiten.KeyG) && g.gpsOn {
		g.gpsOn = false
		g.routeDirty = true
		g.flash("Sat-nav pin cleared.")
	}
	if !g.typing && g.fuel[g.cur] <= 0 && inpututil.IsKeyJustPressed(ebiten.KeyR) {
		g.money -= 150
		g.fuel[g.cur] = models[g.cur].Tank * 0.25
		g.flash("Recovery truck topped you up (25%). -£150")
	}
	g.clock += dt
	if g.hitCool > 0 {
		g.hitCool -= dt
	}
	g.updateTruck()
	g.remoteBump()
	g.updateCars()
	g.updateIncidents()
	g.updateLaw()
	if g.state == stPlay && !g.typing {
		g.updateJobs()
	}
	g.updateGPS()
	for _, t := range []*float64{&g.msgTimer, &g.fineTimer, &g.newsTimer} {
		if *t > 0 {
			*t -= dt
		}
	}
	look := g.truck.Speed * 0.35
	tx := g.truck.X + math.Cos(g.truck.Angle)*look - screenW/2
	ty := g.truck.Y + math.Sin(g.truck.Angle)*look - screenH/2
	g.camX += (tx - g.camX) * math.Min(1, dt*5)
	g.camY += (ty - g.camY) * math.Min(1, dt*5)
	g.shake *= 0.9
	g.saveTimer += dt
	if g.saveTimer > 30 {
		g.saveTimer = 0
		g.save()
	}
	g.updateSound()
	return nil
}

// ---------- draw ----------

func (g *Game) Draw(screen *ebiten.Image) {
	screen.Fill(rgb(80, 130, 60))
	if g.chunkCache == nil {
		return
	}
	cx := math.Max(-200, math.Min(worldW-screenW+200, g.camX))
	cy := math.Max(-200, math.Min(worldH-screenH+200, g.camY))
	if g.shake > 0.5 {
		cx += (g.rng.Float64()*2 - 1) * g.shake
		cy += (g.rng.Float64()*2 - 1) * g.shake
	}
	for i := int(math.Floor(cx / chunkSz)); float64(i*chunkSz) < cx+screenW; i++ {
		for j := int(math.Floor(cy / chunkSz)); float64(j*chunkSz) < cy+screenH; j++ {
			if i < 0 || j < 0 || i*chunkSz >= worldW || j*chunkSz >= worldH {
				continue
			}
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(float64(i*chunkSz)-cx, float64(j*chunkSz)-cy)
			screen.DrawImage(g.chunk(i, j), op)
		}
	}
	if p := g.target(); p != nil && g.state != stTitle {
		x, y, w, h := p.bay()
		a := uint8(70 + 50*math.Sin(g.clock*6))
		c := p.Color
		fillRect(screen, x-cx, y-cy, w, h, color.NRGBA{c.R / 2, c.G / 2, c.B / 2, a})
	}
	if g.state != stTitle {
		g.drawRouteWorld(screen, cx, cy)
	}
	for _, in := range g.incidents {
		g.drawIncident(screen, in, cx, cy)
	}
	for _, c := range g.cars {
		g.drawCar(screen, c, cx, cy)
	}
	g.drawRemotes(screen, cx, cy)
	g.drawLights(screen, cx, cy)

	if g.state == stOnline {
		g.drawNight(screen, cx, cy)
		g.drawOnlineMenu(screen)
		return
	}
	if g.state == stTitle {
		g.drawNight(screen, cx, cy)
		g.drawTitle(screen)
		return
	}
	drawRot(screen, g.truckImg(), g.truck.X-cx, g.truck.Y-cy, g.truck.Angle)
	{
		tb := g.truck.Speed > 5 && pressed(ebiten.KeyS, ebiten.KeyArrowDown, ebiten.KeySpace)
		g.drawLamps(screen, g.truck.X-cx, g.truck.Y-cy, math.Cos(g.truck.Angle), math.Sin(g.truck.Angle), 44, 34, g.ind, tb)
	}
	g.drawNight(screen, cx, cy)

	if p := g.target(); p != nil {
		bx, by := p.bayCenter()
		dx, dy := bx-g.truck.X, by-g.truck.Y
		if math.Hypot(dx, dy) > 160 {
			a := math.Atan2(dy, dx)
			sx, sy := g.truck.X-cx+math.Cos(a)*85, g.truck.Y-cy+math.Sin(a)*85
			col := rgb(255, 220, 60)
			if g.phase == 2 {
				col = rgb(80, 240, 110)
			}
			p1x, p1y := sx+math.Cos(a)*22, sy+math.Sin(a)*22
			p2x, p2y := sx+math.Cos(a+2.4)*14, sy+math.Sin(a+2.4)*14
			p3x, p3y := sx+math.Cos(a-2.4)*14, sy+math.Sin(a-2.4)*14
			fillTri(screen, p1x+2, p1y+2, p2x+2, p2y+2, p3x+2, p3y+2, color.RGBA{0, 0, 0, 140})
			fillTri(screen, p1x, p1y, p2x, p2y, p3x, p3y, col)
		}
	}

	g.drawHUD(screen)
	switch g.state {
	case stPause:
		fillRect(screen, 0, 0, screenW, screenH, color.RGBA{0, 0, 0, 150})
		drawTextShadow(screen, "PAUSED", faceTitle, screenW/2, 150, color.White, text.AlignCenter)
		g.drawControls(screen, 290)
		drawTextShadow(screen, "P / Esc = resume      Q = quit to title", faceMed, screenW/2, 560, color.White, text.AlignCenter)
	case stMap:
		g.drawBigMap(screen)
	case stShop:
		g.drawShop(screen)
	case stJobMaker:
		g.drawJobMaker(screen)
	case stGameOver:
		fillRect(screen, 0, 0, screenW, screenH, color.NRGBA{60, 0, 0, 200})
		drawTextShadow(screen, "LICENCE REVOKED", faceTitle, screenW/2, 160, rgb(255, 80, 80), text.AlignCenter)
		drawTextShadow(screen, "You hit 12 penalty points and have been banned from driving.", faceMed, screenW/2, 280, color.White, text.AlignCenter)
		drawTextShadow(screen, fmt.Sprintf("Deliveries: %d     Money: %s     Fines paid: %s", g.deliveries, pounds(g.money), pounds(g.finesTotal)), faceMed, screenW/2, 330, color.White, text.AlignCenter)
		drawTextShadow(screen, "Press ENTER to return to the title screen", faceBig, screenW/2, 450, color.White, text.AlignCenter)
	}
}

func (g *Game) drawControls(screen *ebiten.Image, y float64) {
	lines := [][2]string{
		{"W / Up", "Accelerate"}, {"S / Down", "Brake / Reverse"}, {"A D / Left Right", "Steer"}, {"Q / E", "Indicate left / right"}, {"X", "Hazard lights"}, {"H", "Horn"}, {"M + right-click", "Set sat-nav destination"}, {"J", "Create your own job"}, {"K", "Clock: fast / real time"},
		{"Space", "Handbrake"}, {"1  2  3", "Choose a job"}, {"M", "City map"},
		{"C", "Cancel job"}, {"R", "Call recovery (when out of fuel)"}, {"P / Esc", "Pause"}, {"F", "Fullscreen"},
	}
	for i, l := range lines {
		col := float64(i / 9)
		row := float64(i % 9)
		x := screenW/2 - 420 + col*440
		drawTextShadow(screen, l[0], faceMed, x, y+row*27, rgb(255, 210, 60), text.AlignStart)
		drawTextShadow(screen, l[1], faceSmall, x+180, y+row*27+3, color.White, text.AlignStart)
	}
}

func (g *Game) drawTitle(screen *ebiten.Image) {
	fillRect(screen, 0, 0, screenW, screenH, color.RGBA{0, 0, 0, 150})
	drawTextShadow(screen, "TRUCK HAUL", faceTitle, screenW/2, 70, rgb(255, 210, 60), text.AlignCenter)
	drawTextShadow(screen, "Haul cargo across town. Obey the Highway Code. Upgrade your rig.", faceMed, screenW/2, 185, color.White, text.AlignCenter)
	tips := "Drive on the LEFT  ·  Watch for speed cameras & red lights  ·  Pull over for blue lights  ·  12 points = banned"
	drawTextShadow(screen, tips, faceSmall, screenW/2, 225, rgb(200, 220, 255), text.AlignCenter)
	g.drawControls(screen, 290)
	if int(g.clock*2)%2 == 0 {
		if g.hasSave {
			drawTextShadow(screen, "ENTER = continue saved game      N = new game", faceBig, screenW/2, 570, color.White, text.AlignCenter)
		} else {
			drawTextShadow(screen, "Press ENTER to start", faceBig, screenW/2, 570, color.White, text.AlignCenter)
		}
	}
	drawText(screen, "O = PLAY ONLINE with friends", faceMed, screenW/2, 625, rgb(120, 240, 120), text.AlignCenter)
	drawText(screen, gameVersion, faceMed, screenW-20, screenH-36, color.RGBA{200, 200, 200, 255}, text.AlignEnd)
}

// ---------- HUD ----------

func (g *Game) drawHUD(screen *ebiten.Image) {
	white := color.White
	m := g.model()
	// stats
	panel(screen, 12, 12, 260, 128)
	mc := rgb(120, 240, 120)
	if g.money < 0 {
		mc = rgb(255, 90, 90)
	}
	drawText(screen, pounds(g.money), faceBig, 26, 16, mc, text.AlignStart)
	drawText(screen, fmt.Sprintf("Deliveries: %d", g.deliveries), faceSmall, 26, 56, white, text.AlignStart)
	drawText(screen, "Fines paid: "+pounds(g.finesTotal), faceSmall, 26, 78, white, text.AlignStart)
	drawText(screen, m.Name, faceSmall, 26, 100, rgb(180, 200, 255), text.AlignStart)

	// job panel
	w := 660.0
	x := 284.0
	if g.phase == 0 {
		panel(screen, x, 12, w, 150)
		drawText(screen, "JOB BOARD - 1/2/3 to pick  ·  J = make your own", faceMed, x+w/2, 20, rgb(255, 220, 80), text.AlignCenter)
		for i, o := range g.offers {
			s := fmt.Sprintf("%d)  %s:  %s  ->  %s", i+1, o.Cargo, places[o.Src].Name, places[o.Dst].Name)
			jf := faceSmall
			if len(s) > 52 {
				jf = faceTiny
			}
			drawText(screen, s, jf, x+20, 58+float64(i)*32, white, text.AlignStart)
			drawText(screen, fmt.Sprintf("%s  /  %.0fs", pounds(o.Pay), o.TimeLimit), faceSmall, x+w-20, 58+float64(i)*32, rgb(120, 240, 120), text.AlignEnd)
		}
	} else {
		panel(screen, x, 12, w, 86)
		var line1 string
		if g.phase == 1 {
			line1 = fmt.Sprintf("Pick up %s at %s", g.job.Cargo, places[g.job.Src].Name)
			drawText(screen, fmt.Sprintf("Then deliver to %s for %s", places[g.job.Dst].Name, pounds(g.job.Pay)), faceSmall, x+w/2, 50, white, text.AlignCenter)
		} else {
			line1 = fmt.Sprintf("Deliver %s to %s", g.job.Cargo, places[g.job.Dst].Name)
			tl := g.timeLeft
			tcol := color.Color(white)
			ts := fmt.Sprintf("Time: %d:%02d", int(tl)/60, int(tl)%60)
			if tl < 0 {
				tcol = rgb(255, 90, 90)
				ts = fmt.Sprintf("LATE by %ds", int(-tl))
			} else if tl < 10 {
				tcol = rgb(255, 200, 80)
			}
			drawText(screen, ts, faceSmall, x+20, 50, tcol, text.AlignStart)
			drawText(screen, fmt.Sprintf("Cargo: %.0f%%", g.condition), faceSmall, x+w/2, 50, white, text.AlignCenter)
			drawText(screen, "Pay: "+pounds(g.job.Pay), faceSmall, x+w-20, 50, rgb(120, 240, 120), text.AlignEnd)
		}
		drawText(screen, line1, faceMed, x+w/2, 20, white, text.AlignCenter)
		p := g.target()
		bx, by := p.bayCenter()
		dist := math.Hypot(bx-g.truck.X, by-g.truck.Y) / 10
		drawText(screen, fmt.Sprintf("%d m away   ·   M = map   ·   C = cancel job", int(dist)), faceSmall, x+w/2, 72, color.RGBA{180, 180, 190, 255}, text.AlignCenter)

		if g.inBay(p) {
			verb := "Loading"
			if g.phase == 2 {
				verb = "Unloading"
			}
			bw := 300.0
			bxp := screenW/2 - bw/2
			panel(screen, bxp, screenH/2+80, bw, 50)
			if math.Abs(g.truck.Speed) < 20 {
				drawText(screen, verb+"...", faceSmall, screenW/2, screenH/2+84, white, text.AlignCenter)
				fillRect(screen, bxp+15, screenH/2+108, bw-30, 12, rgb(50, 50, 60))
				fillRect(screen, bxp+15, screenH/2+108, (bw-30)*g.loadTimer/1.5, 12, rgb(90, 220, 110))
			} else {
				drawText(screen, "Stop the truck to "+strings.ToLower(verb[:len(verb)-3]), faceSmall, screenW/2, screenH/2+94, rgb(255, 220, 80), text.AlignCenter)
			}
		}
	}
	if g.wrongPump != "" && !g.fuelling {
		drawTextShadow(screen, g.wrongPump, faceMed, screenW/2, screenH/2+90, rgb(255, 180, 60), text.AlignCenter)
	}
	if g.fuelling {
		panel(screen, screenW/2-150, screenH/2+80, 300, 50)
		if m.Electric {
			left := (m.Tank - g.fuel[g.cur]) / (m.Tank / 120)
			drawText(screen, fmt.Sprintf("Charging...  %.0f%%   (%d:%02d to full)", 100*g.fuel[g.cur]/m.Tank, int(left)/60, int(left)%60), faceSmall, screenW/2, screenH/2+84, white, text.AlignCenter)
		} else {
			drawText(screen, fmt.Sprintf("Refuelling...  %.0f / %.0f L", g.fuel[g.cur], m.Tank), faceSmall, screenW/2, screenH/2+84, white, text.AlignCenter)
		}
		fillRect(screen, screenW/2-135, screenH/2+108, 270*g.fuel[g.cur]/m.Tank, 12, rgb(250, 180, 40))
	}

	// news
	if g.newsTimer > 0 {
		nw := 300.0
		nx := screenW - nw - 12
		panel(screen, nx, 12, nw, 78)
		fillRect(screen, nx, 12, 6, 78, rgb(230, 40, 40))
		drawText(screen, "TRAFFIC NEWS", faceMed, nx+16, 16, rgb(255, 90, 90), text.AlignStart)
		drawText(screen, g.news, faceTiny, nx+16, 44, white, text.AlignStart)
	}

	// warnings in the middle
	wy := screenH/2 - 150.0
	warn := func(s string, c color.RGBA) {
		if math.Mod(g.clock, 0.8) < 0.55 {
			drawTextShadow(screen, s, faceBig, screenW/2, wy, c, text.AlignCenter)
		}
		wy += 40
	}
	if g.chaser != nil {
		d := math.Hypot(g.chaser.x-g.truck.X, g.chaser.y-g.truck.Y)
		if g.chaseTime <= 8 {
			warn(fmt.Sprintf("POLICE - PULL OVER AND STOP! (%ds)", int(9-g.chaseTime)), rgb(90, 150, 255))
		} else {
			warn(fmt.Sprintf("POLICE PURSUIT - %d m behind - stop, or lose them!", int(d/10)), rgb(255, 80, 80))
			// escape meter
			bw := 360.0
			bx := screenW/2 - bw/2
			by := wy - 8
			fillRect(screen, bx, by, bw, 14, rgb(40, 40, 50))
			fillRect(screen, bx, by, bw*math.Min(1, g.escape/escapeTime), 14, rgb(90, 220, 110))
			drawText(screen, "ESCAPE", faceSign, bx-8, by-1, color.White, text.AlignEnd)
			wy += 18
		}
	}
	if g.wrongTimer > 0.4 {
		warn("WRONG SIDE OF THE ROAD - KEEP LEFT!", rgb(255, 90, 90))
	}
	if g.paveTimer > 0.3 {
		warn("GET OFF THE PAVEMENT!", rgb(255, 90, 90))
	}
	if g.fuel[g.cur] <= 0 {
		if m.Electric {
			warn("BATTERY FLAT!  Press R to call recovery (£150)", rgb(255, 180, 40))
		} else {
			warn("OUT OF FUEL!  Press R to call recovery (£150)", rgb(255, 180, 40))
		}
	} else if g.fuel[g.cur] < m.Tank*0.15 {
		if m.Electric {
			drawTextShadow(screen, "Low battery - find a charging point (GridSwerve, Ionnity, motorway services)", faceMed, screenW/2, wy, rgb(255, 180, 40), text.AlignCenter)
		} else {
			drawTextShadow(screen, "Low fuel - find a fuel station (PB, Crab, Essco, Texacoo, services)", faceMed, screenW/2, wy, rgb(255, 180, 40), text.AlignCenter)
		}
	}

	if g.msgTimer > 0 {
		a := uint8(255 * math.Min(1, g.msgTimer))
		drawTextShadow(screen, g.msg, faceBig, screenW/2, screenH-230, color.NRGBA{255, 255, 255, a}, text.AlignCenter)
	}
	if g.fineTimer > 0 {
		fw := 560.0
		fx := screenW/2 - fw/2
		fy := screenH - 175.0
		fillRect(screen, fx, fy, fw, 70, color.NRGBA{180, 20, 20, 230})
		fillRect(screen, fx+4, fy+4, fw-8, 62, color.RGBA{0, 0, 0, 0})
		drawText(screen, g.fineTitle, faceMed, screenW/2, fy+8, color.White, text.AlignCenter)
		drawText(screen, g.fineSub, faceMed, screenW/2, fy+38, rgb(255, 230, 120), text.AlignCenter)
	}

	// sat-nav instruction
	if g.gpsText != "" {
		gw := 460.0
		gx := 12.0
		gy := 172.0
		panel(screen, gx, gy, gw, 62)
		fillRect(screen, gx, gy, 62, 62, rgb(40, 110, 220))
		drawTurnArrow(screen, gx+31, gy+31, g.gpsTurn, rgb(255, 255, 255))
		label := "SAT-NAV"
		if g.gpsOn {
			label = "SAT-NAV  (pin - G to clear)"
		}
		drawText(screen, label, faceSign, gx+72, gy+6, rgb(120, 180, 255), text.AlignStart)
		drawText(screen, g.gpsText, faceSmall, gx+72, gy+26, white, text.AlignStart)
	}
	g.drawJunctionAhead(screen)
	g.drawNetHUD(screen)
	g.drawClock(screen, 12, screenH-216)
	g.drawDash(screen)
	g.drawMinimap(screen)
}

// drawJunctionAhead shows the light / sign that applies to YOU at the next junction.
func (g *Game) drawJunctionAhead(screen *ebiten.Image) {
	h := g.roadQuery(g.truck.X, g.truck.Y, 200)
	if h.Road < 0 || h.Edge > 10 || math.Abs(g.truck.Speed) < 1 && false {
		return
	}
	rd := g.roads[h.Road]
	if rd.Class == clMotorway {
		return
	}
	_, _, tx, ty := rd.at(h.S)
	fx, fy := math.Cos(g.truck.Angle), math.Sin(g.truck.Angle)
	node, dist := rd.B, rd.Len-h.S
	if fx*tx+fy*ty < 0 {
		node, dist = rd.A, h.S
	}
	n := g.nodes[node]
	dist -= n.R
	if dist > 420 || dist < -10 || n.Ctrl == ctrlNone {
		return
	}
	a := g.apprOf(node, h.Road)
	if a == nil {
		return
	}
	x, y := 404.0, screenH-120.0
	switch {
	case n.Ctrl == ctrlLights:
		st := g.light(node, a.Axis)
		panel(screen, x, y-46, 60, 104)
		fillRect(screen, x+12, y-38, 36, 88, rgb(20, 20, 22))
		for k, lamp := range []int{2, 1, 0} {
			lc := lightColor(lamp)
			on := st == lamp || (st == 3 && lamp != 0)
			if on {
				fillCircle(screen, x+30, y-24+float64(k)*29, 11, lc)
			} else {
				fillCircle(screen, x+30, y-24+float64(k)*29, 10, color.RGBA{lc.R / 5, lc.G / 5, lc.B / 5, 255})
			}
		}
		drawText(screen, "YOUR LIGHT", faceSign, x+30, y+62, color.White, text.AlignCenter)
	case a.Stop:
		drawOct(screen, x+30, y, 26, rgb(200, 20, 20))
		drawOct(screen, x+30, y, 22, rgb(240, 240, 240))
		drawOct(screen, x+30, y, 20, rgb(200, 20, 20))
		drawText(screen, "STOP", faceSign, x+30, y-8, color.White, text.AlignCenter)
		drawText(screen, "STOP AHEAD", faceSign, x+30, y+34, color.White, text.AlignCenter)
	case !a.Major:
		fillTri(screen, x, y-20, x+60, y-20, x+30, y+28, rgb(210, 20, 20))
		fillTri(screen, x+10, y-14, x+50, y-14, x+30, y+16, rgb(240, 240, 240))
		drawText(screen, "GIVE WAY", faceSign, x+30, y+34, color.White, text.AlignCenter)
	default:
		drawText(screen, "You have", faceSign, x+30, y-14, rgb(160, 230, 160), text.AlignCenter)
		drawText(screen, "priority", faceSign, x+30, y+2, rgb(160, 230, 160), text.AlignCenter)
	}
}

func (g *Game) drawDash(screen *ebiten.Image) {
	smoothShapes = true
	defer func() { smoothShapes = false }()
	m := g.model()
	px, py := 12.0, screenH-178.0
	panel(screen, px, py, 380, 166)
	if g.roadName != "" {
		drawText(screen, g.roadName, faceSmall, px+12, py+6, rgb(255, 230, 150), text.AlignStart)
	}
	// speedometer
	cx, cy, r := px+92, py+100, 60.0
	maxMph := 70.0
	ang := func(v float64) float64 { return (135 + v/maxMph*270) * math.Pi / 180 }
	fillCircle(screen, cx, cy, r+4, rgb(30, 30, 36))
	fillCircle(screen, cx, cy, r, rgb(15, 15, 20))
	for v := 0.0; v <= maxMph; v += 5 {
		a := ang(v)
		in := r - 6
		col := color.Color(color.White)
		if v > g.limit {
			col = rgb(240, 70, 70)
		}
		if int(v)%10 == 0 {
			in = r - 12
			tx, ty := cx+math.Cos(a)*(r-24), cy+math.Sin(a)*(r-24)
			drawText(screen, fmt.Sprintf("%d", int(v)), faceSign, tx, ty-8, col, text.AlignCenter)
		}
		strokeLine(screen, cx+math.Cos(a)*in, cy+math.Sin(a)*in, cx+math.Cos(a)*(r-2), cy+math.Sin(a)*(r-2), 2, col)
	}
	mph := math.Abs(g.truck.Speed) * mphK
	a := ang(math.Min(mph, maxMph+2))
	strokeLine(screen, cx, cy, cx+math.Cos(a)*(r-8), cy+math.Sin(a)*(r-8), 3, rgb(255, 120, 40))
	fillCircle(screen, cx, cy, 5, rgb(200, 200, 200))
	sc := color.Color(color.White)
	if mph > g.limit+1 {
		sc = rgb(255, 80, 80)
	}
	drawText(screen, fmt.Sprintf("%d", int(mph)), faceMed, cx, cy+18, sc, text.AlignCenter)
	drawText(screen, "mph", faceTiny, cx, cy+40, color.RGBA{180, 180, 180, 255}, text.AlignCenter)

	// fuel gauge
	fx, fy, fr := px+240, py+100, 46.0
	fang := func(f float64) float64 { return (200 + f*140) * math.Pi / 180 }
	fillCircle(screen, fx, fy, fr+4, rgb(30, 30, 36))
	fillCircle(screen, fx, fy, fr, rgb(15, 15, 20))
	for k := 0; k <= 4; k++ {
		aa := fang(float64(k) / 4)
		col := color.Color(color.White)
		if k == 0 {
			col = rgb(240, 70, 70)
		}
		strokeLine(screen, fx+math.Cos(aa)*(fr-10), fy+math.Sin(aa)*(fr-10), fx+math.Cos(aa)*(fr-2), fy+math.Sin(aa)*(fr-2), 2, col)
	}
	drawText(screen, "E", faceSign, fx+math.Cos(fang(0))*(fr-20), fy+math.Sin(fang(0))*(fr-20)-6, rgb(240, 70, 70), text.AlignCenter)
	drawText(screen, "F", faceSign, fx+math.Cos(fang(1))*(fr-20), fy+math.Sin(fang(1))*(fr-20)-6, color.White, text.AlignCenter)
	frac := g.fuel[g.cur] / m.Tank
	fa := fang(frac)
	nc := rgb(255, 120, 40)
	if frac < 0.15 && math.Mod(g.clock, 0.8) < 0.4 {
		nc = rgb(255, 40, 40)
	}
	strokeLine(screen, fx, fy, fx+math.Cos(fa)*(fr-8), fy+math.Sin(fa)*(fr-8), 3, nc)
	fillCircle(screen, fx, fy, 4, rgb(200, 200, 200))
	if m.Electric {
		drawText(screen, fmt.Sprintf("%.0f%%", 100*frac), faceTiny, fx, fy+12, color.White, text.AlignCenter)
		drawText(screen, "BATTERY", faceSign, fx, fy+30, rgb(0, 220, 190), text.AlignCenter)
	} else {
		drawText(screen, fmt.Sprintf("%.0f L", g.fuel[g.cur]), faceTiny, fx, fy+12, color.White, text.AlignCenter)
		drawText(screen, "FUEL", faceSign, fx, fy+30, color.RGBA{180, 180, 180, 255}, text.AlignCenter)
	}

	// speed limit roundel
	lx, ly := px+340, py+70
	fillCircle(screen, lx, ly, 26, rgb(210, 20, 20))
	fillCircle(screen, lx, ly, 20, color.White)
	drawText(screen, fmt.Sprintf("%d", int(g.limit)), faceMed, lx, ly-12, rgb(10, 10, 10), text.AlignCenter)
	// camera warning
	for _, cam := range g.cameras {
		d := math.Hypot(cam.x-g.truck.X, cam.y-g.truck.Y)
		if d < 450 {
			fillRect(screen, lx-26, ly+34, 52, 40, rgb(250, 200, 0))
			fillRect(screen, lx-18, ly+44, 26, 18, rgb(20, 20, 20))
			fillCircle(screen, lx+12, ly+53, 6, rgb(20, 20, 20))
			drawText(screen, "CAMERA", faceSign, lx, ly+76, rgb(250, 200, 0), text.AlignCenter)
			break
		}
	}
}

func (g *Game) mapMarkers(dst *ebiten.Image, ox, oy, s float64, labels bool) {
	for _, p := range places {
		x, y := p.bayCenter()
		mx, my := ox+x*s, oy+y*s
		sz := 7.0
		if labels {
			sz = 10
		}
		fillRect(dst, mx-sz/2-1, my-sz/2-1, sz+2, sz+2, color.Black)
		fillRect(dst, mx-sz/2, my-sz/2, sz, sz, p.Color)
		if p.Fuel > 0 {
			drawText(dst, "F", faceSign, mx, my-7, color.Black, text.AlignCenter)
		} else if p.Charge > 0 {
			drawText(dst, "E", faceSign, mx, my-7, color.Black, text.AlignCenter)
		}
		if labels {
			drawTextShadow(dst, p.Name, faceTiny, mx, my+6, color.White, text.AlignCenter)
		}
	}
	for _, in := range g.incidents {
		mx, my := ox+in.mx*s, oy+in.my*s
		col := rgb(255, 60, 60)
		if in.kind == incWorks {
			col = rgb(255, 150, 0)
		}
		fillTri(dst, mx, my-9, mx-8, my+6, mx+8, my+6, col)
		drawText(dst, "!", faceSign, mx, my-6, color.White, text.AlignCenter)
	}
	if p := g.target(); p != nil {
		x, y := p.bayCenter()
		r := 9 + 3*math.Sin(g.clock*6)
		vector.StrokeCircle(dst, float32(ox+x*s), float32(oy+y*s), float32(r), 2.5, color.White, true)
	}
	tx, ty := ox+g.truck.X*s, oy+g.truck.Y*s
	a := g.truck.Angle
	fillTri(dst, tx+math.Cos(a)*9, ty+math.Sin(a)*9, tx+math.Cos(a+2.5)*7, ty+math.Sin(a+2.5)*7, tx+math.Cos(a-2.5)*7, ty+math.Sin(a-2.5)*7, rgb(255, 255, 255))
	if g.chaser != nil {
		fillCircle(dst, ox+g.chaser.x*s, oy+g.chaser.y*s, 4, rgb(60, 120, 255))
	}
	for _, c := range g.cars {
		if c.isEV() {
			fillCircle(dst, ox+c.x*s, oy+c.y*s, 3, rgb(60, 120, 255))
		}
	}
}

func (g *Game) drawMinimap(screen *ebiten.Image) {
	mw, mh := float64(g.mini.Bounds().Dx()), float64(g.mini.Bounds().Dy())
	g.mini.Fill(rgb(80, 130, 60))
	s := g.bms
	ox := mw/2 - g.truck.X*s
	oy := mh/2 - g.truck.Y*s
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(ox, oy)
	g.mini.DrawImage(g.bigMap, op)
	g.drawRouteMap(g.mini, ox, oy, s)
	g.mapMarkers(g.mini, ox, oy, s, false)
	g.drawRemotesOnMap(g.mini, ox, oy, s, false)
	// target arrow at edge if off minimap
	if p := g.target(); p != nil {
		x, y := p.bayCenter()
		mx, my := ox+x*s, oy+y*s
		if mx < 0 || my < 0 || mx > mw || my > mh {
			a := math.Atan2(my-mh/2, mx-mw/2)
			ex := mw/2 + math.Cos(a)*(mh/2-12)
			ey := mh/2 + math.Sin(a)*(mh/2-12)
			fillTri(g.mini, ex+math.Cos(a)*10, ey+math.Sin(a)*10, ex+math.Cos(a+2.4)*8, ey+math.Sin(a+2.4)*8, ex+math.Cos(a-2.4)*8, ey+math.Sin(a-2.4)*8, rgb(255, 220, 60))
		}
	}
	sx, sy := screenW-mw-16, screenH-mh-16
	fillRect(screen, sx-4, sy-4, mw+8, mh+8, color.RGBA{10, 12, 20, 220})
	o := &ebiten.DrawImageOptions{}
	o.GeoM.Translate(sx, sy)
	screen.DrawImage(g.mini, o)
	drawText(screen, "M = full map", faceTiny, sx+mw-4, sy-22, color.NRGBA{220, 220, 220, 220}, text.AlignEnd)
}

func (g *Game) drawBigMap(screen *ebiten.Image) {
	fillRect(screen, 0, 0, screenW, screenH, color.RGBA{0, 0, 0, 200})
	bw, bh := float64(g.bigMap.Bounds().Dx()), float64(g.bigMap.Bounds().Dy())
	s := g.bms
	vx0, vy0, vw, vh := 20.0, 44.0, float64(screenW-40), float64(screenH-80)
	sp := 900 * dt
	if pressed(ebiten.KeyArrowLeft, ebiten.KeyA) {
		g.mapPan -= sp
	}
	if pressed(ebiten.KeyArrowRight, ebiten.KeyD) {
		g.mapPan += sp
	}
	if pressed(ebiten.KeyArrowUp, ebiten.KeyW) {
		g.mapPanY -= sp
	}
	if pressed(ebiten.KeyArrowDown, ebiten.KeyS) {
		g.mapPanY += sp
	}
	left := math.Max(0, math.Min(bw-vw, g.truck.X*s+g.mapPan-vw/2))
	top := math.Max(0, math.Min(bh-vh, g.truck.Y*s+g.mapPanY-vh/2))
	g.mapPan = left + vw/2 - g.truck.X*s
	g.mapPanY = top + vh/2 - g.truck.Y*s
	ox, oy := vx0-left, vy0-top
	g.mapOX, g.mapOY = ox, oy
	view := screen.SubImage(image.Rect(int(vx0), int(vy0), int(vx0+vw), int(vy0+vh))).(*ebiten.Image)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(ox, oy)
	view.DrawImage(g.bigMap, op)
	for _, c := range cities {
		x := c.OX + float64(c.Cols-1)*c.S/2
		drawTextShadow(view, c.Name, faceBig, ox+x*s, oy+c.OY*s-48, rgb(255, 210, 60), text.AlignCenter)
	}
	for _, mi := range g.motorways {
		mr := g.roads[mi]
		mx, my, _, _ := mr.at(mr.Len / 2)
		drawTextShadow(view, mr.Name, faceBig, ox+mx*s+40, oy+my*s-60, rgb(120, 170, 255), text.AlignCenter)
	}
	g.drawRouteMap(view, ox, oy, s)
	g.mapMarkers(view, ox, oy, s, true)
	g.drawRemotesOnMap(view, ox, oy, s, true)
	cxp, cyp := ebiten.CursorPosition()
	wx, wy := (float64(cxp)-ox)/s, (float64(cyp)-oy)/s
	if hh := g.roadQuery(wx, wy, 300); hh.Road >= 0 && hh.Edge < 60 {
		drawTextShadow(screen, g.roads[hh.Road].Name, faceSmall, float64(cxp)+14, float64(cyp)-8, rgb(255, 255, 200), text.AlignStart)
	}
	drawTextShadow(screen, "MAP  -  RIGHT-CLICK = set sat-nav destination  ·  Arrows/WASD scroll  ·  G clear  ·  M close", faceMed, screenW/2, 10, color.White, text.AlignCenter)
	drawText(screen, "! red = collision   ! orange = roadworks   F = fuel   E = EV charging   white ring = job   red pin = sat-nav", faceTiny, screenW/2, screenH-28, color.RGBA{220, 220, 220, 255}, text.AlignCenter)
}

func (g *Game) drawJobMaker(screen *ebiten.Image) {
	fillRect(screen, 0, 0, screenW, screenH, color.RGBA{0, 0, 0, 225})
	drawTextShadow(screen, "CREATE YOUR OWN JOB", faceBig, screenW/2, 18, rgb(255, 210, 60), text.AlignCenter)
	drawText(screen, "Left/Right = choose column   ·   Up/Down = choose   ·   ENTER = start job   ·   Esc = cancel", faceSmall, screenW/2, 58, color.White, text.AlignCenter)
	jp := jobPlaces()
	cols := []struct {
		title string
		sel   int
		items []string
		x     float64
	}{
		{"PICK UP FROM", g.jmFrom, nil, 40},
		{"DELIVER TO", g.jmTo, nil, 450},
		{"CARGO", g.jmCargo, cargoTypes, 860},
	}
	for _, i := range jp {
		town := "London"
		if c := places[i].City; c >= 0 {
			town = cities[c].Name
		}
		cols[0].items = append(cols[0].items, places[i].Name+"  ("+town+")")
	}
	cols[1].items = cols[0].items
	for ci, c := range cols {
		y0 := 100.0
		hl := ci == g.jmCol
		tc := color.Color(rgb(160, 160, 170))
		if hl {
			tc = rgb(255, 210, 60)
		}
		drawText(screen, c.title, faceMed, c.x, y0, tc, text.AlignStart)
		// show a window of 12 items around the selection
		start := max(0, min(c.sel-6, len(c.items)-12))
		for k := start; k < min(len(c.items), start+12); k++ {
			y := y0 + 36 + float64(k-start)*30
			if k == c.sel {
				col := color.NRGBA{255, 210, 60, 50}
				if !hl {
					col = color.NRGBA{255, 255, 255, 25}
				}
				fillRect(screen, c.x-8, y-3, 380, 28, col)
			}
			drawText(screen, c.items[k], faceSmall, c.x, y, color.White, text.AlignStart)
		}
	}
	if jp[g.jmFrom] != jp[g.jmTo] {
		j := jobFor(jp[g.jmFrom], jp[g.jmTo], cargoTypes[g.jmCargo])
		pay := j.Pay * 9 / 10 / 10 * 10
		sx, sy := places[j.Src].bayCenter()
		dx, dy := places[j.Dst].bayCenter()
		drawTextShadow(screen, fmt.Sprintf("%s  ·  %s  ·  %.0fs time limit  ·  pays %s", cargoTypes[g.jmCargo], fmtDist(math.Abs(sx-dx)+math.Abs(sy-dy)), j.TimeLimit, pounds(pay)), faceMed, screenW/2, 520, rgb(120, 240, 120), text.AlignCenter)
	}
	if g.jmMsg != "" {
		drawTextShadow(screen, g.jmMsg, faceMed, screenW/2, 560, rgb(255, 110, 110), text.AlignCenter)
	}
}

func bar(screen *ebiten.Image, x, y, w, frac float64, c color.Color) {
	fillRect(screen, x, y, w, 10, rgb(50, 50, 60))
	fillRect(screen, x, y, w*math.Max(0, math.Min(1, frac)), 10, c)
}

func (g *Game) drawShop(screen *ebiten.Image) {
	fillRect(screen, 0, 0, screenW, screenH, color.RGBA{0, 0, 0, 235})
	drawTextShadow(screen, "BIG AL'S TRUCK SALES", faceBig, screenW/2, 24, rgb(255, 210, 60), text.AlignCenter)
	drawText(screen, "\"Every truck a bargain, every bargain a truck!\"   ·   Your money: "+pounds(g.money), faceSmall, screenW/2, 64, color.White, text.AlignCenter)
	lx := 60.0
	for i, mm := range models {
		y := 100 + float64(i)*72
		if i == g.shopSel {
			fillRect(screen, lx-10, y-6, 470, 66, color.NRGBA{255, 210, 60, 60})
			fillRect(screen, lx-10, y-6, 4, 66, rgb(255, 210, 60))
		}
		drawRot(screen, makeTruckImgCached(g.cabColorPreview(i), i), lx+50, y+32, 0)
		drawText(screen, mm.Name, faceMed, lx+110, y, color.White, text.AlignStart)
		status := pounds(mm.Price)
		sc := color.Color(rgb(120, 240, 120))
		switch {
		case i == g.cur:
			status, sc = "DRIVING", rgb(100, 180, 255)
		case g.owned[i]:
			status, sc = "OWNED", rgb(200, 200, 200)
		case mm.Price > g.money:
			sc = rgb(255, 110, 110)
		}
		drawText(screen, status, faceMed, lx+450, y, sc, text.AlignEnd)
		if mm.Electric {
			drawText(screen, "ELECTRIC", faceSign, lx+50, y+50, rgb(0, 220, 190), text.AlignCenter)
		}
		drawText(screen, mm.Blurb, faceTiny, lx+110, y+30, color.RGBA{200, 200, 210, 255}, text.AlignStart)
	}
	// detail panel
	m := &models[g.shopSel]
	dx := 580.0
	panel(screen, dx, 100, 650, 470)
	op := &ebiten.DrawImageOptions{}
	img := makeTruckImgCached(g.cabColorPreviewPaint(g.shopSel, g.shopPaint), g.shopSel)
	op.GeoM.Scale(3, 3)
	op.GeoM.Translate(dx+325-135, 120)
	screen.DrawImage(img, op)
	drawText(screen, m.Name, faceBig, dx+325, 240, color.White, text.AlignCenter)
	stats := []struct {
		n string
		v float64
		s string
	}{
		{"Top speed", m.MaxSpeed / 520, fmt.Sprintf("%d mph", int(m.MaxSpeed*mphK))},
		{"Acceleration", m.Accel / 340, ""},
		{"Handling", m.Steer / 3.3, ""},
		{"Brakes", m.Brake / 980, ""},
		{"Cargo protection", (1.5 - m.Protect) / 1.1, ""},
		{"Fuel tank", m.Tank / 500, fmt.Sprintf("%d L", int(m.Tank))},
		{"Range", m.Tank / m.Economy / 75, fmt.Sprintf("%.0f miles", m.Tank/m.Economy*1000/pxPerMile)},
	}
	if m.Electric {
		stats[5].n, stats[5].s = "Battery", fmt.Sprintf("%d kWh", int(m.Tank))
	}
	for i, st := range stats {
		y := 290 + float64(i)*28
		drawText(screen, st.n, faceSmall, dx+30, y, color.White, text.AlignStart)
		bar(screen, dx+210, y+5, 300, st.v, rgb(90, 200, 255))
		drawText(screen, st.s, faceSmall, dx+620, y, color.RGBA{200, 200, 200, 255}, text.AlignEnd)
	}
	pn := paints[g.shopPaint].Name
	pc := g.cabColorPreviewPaint(g.shopSel, g.shopPaint)
	fillRect(screen, dx+30, 495, 26, 26, pc)
	drawText(screen, "Paint: < "+pn+" >", faceMed, dx+66, 495, color.White, text.AlignStart)
	action := "ENTER = buy for " + pounds(m.Price)
	if g.owned[g.shopSel] {
		action = "ENTER = drive this truck"
		if g.shopSel == g.cur {
			action = "(currently driving)"
		}
	}
	drawText(screen, action, faceMed, dx+30, 530, rgb(255, 210, 60), text.AlignStart)
	drawText(screen, fmt.Sprintf("R = respray (%s)", pounds(paintPrice)), faceMed, dx+620, 530, rgb(255, 210, 60), text.AlignEnd)
	drawText(screen, "Up/Down = choose truck    Left/Right = choose paint    Esc = leave showroom", faceSmall, screenW/2, 610, color.RGBA{220, 220, 220, 255}, text.AlignCenter)
	if g.shopMsg != "" {
		drawTextShadow(screen, g.shopMsg, faceMed, screenW/2, 650, rgb(120, 240, 120), text.AlignCenter)
	}
}

func (g *Game) cabColorPreview(m int) color.RGBA { return g.cabColor(m) }

func (g *Game) cabColorPreviewPaint(m, p int) color.RGBA {
	if p > 0 {
		return paints[p].C
	}
	return models[m].Color
}

var truckPreviewCache = map[[4]int]*ebiten.Image{}

func makeTruckImgCached(c color.RGBA, model int) *ebiten.Image {
	k := [4]int{int(c.R), int(c.G), int(c.B), model}
	if img, ok := truckPreviewCache[k]; ok {
		return img
	}
	img := makeTruckImg(c, nil, model)
	truckPreviewCache[k] = img
	return img
}

func (g *Game) Layout(w, h int) (int, int) { return screenW, screenH }

func main() {
	ebiten.SetWindowSize(screenW, screenH)
	ebiten.SetWindowTitle("Truck Haul")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	if err := ebiten.RunGame(guarded{NewGame()}); err != nil {
		if runWithSoundFallback(err) {
			return
		}
		log.Fatal(err)
	}
}

// gameVersion is shown on the title screen and checked between online players.
const gameVersion = "v5.7"

// smoothShapes turns on anti-aliasing; it's expensive, so only the HUD dials use it.
var smoothShapes = false

package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// ---------- day / night cycle ----------

// Two clock modes (K toggles):
//
//	fast: a whole day passes in 24 real minutes (1 game hour per real minute)
//	real: the game clock is your PC's real UK time
//
// Either way the date is today's real date, and sunrise / sunset are the real
// times for that date in the UK (GMT / BST). Fast time is worked out from the real
// clock too, so everyone playing online sees the same time of day.
const dayLength = 24 * 60.0

var ukZone = func() *time.Location {
	if loc, err := time.LoadLocation("Europe/London"); err == nil {
		return loc
	}
	return time.UTC
}()

func (g *Game) gameTime() time.Time {
	now := time.Now().In(ukZone)
	if g.fixedHour >= 0 {
		d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, ukZone)
		return d.Add(time.Duration(g.fixedHour * float64(time.Hour)))
	}
	if g.realClock {
		return now
	}
	sec := float64(time.Now().UnixMilli()%int64(dayLength*1000)) / 1000
	d := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, ukZone)
	return d.Add(time.Duration(sec / dayLength * 24 * float64(time.Hour)))
}

func (g *Game) gameHour() float64 {
	t := g.gameTime()
	return float64(t.Hour()) + float64(t.Minute())/60 + float64(t.Second())/3600
}

// sunTimes returns sunrise and sunset (local UK hours) for the given date, using the
// NOAA solar position formulas for central London.
func sunTimes(t time.Time) (rise, set float64) {
	const lat, lon = 51.5074, -0.1278
	gamma := 2 * math.Pi / 365 * float64(t.YearDay()-1)
	eqt := 229.18 * (0.000075 + 0.001868*math.Cos(gamma) - 0.032077*math.Sin(gamma) -
		0.014615*math.Cos(2*gamma) - 0.040849*math.Sin(2*gamma))
	decl := 0.006918 - 0.399912*math.Cos(gamma) + 0.070257*math.Sin(gamma) - 0.006758*math.Cos(2*gamma) +
		0.000907*math.Sin(2*gamma) - 0.002697*math.Cos(3*gamma) + 0.00148*math.Sin(3*gamma)
	la := lat * math.Pi / 180
	ha := math.Acos(math.Cos(90.833*math.Pi/180)/(math.Cos(la)*math.Cos(decl)) - math.Tan(la)*math.Tan(decl))
	haDeg := ha * 180 / math.Pi
	riseUTC := 720 - 4*(lon+haDeg) - eqt
	setUTC := 720 - 4*(lon-haDeg) - eqt
	noon := time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, ukZone)
	_, off := noon.Zone() // 0 in winter (GMT), 3600 in summer (BST)
	return (riseUTC + float64(off)/60) / 60, (setUTC + float64(off)/60) / 60
}

// darkness: 0 in daylight, up to ~0.78 at night, fading through dawn and dusk twilight.
func (g *Game) darkness() float64 {
	h := g.gameHour()
	rise, set := sunTimes(g.gameTime())
	const night = 0.78
	switch {
	case h < rise-0.6 || h > set+0.7:
		return night
	case h < rise+0.4:
		return night * (1 - smooth01((h-(rise-0.6))/1.0))
	case h > set-0.4:
		return night * smooth01((h-(set-0.4))/1.1)
	}
	return 0
}

var (
	lightTex *ebiten.Image // soft round pool of light
	coneTex  *ebiten.Image // headlight beam pointing along +x
	nightImg *ebiten.Image
)

func initLightTextures() {
	const r = 128
	img := image.NewRGBA(image.Rect(0, 0, 2*r, 2*r))
	for y := 0; y < 2*r; y++ {
		for x := 0; x < 2*r; x++ {
			d := math.Hypot(float64(x-r)+0.5, float64(y-r)+0.5) / r
			a := 0.0
			if d < 1 {
				a = math.Pow(1-d, 1.6)
			}
			v := uint8(a * 255)
			img.SetRGBA(x, y, color.RGBA{v, v, v, v})
		}
	}
	lightTex = ebiten.NewImageFromImage(img)
	const L, H = 256, 160
	cone := image.NewRGBA(image.Rect(0, 0, L, H))
	for y := 0; y < H; y++ {
		for x := 0; x < L; x++ {
			dx, dy := float64(x)+0.5, float64(y-H/2)+0.5
			ang := math.Abs(math.Atan2(dy, dx))
			a := 0.0
			if ang < 0.42 {
				a = (1 - dx/L) * (1 - math.Pow(ang/0.42, 2))
				if dx < 12 {
					a *= dx / 12
				}
			}
			if a < 0 {
				a = 0
			}
			v := uint8(math.Min(1, a*1.3) * 255)
			cone.SetRGBA(x, y, color.RGBA{v, v, v, v})
		}
	}
	coneTex = ebiten.NewImageFromImage(cone)
	nightImg = ebiten.NewImage(screenW, screenH)
}

// buildLamps puts lamp posts along town and link roads, alternating sides.
func (g *Game) buildLamps() {
	g.lamps, g.yardLamps = nil, nil
	for _, rd := range g.roads {
		if rd.City < 0 && rd.Class != clLink {
			continue
		}
		side := 1.0
		for s := g.nodes[rd.A].R + 90; s < rd.Len-g.nodes[rd.B].R-60; s += 230 {
			x, y, tx, ty := rd.at(s)
			lx, ly := ty*side, -tx*side
			px, py := x+lx*(cityHalf+16), y+ly*(cityHalf+16)
			if k, ok := g.tileAt(px, py); ok && !k.solid() {
				g.lamps = append(g.lamps, [4]float64{px, py, -lx, -ly})
			}
			side = -side
		}
	}
	// floodlights over forecourts and loading yards
	for _, p := range places {
		x, y := p.bayCenter()
		g.yardLamps = append(g.yardLamps, [2]float64{x, y})
	}
}

func (g *Game) drawLampPosts(img *ebiten.Image, ox, oy float64) {
	for _, l := range g.lamps {
		if l[0] < ox-20 || l[1] < oy-20 || l[0] > ox+chunkSz+20 || l[1] > oy+chunkSz+20 {
			continue
		}
		x, y := l[0]-ox, l[1]-oy
		strokeLine(img, x, y, x+l[2]*20, y+l[3]*20, 3, rgb(70, 70, 75))
		fillCircle(img, x, y, 4, rgb(60, 60, 65))
		fillCircle(img, x+l[2]*20, y+l[3]*20, 4, rgb(200, 200, 190))
	}
}

func drawLight(dst, tex *ebiten.Image, x, y, angle, sx, sy, alpha float64, blend ebiten.Blend, tint color.RGBA) {
	w, h := float64(tex.Bounds().Dx()), float64(tex.Bounds().Dy())
	op := &ebiten.DrawImageOptions{Blend: blend}
	if tex == coneTex {
		op.GeoM.Translate(0, -h/2)
	} else {
		op.GeoM.Translate(-w/2, -h/2)
	}
	op.GeoM.Scale(sx, sy)
	op.GeoM.Rotate(angle)
	op.GeoM.Translate(x, y)
	op.ColorScale.Scale(float32(tint.R)/255, float32(tint.G)/255, float32(tint.B)/255, 1)
	op.ColorScale.ScaleAlpha(float32(alpha))
	op.Filter = ebiten.FilterLinear
	dst.DrawImage(tex, op)
}

// drawNight darkens the world and cuts holes for every light, then adds a warm glow.
func (g *Game) drawNight(screen *ebiten.Image, cx, cy float64) {
	d := g.darkness()
	if d < 0.01 {
		return
	}
	if lightTex == nil {
		initLightTextures()
	}
	nightImg.Fill(color.NRGBA{6, 10, 32, uint8(d * 255)})
	erase := ebiten.BlendDestinationOut
	white := rgb(255, 255, 255)
	vis := func(x, y, m float64) bool {
		return x-cx > -m && y-cy > -m && x-cx < screenW+m && y-cy < screenH+m
	}
	type glow struct {
		tex        *ebiten.Image
		x, y, a    float64
		sx, sy, al float64
		tint       color.RGBA
	}
	var glows []glow
	for _, l := range g.lamps {
		if !vis(l[0], l[1], 200) {
			continue
		}
		hx, hy := l[0]+l[2]*22-cx, l[1]+l[3]*22-cy
		drawLight(nightImg, lightTex, hx, hy, 0, 1.05, 1.05, 0.8, erase, white)
		glows = append(glows, glow{lightTex, hx, hy, 0, 1.2, 1.2, 0.22, rgb(255, 190, 110)})
	}
	for _, l := range g.yardLamps {
		if vis(l[0], l[1], 300) {
			drawLight(nightImg, lightTex, l[0]-cx, l[1]-cy, 0, 2.2, 2.2, 0.9, erase, white)
			glows = append(glows, glow{lightTex, l[0] - cx, l[1] - cy, 0, 2.0, 2.0, 0.12, rgb(230, 240, 255)})
		}
	}
	headlights := func(x, y, hx, hy, half, width, length, power float64) {
		ang := math.Atan2(hy, hx)
		rx, ry := -hy, hx
		for _, side := range []float64{-1, 1} {
			fx := x + hx*(half-2) + rx*side*(width/2-5)
			fy := y + hy*(half-2) + ry*side*(width/2-5)
			drawLight(nightImg, coneTex, fx-cx, fy-cy, ang, length/256, 0.9, power, erase, white)
		}
		drawLight(nightImg, lightTex, x+hx*(half+20)-cx, y+hy*(half+20)-cy, 0, 0.35, 0.35, 0.7*power, erase, white)
		glows = append(glows, glow{coneTex, x + hx*half - cx, y + hy*half - cy, ang, length / 256, 1.3, 0.14 * power, rgb(255, 245, 210)})
	}
	for _, c := range g.cars {
		if !vis(c.x, c.y, 400) {
			continue
		}
		hx, hy := c.dhx, c.dhy
		if hx == 0 && hy == 0 {
			hx, hy = c.hx, c.hy
		}
		headlights(c.x, c.y, hx, hy, c.half, c.width, 230, 0.85)
		// tail lights
		glows = append(glows, glow{lightTex, c.x - hx*c.half - cx, c.y - hy*c.half - cy, 0, 0.12, 0.12, 0.8, rgb(255, 40, 30)})
	}
	for _, r := range g.remotes() {
		if vis(r.dx, r.dy, 400) {
			headlights(r.dx, r.dy, math.Cos(r.da), math.Sin(r.da), 44, 34, 330, 1)
		}
	}
	t := g.truck
	if g.state != stTitle {
		headlights(t.X, t.Y, math.Cos(t.Angle), math.Sin(t.Angle), 44, 34, 380, 1)
		drawLight(nightImg, lightTex, t.X-cx, t.Y-cy, 0, 0.9, 0.9, 0.35, erase, white) // a little ambient around you
	}
	// traffic lights glow in their own colour
	for id, n := range g.nodes {
		if n.Ctrl != ctrlLights || !vis(n.X, n.Y, 200) {
			continue
		}
		for _, a := range n.Appr {
			col := lightColor(g.light(id, a.Axis))
			drawLight(nightImg, lightTex, a.SignX-cx, a.SignY-cy, 0, 0.35, 0.35, 0.8, erase, white)
			glows = append(glows, glow{lightTex, a.SignX - cx, a.SignY - cy, 0, 0.5, 0.5, 0.5, col})
		}
	}
	screen.DrawImage(nightImg, nil)
	for _, gl := range glows {
		drawLight(screen, gl.tex, gl.x, gl.y, gl.a, gl.sx, gl.sy, gl.al, ebiten.BlendLighter, gl.tint)
	}
	// lamps that should stay bright on top of the darkness
	g.drawLights(screen, cx, cy)
	for _, c := range g.cars {
		if vis(c.x, c.y, 100) {
			g.drawCarLamps(screen, c, cx, cy)
		}
	}
	tb := t.Speed > 5 && pressed(ebiten.KeyS, ebiten.KeyArrowDown, ebiten.KeySpace)
	if g.state != stTitle {
		g.drawLamps(screen, t.X-cx, t.Y-cy, math.Cos(t.Angle), math.Sin(t.Angle), 44, 34, g.ind, tb)
	}
}

// drawClock is the digital clock above the dashboard: time, date, sunrise and sunset.
func (g *Game) drawClock(screen *ebiten.Image, x, y float64) {
	t := g.gameTime()
	rise, set := sunTimes(t)
	hm := func(h float64) string { return fmt.Sprintf("%02d:%02d", int(h), int((h-math.Floor(h))*60)) }
	panel(screen, x, y, 380, 34)
	// digital readout
	fillRect(screen, x+6, y+4, 92, 26, rgb(10, 14, 12))
	colon := ":"
	if t.Second()%2 == 1 && g.realClock {
		colon = " "
	}
	drawText(screen, fmt.Sprintf("%02d%s%02d", t.Hour(), colon, t.Minute()), faceMed, x+52, y+5, rgb(90, 255, 140), text.AlignCenter)
	// sun or moon
	ix := x + 114.0
	if g.darkness() > 0.3 {
		fillCircle(screen, ix, y+17, 8, rgb(230, 230, 210))
		fillCircle(screen, ix+4, y+14, 7, rgb(14, 16, 26))
	} else {
		fillCircle(screen, ix, y+17, 6, rgb(255, 210, 60))
		for k := 0; k < 8; k++ {
			a := float64(k) * math.Pi / 4
			strokeLine(screen, ix+math.Cos(a)*8, y+17+math.Sin(a)*8, ix+math.Cos(a)*11, y+17+math.Sin(a)*11, 2, rgb(255, 210, 60))
		}
	}
	mode := "fast"
	if g.realClock {
		mode = "real"
	}
	drawText(screen, t.Format("Mon 2 Jan"), faceSign, x+132, y+3, color.White, text.AlignStart)
	drawText(screen, "Sunrise "+hm(rise)+"  Sunset "+hm(set), faceSign, x+132, y+17, rgb(255, 210, 120), text.AlignStart)
	drawText(screen, "K: "+mode+" time", faceSign, x+374, y+10, color.RGBA{170, 170, 180, 255}, text.AlignEnd)
}

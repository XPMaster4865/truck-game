package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type saveOffence struct {
	Age float64 `json:"age"`
	Pts int     `json:"pts"`
}

type SaveData struct {
	Version    int           `json:"version"`
	Money      int           `json:"money"`
	Deliveries int           `json:"deliveries"`
	FinesTotal int           `json:"finesTotal"`
	Owned      []bool        `json:"owned"`
	Paint      []int         `json:"paint"`
	Fuel       []float64     `json:"fuel"`
	Cur        int           `json:"cur"`
	X          float64       `json:"x"`
	Y          float64       `json:"y"`
	A          float64       `json:"a"`
	Offences   []saveOffence `json:"offences"`
}

func savePath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "TruckHaul", "save.json")
}

func saveExists() bool {
	_, err := os.Stat(savePath())
	return err == nil
}

func (g *Game) save() {
	if g.noSave {
		return
	}
	d := SaveData{Version: 3, Money: g.money, Deliveries: g.deliveries, FinesTotal: g.finesTotal,
		Owned: g.owned, Paint: g.paint, Fuel: g.fuel, Cur: g.cur, X: g.truck.X, Y: g.truck.Y, A: g.truck.Angle}
	for _, o := range g.offences {
		if g.clock-o.t < 360 {
			d.Offences = append(d.Offences, saveOffence{g.clock - o.t, o.pts})
		}
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return
	}
	p := savePath()
	os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, p)
	}
	g.hasSave = true
}

func (g *Game) load() bool {
	b, err := os.ReadFile(savePath())
	if err != nil {
		return false
	}
	var d SaveData
	if json.Unmarshal(b, &d) != nil {
		return false
	}
	g.reset()
	g.money, g.deliveries, g.finesTotal = d.Money, d.Deliveries, d.FinesTotal
	for i := range models {
		if i < len(d.Owned) {
			g.owned[i] = d.Owned[i]
		}
		if i < len(d.Paint) && d.Paint[i] < len(paints) {
			g.paint[i] = d.Paint[i]
		}
		if i < len(d.Fuel) {
			g.fuel[i] = d.Fuel[i]
		}
	}
	g.owned[0] = true
	if d.Cur >= 0 && d.Cur < len(models) && g.owned[d.Cur] {
		g.cur = d.Cur
	}
	if d.X > 0 && d.Y > 0 && d.X < worldW && d.Y < worldH && !g.truckHits(d.X, d.Y, d.A) {
		g.truck.X, g.truck.Y, g.truck.Angle = d.X, d.Y, d.A
		g.camX, g.camY = d.X-screenW/2, d.Y-screenH/2
		for _, c := range g.cars {
			if !g.isMotor(c.a, c.b) && c.x > g.truck.X-900 && c.x < g.truck.X+900 && c.y > g.truck.Y-600 && c.y < g.truck.Y+600 {
				g.respawnCar(c)
			}
		}
	}
	for _, o := range d.Offences {
		g.offences = append(g.offences, Offence{g.clock - o.Age, o.Pts})
	}
	return true
}

func deleteSave() { os.Remove(savePath()) }

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// ---------- online multiplayer (client side) ----------

type netPlayer struct {
	ID    int     `json:"id"`
	Name  string  `json:"name"`
	Color [3]int  `json:"color"`
	Model int     `json:"model"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	A     float64 `json:"a"`
	S     float64 `json:"s"`
	Ind   int     `json:"ind"`
	Cargo int     `json:"cargo"`
	Horn  bool    `json:"horn"`
	Ver   string  `json:"ver"`
}

type netMsg struct {
	T string `json:"t"`
	netPlayer
	Msg     string       `json:"msg,omitempty"`
	From    string       `json:"from,omitempty"`
	Players []*netPlayer `json:"players,omitempty"`
}

// Remote is another player's truck as drawn on this PC (smoothed between updates).
type Remote struct {
	netPlayer
	dx, dy, da float64
	init       bool
}

type chatLine struct {
	from, msg string
	at        time.Time
}

type Net struct {
	mu      sync.Mutex
	url     string
	name    string
	status  string
	online  bool
	myID    int
	players map[int]*Remote
	chat    []chatLine
	send    chan []byte
	cancel  context.CancelFunc
}

// normalizeURL turns "https://x.onrender.com" or "x.onrender.com" into "wss://x.onrender.com/ws".
func normalizeURL(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "/")
	switch {
	case strings.HasPrefix(s, "https://"):
		s = "wss://" + strings.TrimPrefix(s, "https://")
	case strings.HasPrefix(s, "http://"):
		s = "ws://" + strings.TrimPrefix(s, "http://")
	case strings.HasPrefix(s, "ws://"), strings.HasPrefix(s, "wss://"):
	default:
		if strings.Contains(s, "localhost") || strings.HasPrefix(s, "127.") || strings.HasPrefix(s, "192.168.") {
			s = "ws://" + s
		} else {
			s = "wss://" + s
		}
	}
	if !strings.HasSuffix(s, "/ws") {
		s += "/ws"
	}
	return s
}

func startNet(server, name string, col color.RGBA, model int) *Net {
	n := &Net{url: normalizeURL(server), name: name, players: map[int]*Remote{}, send: make(chan []byte, 32),
		status: "Connecting..."}
	ctx, cancel := context.WithCancel(context.Background())
	n.cancel = cancel
	go n.run(ctx, col, model)
	return n
}

func (n *Net) addChat(from, msg string) {
	n.chat = append(n.chat, chatLine{from, msg, time.Now()})
	if len(n.chat) > 50 {
		n.chat = n.chat[len(n.chat)-50:]
	}
}

func (n *Net) setStatus(s string, online bool) {
	n.mu.Lock()
	n.status, n.online = s, online
	n.mu.Unlock()
}

func (n *Net) run(ctx context.Context, col color.RGBA, model int) {
	for attempt := 0; ctx.Err() == nil; attempt++ {
		if attempt == 0 {
			n.setStatus("Connecting... (a sleeping free server can take up to a minute to wake)", false)
		} else {
			n.setStatus(fmt.Sprintf("Reconnecting... (try %d)", attempt+1), false)
		}
		dctx, dc := context.WithTimeout(ctx, 75*time.Second)
		conn, _, err := websocket.Dial(dctx, n.url, nil)
		dc()
		if err != nil {
			n.setStatus("Can't reach server - retrying... ("+shortErr(err)+")", false)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		conn.SetReadLimit(1 << 20)
		n.session(ctx, conn, col, model)
		conn.Close(websocket.StatusNormalClosure, "")
		n.mu.Lock()
		n.players = map[int]*Remote{}
		n.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		n.setStatus("Disconnected - retrying...", false)
		time.Sleep(3 * time.Second)
	}
}

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 60 {
		s = s[:60] + "..."
	}
	return s
}

func (n *Net) session(ctx context.Context, conn *websocket.Conn, col color.RGBA, model int) {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	hello, _ := json.Marshal(netMsg{T: "hello", netPlayer: netPlayer{Name: n.name, Ver: gameVersion, Color: [3]int{int(col.R), int(col.G), int(col.B)}, Model: model}})
	if conn.Write(sctx, websocket.MessageText, hello) != nil {
		return
	}
	n.setStatus("Online", true)
	go func() {
		for {
			select {
			case <-sctx.Done():
				return
			case b := <-n.send:
				wctx, wc := context.WithTimeout(sctx, 5*time.Second)
				err := conn.Write(wctx, websocket.MessageText, b)
				wc()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	for {
		rctx, rc := context.WithTimeout(sctx, 20*time.Second)
		_, data, err := conn.Read(rctx)
		rc()
		if err != nil {
			return
		}
		var m netMsg
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		n.mu.Lock()
		switch m.T {
		case "welcome":
			n.myID = m.ID
		case "chat":
			n.addChat(m.From, m.Msg)
		case "state":
			seen := map[int]bool{}
			for _, p := range m.Players {
				if p.ID == n.myID {
					continue
				}
				seen[p.ID] = true
				r := n.players[p.ID]
				if r == nil {
					r = &Remote{}
					n.players[p.ID] = r
					if p.Ver != "" && p.Ver != gameVersion {
						n.addChat("Server", fmt.Sprintf("Warning: %s is on %s, you're on %s - the roads won't match. Both get the latest TruckHaul.exe!", p.Name, p.Ver, gameVersion))
					}
				}
				r.netPlayer = *p
			}
			for id := range n.players {
				if !seen[id] {
					delete(n.players, id)
				}
			}
		}
		n.mu.Unlock()
	}
}

func (n *Net) stop() {
	if n != nil && n.cancel != nil {
		n.cancel()
	}
}

func (n *Net) sendMsg(m netMsg) {
	b, _ := json.Marshal(m)
	select {
	case n.send <- b:
	default:
	}
}

// ---------- game integration ----------

func (g *Game) netUpdate() {
	n := g.net
	if n == nil {
		return
	}
	g.netTick++
	if g.netTick%6 == 0 && g.state != stTitle {
		cargo := -1
		if g.phase == 2 {
			cargo = g.job.Src
		}
		c := g.cabColor(g.cur)
		n.sendMsg(netMsg{T: "pos", netPlayer: netPlayer{X: g.truck.X, Y: g.truck.Y, A: g.truck.Angle, S: g.truck.Speed,
			Ind: g.ind, Cargo: cargo, Horn: g.hornOn, Color: [3]int{int(c.R), int(c.G), int(c.B)}, Model: g.cur}})
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	k := math.Min(1, dt*8)
	for _, r := range n.players {
		// predict a little ahead using their speed, then glide towards it
		tx := r.X + math.Cos(r.A)*r.S*0.05
		ty := r.Y + math.Sin(r.A)*r.S*0.05
		if !r.init || math.Hypot(tx-r.dx, ty-r.dy) > 600 {
			r.dx, r.dy, r.da, r.init = tx, ty, r.A, true
		}
		r.dx += (tx - r.dx) * k
		r.dy += (ty - r.dy) * k
		r.da += math.Remainder(r.A-r.da, 2*math.Pi) * k
		if r.Horn && math.Hypot(r.dx-g.truck.X, r.dy-g.truck.Y) < 900 {
			g.aiHonk, g.aiHonkVol = 0.15, 0.8
		}
	}
}

// remotes returns a copy of the other players for drawing / collisions.
func (g *Game) remotes() []Remote {
	if g.net == nil {
		return nil
	}
	g.net.mu.Lock()
	defer g.net.mu.Unlock()
	out := make([]Remote, 0, len(g.net.players))
	for _, r := range g.net.players {
		if r.init {
			out = append(out, *r)
		}
	}
	return out
}

var remoteImgs = map[[5]int]*ebiten.Image{}

func (g *Game) drawRemotes(screen *ebiten.Image, cx, cy float64) {
	for _, r := range g.remotes() {
		sx, sy := r.dx-cx, r.dy-cy
		if sx < -150 || sy < -150 || sx > screenW+150 || sy > screenH+150 {
			continue
		}
		key := [5]int{r.Color[0], r.Color[1], r.Color[2], r.Model, r.Cargo}
		img := remoteImgs[key]
		if img == nil {
			var cargo *color.RGBA
			if r.Cargo >= 0 && r.Cargo < len(places) {
				c := places[r.Cargo].Color
				cargo = &c
			}
			img = makeTruckImg(rgb(uint8(r.Color[0]), uint8(r.Color[1]), uint8(r.Color[2])), cargo, r.Model)
			remoteImgs[key] = img
		}
		drawRot(screen, img, sx, sy, r.da)
		g.drawLamps(screen, sx, sy, math.Cos(r.da), math.Sin(r.da), 44, 34, r.Ind, false)
		drawTextShadow(screen, r.Name, faceSign, sx, sy-46, rgb(255, 255, 160), text.AlignCenter)
	}
}

// remoteBump keeps your truck from driving through your friends' trucks.
func (g *Game) remoteBump() {
	t := &g.truck
	for _, r := range g.remotes() {
		if math.Abs(r.dx-t.X) > 120 || math.Abs(r.dy-t.Y) > 120 {
			continue
		}
		cs, sn := math.Cos(r.da), math.Sin(r.da)
		for _, p := range g.truckCircles() {
			for _, off := range []float64{-28, 0, 28} {
				qx, qy := r.dx+cs*off, r.dy+sn*off
				if math.Hypot(p[0]-qx, p[1]-qy) < 32 {
					fx, fy := math.Cos(t.Angle), math.Sin(t.Angle)
					if ((qx-t.X)*fx+(qy-t.Y)*fy)*t.Speed > 0 {
						g.crash(math.Abs(t.Speed))
						t.Speed = -t.Speed * 0.3
					}
					return
				}
			}
		}
	}
}

func (g *Game) drawRemotesOnMap(dst *ebiten.Image, ox, oy, s float64, labels bool) {
	for _, r := range g.remotes() {
		x, y := ox+r.dx*s, oy+r.dy*s
		c := rgb(uint8(r.Color[0]), uint8(r.Color[1]), uint8(r.Color[2]))
		fillCircle(dst, x, y, 6, color.Black)
		fillCircle(dst, x, y, 4.5, c)
		if labels {
			drawTextShadow(dst, r.Name, faceTiny, x, y+6, rgb(255, 255, 160), text.AlignCenter)
		}
	}
}

// ---------- chat ----------

func (g *Game) updateChatInput() {
	g.chatBuf += string(ebiten.AppendInputChars(nil))
	if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) && len(g.chatBuf) > 0 {
		r := []rune(g.chatBuf)
		g.chatBuf = string(r[:len(r)-1])
	}
	if pasteKey() {
		g.chatBuf += clipboardText()
	}
	if len(g.chatBuf) > 120 {
		g.chatBuf = g.chatBuf[:120]
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		g.typing = false
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter) {
		if strings.TrimSpace(g.chatBuf) != "" && g.net != nil {
			g.net.sendMsg(netMsg{T: "chat", Msg: g.chatBuf})
		}
		g.typing = false
	}
}

func pasteKey() bool {
	return (ebiten.IsKeyPressed(ebiten.KeyControl) || ebiten.IsKeyPressed(ebiten.KeyMeta)) && inpututil.IsKeyJustPressed(ebiten.KeyV)
}

func (g *Game) drawNetHUD(screen *ebiten.Image) {
	n := g.net
	if n == nil {
		return
	}
	n.mu.Lock()
	status, online, count := n.status, n.online, len(n.players)
	lines := append([]chatLine(nil), n.chat...)
	n.mu.Unlock()
	// status pill (top right, under the news)
	txt := fmt.Sprintf("ONLINE  ·  %d other driver%s", count, map[bool]string{true: "", false: "s"}[count == 1])
	col := rgb(120, 240, 120)
	if !online {
		txt, col = status, rgb(255, 200, 80)
	}
	w := text.Advance(txt, faceSign) + 24
	panel(screen, screenW-w-12, 96, w, 24)
	drawText(screen, txt, faceSign, screenW-12-w/2, 100, col, text.AlignCenter)
	if online && !g.typing {
		drawText(screen, "T = chat", faceTiny, screenW-14, 124, color.RGBA{200, 200, 200, 200}, text.AlignEnd)
	}
	// chat (bottom left, above the dashboard)
	y := screenH - 200.0
	shown := 0
	for i := len(lines) - 1; i >= 0 && shown < 6; i-- {
		l := lines[i]
		age := time.Since(l.at).Seconds()
		if age > 15 && !g.typing {
			break
		}
		a := uint8(255)
		if !g.typing && age > 12 {
			a = uint8(255 * (15 - age) / 3)
		}
		nameCol := color.NRGBA{255, 220, 90, a}
		if l.from == "Server" {
			nameCol = color.NRGBA{140, 200, 255, a}
		}
		y -= 22
		fillRect(screen, 12, y-2, 470, 22, color.NRGBA{0, 0, 0, a / 2})
		drawText(screen, l.from+":", faceSmall, 18, y, nameCol, text.AlignStart)
		drawText(screen, l.msg, faceSmall, 26+text.Advance(l.from+":", faceSmall), y, color.NRGBA{255, 255, 255, a}, text.AlignStart)
		shown++
	}
	if g.typing {
		fillRect(screen, 12, screenH-198, 470, 26, color.NRGBA{20, 30, 60, 230})
		cursor := ""
		if math.Mod(g.clock, 1) < 0.5 {
			cursor = "|"
		}
		drawText(screen, "Say: "+g.chatBuf+cursor, faceSmall, 18, screenH-194, color.White, text.AlignStart)
	}
}

// ---------- online menu (title screen, press O) ----------

type netSettings struct {
	Server string `json:"server"`
	Name   string `json:"name"`
}

func settingsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "TruckHaul", "online.json")
}

func loadNetSettings() netSettings {
	var s netSettings
	if b, err := os.ReadFile(settingsPath()); err == nil {
		json.Unmarshal(b, &s)
	}
	return s
}

func saveNetSettings(s netSettings) {
	b, _ := json.MarshalIndent(s, "", "  ")
	os.MkdirAll(filepath.Dir(settingsPath()), 0o755)
	os.WriteFile(settingsPath(), b, 0o644)
}

func (g *Game) openOnlineMenu() {
	s := loadNetSettings()
	g.olServer, g.olName, g.olField, g.olMsg = s.Server, s.Name, 0, ""
	if g.olServer == "" {
		g.olField = 0
	} else if g.olName == "" {
		g.olField = 1
	}
	g.state = stOnline
}

func (g *Game) updateOnlineMenu() {
	field := &g.olServer
	if g.olField == 1 {
		field = &g.olName
	}
	*field += string(ebiten.AppendInputChars(nil))
	if pasteKey() {
		*field += strings.TrimSpace(clipboardText())
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) && len(*field) > 0 {
		r := []rune(*field)
		*field = string(r[:len(r)-1])
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyTab) || inpututil.IsKeyJustPressed(ebiten.KeyArrowDown) || inpututil.IsKeyJustPressed(ebiten.KeyArrowUp) {
		g.olField = 1 - g.olField
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		g.state = stTitle
		return
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter) {
		if strings.TrimSpace(g.olServer) == "" {
			g.olMsg, g.olField = "Paste your server address first (Ctrl+V).", 0
			return
		}
		if strings.TrimSpace(g.olName) == "" {
			g.olMsg, g.olField = "Type a driver name.", 1
			return
		}
		saveNetSettings(netSettings{Server: strings.TrimSpace(g.olServer), Name: strings.TrimSpace(g.olName)})
		g.net.stop()
		if !g.hasSave || !g.load() {
			g.reset()
		}
		g.net = startNet(g.olServer, strings.TrimSpace(g.olName), g.cabColor(g.cur), g.cur)
		g.state = stPlay
		g.flash("Joining the online world...")
	}
}

func (g *Game) drawOnlineMenu(screen *ebiten.Image) {
	fillRect(screen, 0, 0, screenW, screenH, color.RGBA{0, 0, 0, 225})
	drawTextShadow(screen, "PLAY ONLINE", faceTitle, screenW/2, 60, rgb(255, 210, 60), text.AlignCenter)
	drawText(screen, "Everyone must be on the same version ("+gameVersion+")", faceSmall, screenW/2, 215, rgb(255, 200, 120), text.AlignCenter)
	drawText(screen, "Drive in the same world as your friends. Everyone uses the same server address.", faceMed, screenW/2, 180, color.White, text.AlignCenter)
	box := func(label, val string, y float64, active bool) {
		drawText(screen, label, faceMed, 240, y, rgb(200, 200, 210), text.AlignStart)
		bc := color.Color(rgb(60, 60, 70))
		if active {
			bc = rgb(255, 210, 60)
		}
		fillRect(screen, 238, y+30, 804, 48, bc)
		fillRect(screen, 240, y+32, 800, 44, rgb(20, 22, 30))
		cur := ""
		if active && math.Mod(g.clock, 1) < 0.5 {
			cur = "|"
		}
		drawText(screen, val+cur, faceMed, 252, y+42, color.White, text.AlignStart)
	}
	box("Server address  (paste with Ctrl+V)", g.olServer, 250, g.olField == 0)
	box("Your driver name", g.olName, 370, g.olField == 1)
	drawText(screen, "Tab = switch box   ·   ENTER = join   ·   Esc = back", faceMed, screenW/2, 500, rgb(255, 210, 60), text.AlignCenter)
	drawText(screen, "The first join after a quiet spell can take up to a minute while the free server wakes up.", faceSmall, screenW/2, 545, color.RGBA{200, 200, 210, 255}, text.AlignCenter)
	if g.olMsg != "" {
		drawTextShadow(screen, g.olMsg, faceMed, screenW/2, 590, rgb(255, 120, 120), text.AlignCenter)
	}
}

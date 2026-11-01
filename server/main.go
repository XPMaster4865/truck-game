// Truck Haul multiplayer relay server.
//
// Every player connects over a WebSocket to /ws, says hello, then sends its
// truck position ~10 times a second. Ten times a second the server sends every
// player the list of everyone else. Chat messages are passed on to everyone.
// Nothing is stored on disk - money, jobs and saves stay on each player's PC.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Player struct {
	ID    int     `json:"id"`
	Name  string  `json:"name"`
	Color [3]int  `json:"color"`
	Model int     `json:"model"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	A     float64 `json:"a"`
	S     float64 `json:"s"`
	Ind   int     `json:"ind"`
	Cargo int     `json:"cargo"` // -1 = empty trailer, else index of the cargo's colour
	Horn  bool    `json:"horn"`
}

type msg struct {
	T string `json:"t"`
	Player
	Msg     string    `json:"msg,omitempty"`
	From    string    `json:"from,omitempty"`
	Players []*Player `json:"players,omitempty"`
}

type client struct {
	conn *websocket.Conn
	p    Player
	out  chan []byte
}

type hub struct {
	mu      sync.Mutex
	clients map[int]*client
	nextID  int
}

func (h *hub) broadcast(b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.clients {
		select {
		case c.out <- b:
		default: // slow client: drop this update rather than block everyone
		}
	}
}

func (h *hub) tick() {
	for range time.Tick(100 * time.Millisecond) {
		h.mu.Lock()
		list := make([]*Player, 0, len(h.clients))
		for _, c := range h.clients {
			p := c.p
			list = append(list, &p)
		}
		h.mu.Unlock()
		b, _ := json.Marshal(msg{T: "state", Players: list})
		h.broadcast(b)
	}
}

func clean(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = s[:n]
	}
	return s
}

func (h *hub) serve(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(4096)
	h.mu.Lock()
	if len(h.clients) >= 32 {
		h.mu.Unlock()
		conn.Close(websocket.StatusTryAgainLater, "server full")
		return
	}
	h.nextID++
	c := &client{conn: conn, out: make(chan []byte, 16)}
	c.p.ID = h.nextID
	c.p.Name = fmt.Sprintf("Driver %d", c.p.ID)
	c.p.Cargo = -1
	h.clients[c.p.ID] = c
	h.mu.Unlock()
	log.Printf("player %d connected (%s)", c.p.ID, r.RemoteAddr)

	ctx, cancel := context.WithCancel(r.Context())
	defer func() {
		cancel()
		h.mu.Lock()
		delete(h.clients, c.p.ID)
		name := c.p.Name
		h.mu.Unlock()
		conn.Close(websocket.StatusNormalClosure, "")
		b, _ := json.Marshal(msg{T: "chat", From: "Server", Msg: name + " left the game"})
		h.broadcast(b)
		log.Printf("player %d left", c.p.ID)
	}()

	welcome, _ := json.Marshal(msg{T: "welcome", Player: Player{ID: c.p.ID}})
	c.out <- welcome

	// writer
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case b := <-c.out:
				wctx, wc := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Write(wctx, websocket.MessageText, b)
				wc()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()

	// reader
	for {
		rctx, rc := context.WithTimeout(ctx, 30*time.Second)
		_, data, err := conn.Read(rctx)
		rc()
		if err != nil {
			return
		}
		var m msg
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		switch m.T {
		case "hello":
			h.mu.Lock()
			c.p.Name = clean(m.Name, 20)
			if c.p.Name == "" {
				c.p.Name = fmt.Sprintf("Driver %d", c.p.ID)
			}
			c.p.Color, c.p.Model = m.Color, m.Model
			name := c.p.Name
			h.mu.Unlock()
			b, _ := json.Marshal(msg{T: "chat", From: "Server", Msg: name + " joined the game"})
			h.broadcast(b)
		case "pos":
			h.mu.Lock()
			id, name := c.p.ID, c.p.Name
			c.p = m.Player
			c.p.ID, c.p.Name = id, name
			h.mu.Unlock()
		case "chat":
			h.mu.Lock()
			name := c.p.Name
			h.mu.Unlock()
			if t := clean(m.Msg, 120); t != "" {
				b, _ := json.Marshal(msg{T: "chat", From: name, Msg: t})
				h.broadcast(b)
			}
		}
	}
}

func main() {
	h := &hub{clients: map[int]*client{}}
	go h.tick()
	http.HandleFunc("/ws", h.serve)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		n := len(h.clients)
		h.mu.Unlock()
		fmt.Fprintf(w, "Truck Haul server is running. Players online: %d\n", n)
	})
	port := os.Getenv("PORT") // Render tells us which port to use
	if port == "" {
		port = "8080"
	}
	log.Printf("Truck Haul server listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

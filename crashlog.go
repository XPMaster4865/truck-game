package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

// guarded wraps the game so any crash is written to crash.log next to the save file.
type guarded struct{ *Game }

func writeCrash(where string, r interface{}) {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	p := filepath.Join(dir, "TruckHaul", "crash.log")
	os.MkdirAll(filepath.Dir(p), 0o755)
	msg := fmt.Sprintf("Truck Haul crashed in %s at %s\n%v\n\n%s", where, time.Now().Format(time.RFC1123), r, debug.Stack())
	os.WriteFile(p, []byte(msg), 0o644)
}

func (g guarded) Update() (err error) {
	defer func() {
		if r := recover(); r != nil {
			writeCrash("Update", r)
			panic(r)
		}
	}()
	return g.Game.Update()
}

func (g guarded) Draw(s *ebiten.Image) {
	defer func() {
		if r := recover(); r != nil {
			writeCrash("Draw", r)
			panic(r)
		}
	}()
	g.Game.Draw(s)
}

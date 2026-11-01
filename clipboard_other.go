//go:build !windows

package main

import (
	"os/exec"
	"strings"
)

// clipboardText: best effort on Linux / macOS.
func clipboardText() string {
	for _, cmd := range [][]string{{"pbpaste"}, {"xclip", "-o", "-selection", "clipboard"}, {"wl-paste", "-n"}} {
		if out, err := exec.Command(cmd[0], cmd[1:]...).Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}

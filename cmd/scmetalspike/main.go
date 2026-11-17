//go:build darwin

// Command scmetalspike is a throwaway P0 check, not part of ScottiCAD. It
// opens a Metal-backed Cocoa window and paints it a colour that drifts over
// time, printing the drawable's pixel size whenever it changes.
//
// Build and run it on a Mac to confirm that cgo, the ObjC host, the main
// thread and Retina sizing all work before the real darwin window is built on
// top of internal/mac:
//
//	go run ./cmd/scmetalspike
//
// A window should appear and slowly change colour; resizing it and moving it
// between a Retina and a non-Retina display should print new pixel sizes.
package main

import (
	"math"
	"runtime"
	"time"

	"github.com/scotticad/scotticad/internal/mac"
)

func init() {
	// Cocoa runs on the process's main thread. Go does not promise the main
	// goroutine stays there, so pin it before anything else happens.
	runtime.LockOSThread()
}

func main() {
	start := time.Now()

	mac.OnResize = func(w, h int) {
		println("drawable:", w, "x", h, "px")
	}
	mac.OnDraw = func() {
		t := time.Since(start).Seconds()
		mac.Clear(
			0.5+0.5*math.Sin(t),
			0.5+0.5*math.Sin(t+2.094),
			0.5+0.5*math.Sin(t+4.188),
			1,
		)
	}

	mac.Run("ScottiCAD Metal spike", 1280, 800, true)
}

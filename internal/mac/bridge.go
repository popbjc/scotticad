//go:build darwin

// Package mac is the Cocoa/Metal host the darwin window sits on. It is the
// only package in the tree that uses cgo: sc_app.m creates the NSWindow and
// its Metal-backed view, and this file is the Go side of that conversation.
//
// The C side calls back into goDraw (once per frame) and goResize (when the
// drawable's pixel size changes), both on the main thread. Set OnDraw and
// OnResize to receive them.
package mac

/*
#cgo LDFLAGS: -framework Cocoa -framework Metal -framework MetalKit -framework QuartzCore
#include <stdlib.h>
#include "sc_app.h"
*/
import "C"

import "unsafe"

// OnDraw, if set, is called once per frame from inside goDraw, on the main
// thread.
var OnDraw func()

// OnResize, if set, is called when the drawable's size in pixels changes, on
// the main thread.
var OnResize func(w, h int)

//export goDraw
func goDraw() {
	if OnDraw != nil {
		OnDraw()
	}
}

//export goResize
func goResize(w, h C.int) {
	if OnResize != nil {
		OnResize(int(w), int(h))
	}
}

// Run creates the application and a window, and runs its event loop until the
// application terminates. It must be called from the main goroutine, locked to
// the main thread (see the LockOSThread note in cmd/scmetalspike).
func Run(title string, w, h int, visible bool) {
	t := C.CString(title)
	defer C.free(unsafe.Pointer(t))
	v := C.int(0)
	if visible {
		v = 1
	}
	C.sc_run(t, C.int(w), C.int(h), v)
}

// End terminates the application, so Run returns.
func End() {
	C.sc_end()
}

// SetTitle sets the window's title.
func SetTitle(title string) {
	t := C.CString(title)
	defer C.free(unsafe.Pointer(t))
	C.sc_set_title(t)
}

// Clear sets the colour the current frame is cleared to, components in 0..1.
// Call it from OnDraw.
func Clear(r, g, b, a float64) {
	C.sc_clear(C.double(r), C.double(g), C.double(b), C.double(a))
}

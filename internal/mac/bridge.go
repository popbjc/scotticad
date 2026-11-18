//go:build darwin

// Package mac is the Cocoa/Metal host the darwin window and renderers sit
// on. It is the only package in the tree that uses cgo (see metal.go for the
// renderer side). sc_app.m creates the NSWindow, its Metal-backed view and
// the Metal backend, and the files here are the Go side of that conversation.
//
// The C side calls back into the goExport* functions below, all on the main
// thread. Set the On* variables to receive them.

package mac

/*
#cgo LDFLAGS: -framework Cocoa -framework Metal -framework MetalKit -framework QuartzCore
#include <stdlib.h>
#include "sc_app.h"
*/
import "C"

import (
	"strings"
	"unsafe"
)

// On* are the callbacks the host invokes, all on the main thread.
var (
	// OnDraw is called once per requested frame.
	OnDraw func()
	// OnResize is called when the drawable's pixel size changes; scale is
	// the backing scale (2 on a Retina display).
	OnResize func(w, h int, scale float64)
	// OnIdle is called regularly (about 60 times a second) so the program
	// can do work between frames.
	OnIdle func()

	OnMouseMove  func(x, y float64)
	OnMouseDown  func(btn int, x, y float64, mods, clicks int)
	OnMouseUp    func(btn int, x, y float64, mods int)
	OnWheel      func(x, y, dx, dy float64)
	OnMouseLeave func()
	OnKeyDown    func(vk int, mods int, repeat bool)
	OnKeyUp      func(vk int, mods int)
	OnChar       func(r rune)
	OnDrop       func(paths []string, x, y float64)
	OnClose      func() bool
	OnCapture    func(lost bool)
)

//export goDraw
func goDraw() {
	if OnDraw != nil {
		OnDraw()
	}
}

//export goResize
func goResize(w, h C.int, scale C.double) {
	if OnResize != nil {
		OnResize(int(w), int(h), float64(scale))
	}
}

//export goEventIdle
func goEventIdle() {
	if OnIdle != nil {
		OnIdle()
	}
}

//export goEventMouseMove
func goEventMouseMove(x, y C.double) {
	if OnMouseMove != nil {
		OnMouseMove(float64(x), float64(y))
	}
}

//export goEventMouseDown
func goEventMouseDown(btn C.int, x, y C.double, mods C.int, clicks C.int) {
	if OnMouseDown != nil {
		OnMouseDown(int(btn), float64(x), float64(y), int(mods), int(clicks))
	}
}

//export goEventMouseUp
func goEventMouseUp(btn C.int, x, y C.double, mods C.int) {
	if OnMouseUp != nil {
		OnMouseUp(int(btn), float64(x), float64(y), int(mods))
	}
}

//export goEventWheel
func goEventWheel(x, y, dx, dy C.double) {
	if OnWheel != nil {
		OnWheel(float64(x), float64(y), float64(dx), float64(dy))
	}
}

//export goEventMouseLeave
func goEventMouseLeave() {
	if OnMouseLeave != nil {
		OnMouseLeave()
	}
}

//export goEventKeyDown
func goEventKeyDown(vk C.int, mods C.int, repeat C.int) {
	if OnKeyDown != nil {
		OnKeyDown(int(vk), int(mods), repeat != 0)
	}
}

//export goEventKeyUp
func goEventKeyUp(vk C.int, mods C.int) {
	if OnKeyUp != nil {
		OnKeyUp(int(vk), int(mods))
	}
}

//export goEventChar
func goEventChar(r C.int) {
	if OnChar != nil {
		OnChar(rune(r))
	}
}

//export goEventDrop
func goEventDrop(paths *C.char, x, y C.double) {
	if OnDrop == nil || paths == nil {
		return
	}
	s := C.GoString(paths)
	if s == "" {
		return
	}
	OnDrop(strings.Split(s, "\n"), float64(x), float64(y))
}

//export goEventClose
func goEventClose() C.int {
	if OnClose != nil && !OnClose() {
		return 0
	}
	return 1
}

//export goEventCapture
func goEventCapture(lost C.int) {
	if OnCapture != nil {
		OnCapture(lost != 0)
	}
}

// --- the host ---------------------------------------------------------------

// Start creates the application, its Metal device and a window with a
// Metal-backed view, without running anything. Call RunLoop afterwards to
// run the event loop. It must be called from the main goroutine, locked to
// the main thread (see the darwin window layer).
func Start(title string, w, h int, visible bool) {
	t := C.CString(title)
	defer C.free(unsafe.Pointer(t))
	v := C.int(0)
	if visible {
		v = 1
	}
	C.sc_start(t, C.int(w), C.int(h), v)
}

// RunLoop runs the application's event loop until the application
// terminates. It must be called from the main thread, after Start.
func RunLoop() { C.sc_runloop() }

// End terminates the application, so RunLoop returns.
func End() { C.sc_end() }

// SetTitle sets the window's title.
func SetTitle(title string) {
	t := C.CString(title)
	defer C.free(unsafe.Pointer(t))
	C.sc_set_title(t)
}

// Show orders the window to the front.
func Show() { C.sc_show() }

// SetMinSize sets the smallest content size, in logical points.
func SetMinSize(w, h int) { C.sc_set_min_size(C.int(w), C.int(h)) }

// RequestFrame asks the host to draw a frame now.
func RequestFrame() { C.sc_request_frame() }

// SetScale fixes the interface scale (a --scale override); 0 uses the
// display's own. Call it before Start.
func SetScale(scale float64) { C.sc_set_scale(C.double(scale)) }

// BackingScale is the window's backing scale (2 on a Retina display).
func BackingScale() float64 { return float64(C.sc_backing_scale()) }

// --- system services --------------------------------------------------------

// SetClipboard replaces the pasteboard's string.
func SetClipboard(s string) {
	c := C.CString(s)
	defer C.free(unsafe.Pointer(c))
	C.sc_set_clipboard(c)
}

// Clipboard returns the pasteboard's string.
func Clipboard() string {
	p := C.sc_get_clipboard()
	if p == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p)
}

// SetCursor sets the mouse cursor from a window.Cursor id.
func SetCursor(id int) { C.sc_set_cursor(C.int(id)) }

// SetIcon sets the application's Dock icon from a size x size RGBA image.
func SetIcon(rgba []byte, size int, big bool) {
	if len(rgba) == 0 {
		return
	}
	b := C.int(0)
	if big {
		b = 1
	}
	C.sc_set_icon(unsafe.Pointer(&rgba[0]), C.int(size), b)
}

// cstr returns a C string the caller frees, or nil for "".
func cstr(s string) *C.char {
	if s == "" {
		return nil
	}
	return C.CString(s)
}

// OpenFile shows an open dialog and returns the chosen path.
func OpenFile(title, filters string) (string, bool) {
	ct := cstr(title)
	cf := cstr(filters)
	defer C.free(unsafe.Pointer(ct))
	defer C.free(unsafe.Pointer(cf))
	p := C.sc_open_file(ct, cf)
	return takeString(p)
}

// SaveFile shows a save dialog and returns the chosen path.
func SaveFile(title, filters, defname string) (string, bool) {
	ct := cstr(title)
	cf := cstr(filters)
	cd := cstr(defname)
	defer C.free(unsafe.Pointer(ct))
	defer C.free(unsafe.Pointer(cf))
	defer C.free(unsafe.Pointer(cd))
	p := C.sc_save_file(ct, cf, cd)
	return takeString(p)
}

func takeString(p *C.char) (string, bool) {
	if p == nil {
		return "", false
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p), true
}

// Message shows a message box and returns the button pressed (a window.Msg*
// constant).
func Message(title, text string, kind, buttons int) int {
	ct := cstr(title)
	cx := cstr(text)
	defer C.free(unsafe.Pointer(ct))
	defer C.free(unsafe.Pointer(cx))
	return int(C.sc_message(ct, cx, C.int(kind), C.int(buttons)))
}

// OpenURL opens a URL with the default browser.
func OpenURL(url string) {
	cu := cstr(url)
	defer C.free(unsafe.Pointer(cu))
	C.sc_open_url(cu)
}

// AcceptDrop turns file-drop support on or off.
func AcceptDrop(on bool) {
	v := C.int(0)
	if on {
		v = 1
	}
	C.sc_accept_drop(v)
}

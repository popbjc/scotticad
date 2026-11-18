//go:build darwin

package window

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/scotticad/scotticad/internal/mac"
)

// Window is the single Cocoa window and its Metal-backed rendering surface.
type Window struct {
	h           Handler
	W, H        int
	DPI         float64
	FixedDPI    bool
	MinW, MinH  int
	Animate     bool
	OnPanic     func(r any)
	IgnoreInput bool
	quit        bool
	dirty       bool
	captured    bool
	panicSent   bool
	buttons     uint32
	running     bool
	mods        Mods
	cursorID    int
	clip        string
	icons       map[int][]byte
	keysDown    map[int]bool
}

// New creates the Cocoa window and Metal device. Width and height are logical
// points; all event coordinates and render dimensions are backing pixels.
func New(title string, width, height int, visible bool, handler Handler, scale float64) (*Window, error) {
	runtime.LockOSThread()
	w := &Window{
		h: handler, DPI: scale, FixedDPI: scale > 0, dirty: true,
		cursorID: -1, icons: make(map[int][]byte), keysDown: make(map[int]bool),
	}
	mac.SetScale(scale)
	mac.OnDraw = func() {
		if w.quit || w.h == nil {
			return
		}
		w.dirty = false
		w.safe(func() { w.h.Frame() })
	}
	mac.OnIdle = func() {
		if w.quit || w.h == nil {
			return
		}
		w.safe(func() { w.h.Idle() })
		if w.dirty || w.Animate {
			mac.RequestFrame()
		}
	}
	mac.OnResize = func(width, height int, actualScale float64) {
		if actualScale <= 0 {
			actualScale = 1
		}
		if !w.FixedDPI {
			w.DPI = actualScale
		}
		w.W, w.H = width, height
		if w.running && w.h != nil {
			w.safe(func() { w.h.Resize(width, height) })
		}
		w.Redraw()
	}
	mac.OnMouseMove = func(x, y float64) {
		if !w.IgnoreInput && w.h != nil {
			w.safe(func() { w.h.MouseMove(x, y, w.mods) })
		}
	}
	mac.OnMouseDown = func(btn int, x, y float64, mods, clicks int) {
		if !w.IgnoreInput && w.h != nil {
			w.buttonsSet(btn, true)
			w.mods = Mods(mods)
			w.safe(func() { w.h.MouseDown(btn, x, y, w.mods, clicks > 1) })
		}
	}
	mac.OnMouseUp = func(btn int, x, y float64, mods int) {
		if !w.IgnoreInput && w.h != nil {
			w.buttonsSet(btn, false)
			w.mods = Mods(mods)
			w.safe(func() { w.h.MouseUp(btn, x, y, w.mods) })
		}
	}
	mac.OnWheel = func(x, y, dx, dy float64) {
		if !w.IgnoreInput && w.h != nil {
			w.safe(func() { w.h.MouseWheel(dy, x, y, w.mods) })
		}
	}
	mac.OnMouseLeave = func() {
		if !w.IgnoreInput && w.h != nil {
			w.safe(w.h.MouseLeave)
		}
	}
	mac.OnKeyDown = func(vk int, mods int, repeat bool) {
		if !w.IgnoreInput && w.h != nil {
			w.keysDown[vk] = true
			w.mods = Mods(mods)
			w.safe(func() { w.h.KeyDown(vk, w.mods, repeat) })
		}
	}
	mac.OnKeyUp = func(vk int, mods int) {
		delete(w.keysDown, vk)
		if !w.IgnoreInput && w.h != nil {
			w.mods = Mods(mods)
			w.safe(func() { w.h.KeyUp(vk, w.mods) })
		}
	}
	mac.OnChar = func(r rune) {
		if !w.IgnoreInput && w.h != nil {
			w.safe(func() { w.h.Char(r) })
		}
	}
	mac.OnDrop = func(paths []string, x, y float64) {
		if !w.IgnoreInput && w.h != nil {
			w.safe(func() { w.h.DropFiles(paths, x, y) })
		}
	}
	mac.OnClose = func() bool {
		if w.h == nil {
			return true
		}
		allow := false
		w.safe(func() { allow = w.h.Close() })
		return allow
	}
	mac.OnCapture = func(lost bool) {
		if lost {
			wasCaptured := w.captured
			w.captured = false
			w.buttons = 0
			w.mods = 0
			if wasCaptured && w.h != nil {
				w.safe(w.h.CaptureLost)
			}
			for vk := range w.keysDown {
				delete(w.keysDown, vk)
				if w.h != nil && !w.quit {
					w.safe(func() { w.h.KeyUp(vk, 0) })
				}
			}
		}
	}
	mac.AcceptDrop(true)

	if width < 1 {
		width = 1280
	}
	if height < 1 {
		height = 800
	}
	mac.Start(title, width, height, visible)
	if !mac.DeviceReady() {
		return nil, errors.New("mac: Metal device initialization failed")
	}
	if w.DPI <= 0 {
		w.DPI = mac.BackingScale()
		if w.DPI <= 0 {
			w.DPI = 1
		}
	}
	return w, nil
}

func (w *Window) buttonsSet(btn int, down bool) {
	if btn < 0 || btn > 2 {
		return
	}
	bit := uint32(1 << btn)
	if down {
		w.buttons |= bit
	} else {
		w.buttons &^= bit
	}
}

func (w *Window) safe(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "panic: %v\n%s\n", r, debug.Stack())
			if w.OnPanic != nil && !w.panicSent {
				w.panicSent = true
				func() {
					defer func() { _ = recover() }()
					w.OnPanic(r)
				}()
			}
			w.quit = true
			mac.End()
		}
	}()
	fn()
}

func (w *Window) Handler(h Handler) { w.h = h }
func (w *Window) Redraw() {
	w.dirty = true
	mac.RequestFrame()
}
func (w *Window) Wake()                 { mac.RequestFrame() }
func (w *Window) SetTitle(title string) { mac.SetTitle(title) }
func (w *Window) Show()                 { mac.Show() }
func (w *Window) Quit() {
	w.quit = true
	mac.End()
}
func (w *Window) SetCursor(id int) {
	w.cursorID = id
	mac.SetCursor(id)
}
func (w *Window) Capture(on bool) { w.captured = on }
func (w *Window) Swap()           {}
func (w *Window) SetClipboard(s string) {
	w.clip = s
	mac.SetClipboard(s)
}
func (w *Window) Clipboard() string { return mac.Clipboard() }
func (w *Window) SetIcon(rgba []byte, size int, big bool) {
	w.icons[size] = append([]byte(nil), rgba...)
	mac.SetIcon(rgba, size, big)
}
func (w *Window) Run() {
	w.running = true
	if w.MinW > 0 || w.MinH > 0 {
		minW, minH := w.MinW, w.MinH
		if minW < 1 {
			minW = 1
		}
		if minH < 1 {
			minH = 1
		}
		mac.SetMinSize(minW, minH)
	}
	if w.h != nil {
		w.safe(func() { w.h.Resize(w.W, w.H) })
	}
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "panic: %v\n%s\n", r, debug.Stack())
			w.safe(func() {
				if w.OnPanic != nil && !w.panicSent {
					w.panicSent = true
					w.OnPanic(r)
				}
			})
		}
	}()
	mac.RunLoop()
}

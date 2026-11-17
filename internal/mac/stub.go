//go:build !darwin

// On non-darwin platforms the Cocoa/Metal host does not exist, so this stub
// keeps the package (and `go build ./...`) valid. Nothing calls it there.
package mac

// OnDraw, if set, is called once per frame.
var OnDraw func()

// OnResize, if set, is called when the drawable's size in pixels changes.
var OnResize func(w, h int)

// Run is a no-op off darwin.
func Run(title string, w, h int, visible bool) {}

// End is a no-op off darwin.
func End() {}

// SetTitle is a no-op off darwin.
func SetTitle(title string) {}

// Clear is a no-op off darwin.
func Clear(r, g, b, a float64) {}

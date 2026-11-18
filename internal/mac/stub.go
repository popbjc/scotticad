//go:build !darwin

// On non-darwin platforms the Cocoa/Metal host does not exist, so this stub
// keeps the package (and `go build ./...`) valid. Nothing calls it there.
package mac

// On* mirror the callbacks the darwin host invokes.
var (
	OnDraw       func()
	OnResize     func(w, h int, scale float64)
	OnIdle       func()
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

// --- the host ---------------------------------------------------------------

func Start(title string, w, h int, visible bool) {}
func RunLoop()                                   {}
func End()                                       {}
func SetTitle(title string)                      {}
func Show()                                      {}
func SetMinSize(w, h int)                        {}
func RequestFrame()                              {}
func SetScale(scale float64)                     {}
func BackingScale() float64                      { return 1 }

// --- system services --------------------------------------------------------

func SetClipboard(s string)               {}
func Clipboard() string                   { return "" }
func SetCursor(id int)                    {}
func SetIcon(rgba []byte, size int, big bool) {}
func OpenFile(title, filters string) (string, bool)      { return "", false }
func SaveFile(title, filters, defname string) (string, bool) { return "", false }
func Message(title, text string, kind, buttons int) int  { return 0 }
func OpenURL(url string)                  {}
func AcceptDrop(on bool)                  {}

package render

import (
	"image"

	"github.com/scotticad/scotticad/internal/mac"
)

// Target is an offscreen multisampled Metal target that is resolved to the
// window (or read back for screenshots).
type Target struct {
	W, H    int32
	Samples int32
	target  mac.Target
}

func NewTarget(samples int32) *Target { return &Target{Samples: samples} }

// Resize (re)allocates storage when the size changes.
func (t *Target) Resize(w, h int32) {
	// A minimized window is 0x0; Metal targets need positive dimensions.
	w, h = max(w, 1), max(h, 1)
	if w == t.W && h == t.H && t.target != 0 {
		return
	}
	t.Delete()
	// Clamp to the device's supported count, while keeping a valid minimum.
	if t.Samples < 1 {
		t.Samples = 1
	}
	if most := mac.TargetMaxSamples(); most > 0 && int(t.Samples) > most {
		t.Samples = int32(most)
	}
	t.target = mac.NewTarget(int(w), int(h), int(t.Samples), 1, false)
	t.W, t.H = int32(t.target.Width()), int32(t.target.Height())
}

// Handle returns the Metal target for renderer operations that accept it.
func (t *Target) Handle() mac.Target { return t.target }

func (t *Target) Bind() {
	mac.BindTarget(t.target)
	mac.Viewport(0, 0, int(t.W), int(t.H))
}

// BlitToWindow resolves the target to the window's drawable.
func (t *Target) BlitToWindow() {
	t.target.Blit()
}

// Read resolves and reads the rectangle (x, y from top-left) as an image.
func (t *Target) Read(x, y, w, h int32) *image.RGBA {
	if w <= 0 || h <= 0 {
		return image.NewRGBA(image.Rectangle{})
	}
	buf := make([]byte, int(w)*int(h)*4)
	t.target.Read(int(x), int(y), int(w), int(h), buf)
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	// The mac bridge converts Metal's BGRA readback to RGBA.
	copy(img.Pix, buf)
	return img
}

// Delete frees the Metal target.
func (t *Target) Delete() {
	if t.target == 0 {
		return
	}
	t.target.Free()
	t.target = 0
}

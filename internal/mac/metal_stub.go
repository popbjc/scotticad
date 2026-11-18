//go:build !darwin

// The Metal backend exists only on darwin; these stubs keep the package (and
// `go build ./...`) valid elsewhere. Nothing calls them there.
package mac

type (
	Pipeline uintptr
	Buffer   uintptr
	Texture  uintptr
)

func DeviceReady() bool                                     { return false }
func NewPipelineBatch(msl, vs, fs string) (Pipeline, error) { return 0, nil }
func (p Pipeline) Free()                                    {}
func NewBuffer(nbytes int) Buffer                           { return 0 }
func (b Buffer) Update(data []float32)                      {}
func (b Buffer) Free()                                      {}
func NewTextureRGBA8(w, h int) Texture                      { return 0 }
func (t Texture) Update(pix []byte, w, h, stride int)       {}
func (t Texture) Free()                                     {}
func FrameBegin() bool                                      { return false }
func FrameEnd()                                             {}
func DrawSetPipeline(p Pipeline)                            {}
func DrawSetVertexBuffer(b Buffer)                          {}
func DrawSetTexture(t Texture)                              {}
func DrawSetScreen(w, h float32)                            {}
func Scissor(x, y, w, h int)                                {}
func ScissorReset()                                         {}
func DrawVertices(count int)                                {}

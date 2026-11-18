//go:build !darwin

// The Metal backend exists only on darwin; these stubs keep the package (and
// `go build ./...`) valid elsewhere. Nothing calls them there, so they do
// nothing and report that there is no device.
package mac

// Program, Buffer, Texture and Target mirror the handles the darwin backend
// hands out.
type (
	Program uintptr
	Buffer  uintptr
	Texture uintptr
	Target  uintptr
)

// Draw modes for Draw.
const (
	Triangles = 0
	Lines     = 1
)

func DeviceReady() bool { return false }

func NewProgram(kind, msl, vs, fs, uniforms, attrs string) (Program, error) {
	return 0, nil
}

func (p Program) Free()                    {}
func (p Program) Uniform(name string) int32 { return -1 }
func (p Program) Use()                     {}

func Uniform1i(loc int32, v int32)          {}
func Uniform1f(loc int32, v float32)        {}
func Uniform2f(loc int32, a, b float32)     {}
func Uniform3f(loc int32, a, b, c float32)  {}
func Uniform4f(loc int32, a, b, c, d float32) {}
func UniformMat4(loc int32, m *[16]float32) {}

func NewBuffer(n int) Buffer         { return 0 }
func (b Buffer) Update(data []byte)  {}
func (b Buffer) Free()               {}
func VertexBuffer(b Buffer)          {}
func Draw(mode, first, count int)    {}

func NewTexture(w, h int, nearest bool) Texture    { return 0 }
func (t Texture) Update(pix []byte, w, h, stride int) {}
func (t Texture) Free()                            {}
func BindTexture(t Texture)                        {}

func NewTarget(w, h, samples, depth int, nearest bool) Target { return 0 }
func (t Target) Free()                                        {}
func (t Target) Width() int                                   { return 0 }
func (t Target) Height() int                                  { return 0 }
func TargetMaxSamples() int                                   { return 0 }
func (t Target) Blit()                                        {}
func (t Target) Read(x, y, w, h int, out []byte)              {}
func BindTarget(t Target)                                     {}

func FrameBegin() bool                 { return false }
func FrameEnd()                        {}
func ClearColor(r, g, b, a float64)    {}
func Clear(color, depth bool)          {}
func Viewport(x, y, w, h int)          {}
func Scissor(x, y, w, h int)           {}
func ScissorReset()                    {}

func SetDepthTest(on bool)  {}
func SetDepthWrite(on bool) {}
func SetDepthFuncLequal(on bool) {}
func SetBlend(on bool)      {}
func SetCull(on bool)       {}

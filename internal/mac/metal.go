//go:build darwin

// Package mac is the Cocoa/Metal host the darwin window and renderers sit
// on. It is the only package in the tree that uses cgo: sc_app.m creates the
// NSWindow, its Metal-backed view and the Metal backend the renderers draw
// through, and this file (with bridge.go) is the Go side of that
// conversation.
//
// Everything the renderers need is behind this package's Go API. It is the
// same on every platform (see metal_stub.go) so the renderers build
// everywhere; only darwin draws anything.
package mac

/*
#include <stdlib.h>
#include "sc_app.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Program, Buffer, Texture and Target are handles to Metal objects owned by
// the C side. Each is released by its Free method; nothing else may be done
// with a handle after that.
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

// DeviceReady reports whether the Metal device and command queue exist. They
// do once the window has been created (sc_start), which the renderers call
// before building their pipelines.
func DeviceReady() bool { return C.sc_device_ready() != 0 }

// NewProgram compiles msl and builds a render program from its vertex
// function vs and fragment function fs. uniforms and attrs describe the
// program's uniform bindings and vertex attributes, as sc_app.h documents:
// "name:stage:index:offset:size" entries separated by ';' for uniforms (one
// per buffer index) and "stride:location:size:offset" entries for attrs.
func NewProgram(kind, msl, vs, fs, uniforms, attrs string) (Program, error) {
	ck := C.CString(kind)
	cm := C.CString(msl)
	cv := C.CString(vs)
	cf := C.CString(fs)
	cu := C.CString(uniforms)
	ca := C.CString(attrs)
	defer C.free(unsafe.Pointer(ck))
	defer C.free(unsafe.Pointer(cm))
	defer C.free(unsafe.Pointer(cv))
	defer C.free(unsafe.Pointer(cf))
	defer C.free(unsafe.Pointer(cu))
	defer C.free(unsafe.Pointer(ca))
	p := C.sc_program(ck, cm, cv, cf, cu, ca)
	if p == 0 {
		return 0, fmt.Errorf("mac: could not build the %s program (%s/%s)", kind, vs, fs)
	}
	return Program(p), nil
}

// Free releases the program.
func (p Program) Free() { C.sc_program_free(C.uintptr_t(p)) }

// Uniform returns the program's slot for a uniform by name, or -1.
func (p Program) Uniform(name string) int32 {
	cn := C.CString(name)
	defer C.free(unsafe.Pointer(cn))
	return int32(C.sc_uniform(C.uintptr_t(p), cn))
}

// Use selects the program for the draws that follow.
func (p Program) Use() { C.sc_use_program(C.uintptr_t(p)) }

func Uniform1i(loc int32, v int32)  { C.sc_uniform1i(C.int(loc), C.int(v)) }
func Uniform1f(loc int32, v float32) { C.sc_uniform1f(C.int(loc), C.float(v)) }
func Uniform2f(loc int32, a, b float32) {
	C.sc_uniform2f(C.int(loc), C.float(a), C.float(b))
}
func Uniform3f(loc int32, a, b, c float32) {
	C.sc_uniform3f(C.int(loc), C.float(a), C.float(b), C.float(c))
}
func Uniform4f(loc int32, a, b, c, d float32) {
	C.sc_uniform4f(C.int(loc), C.float(a), C.float(b), C.float(c), C.float(d))
}

// UniformMat4 sets a 4x4 matrix uniform from a column-major [16]float32.
func UniformMat4(loc int32, m *[16]float32) {
	C.sc_uniform_mat4(C.int(loc), (*C.float)(unsafe.Pointer(m)))
}

// --- buffers ----------------------------------------------------------------

// NewBuffer makes a shared-storage buffer of n bytes.
func NewBuffer(n int) Buffer { return Buffer(C.sc_buffer_new(C.size_t(n))) }

// Update copies data into the buffer from offset 0.
func (b Buffer) Update(data []byte) {
	if b == 0 || len(data) == 0 {
		return
	}
	C.sc_buffer_update(C.uintptr_t(b), unsafe.Pointer(&data[0]), C.size_t(len(data)))
}

// Free releases the buffer.
func (b Buffer) Free() { C.sc_buffer_free(C.uintptr_t(b)) }

// VertexBuffer binds b as the vertex buffer read at buffer(0).
func VertexBuffer(b Buffer) { C.sc_vertex_buffer(C.uintptr_t(b)) }

// Draw draws count vertices from first as a triangle list (Triangles) or a
// line list (Lines).
func Draw(mode, first, count int) {
	C.sc_draw(C.int(mode), C.int(first), C.int(count))
}

// --- textures ---------------------------------------------------------------

// NewTexture makes a w x h RGBA8 texture the CPU can write and shaders can
// read; nearest picks nearest filtering, else linear.
func NewTexture(w, h int, nearest bool) Texture {
	n := C.int(0)
	if nearest {
		n = 1
	}
	return Texture(C.sc_texture_new(C.int(w), C.int(h), n))
}

// Update replaces the texture with pix, whose rows are stride bytes apart
// (stride is normally 4*w).
func (t Texture) Update(pix []byte, w, h, stride int) {
	if t == 0 || len(pix) == 0 {
		return
	}
	C.sc_texture_update(C.uintptr_t(t), unsafe.Pointer(&pix[0]), C.int(w), C.int(h), C.int(stride))
}

// Free releases the texture.
func (t Texture) Free() { C.sc_texture_free(C.uintptr_t(t)) }

// BindTexture binds t as the fragment texture at texture(0).
func BindTexture(t Texture) { C.sc_bind_texture(C.uintptr_t(t)) }

// --- offscreen targets ------------------------------------------------------

// NewTarget makes a w x h colour target with samples samples and a depth
// attachment when depth is non-zero; nearest picks nearest filtering.
func NewTarget(w, h, samples, depth int, nearest bool) Target {
	n := C.int(0)
	if nearest {
		n = 1
	}
	return Target(C.sc_target_new(C.int(w), C.int(h), C.int(samples), C.int(depth), n))
}

// Free releases the target.
func (t Target) Free() { C.sc_target_free(C.uintptr_t(t)) }

// Width and Height are the target's size in pixels.
func (t Target) Width() int  { return int(C.sc_target_width(C.uintptr_t(t))) }
func (t Target) Height() int { return int(C.sc_target_height(C.uintptr_t(t))) }

// TargetMaxSamples is how many samples the device supports.
func TargetMaxSamples() int { return int(C.sc_target_max_samples()) }

// Blit copies the target's resolved colour to the window's drawable.
func (t Target) Blit() { C.sc_target_blit(C.uintptr_t(t)) }

// Read resolves the target and copies the rectangle (x, y from the
// top-left) as w*h*4 RGBA bytes into out.
func (t Target) Read(x, y, w, h int, out []byte) {
	if t == 0 || len(out) == 0 {
		return
	}
	C.sc_target_read(C.uintptr_t(t), C.int(x), C.int(y), C.int(w), C.int(h), unsafe.Pointer(&out[0]))
}

// BindTarget makes target the pass the draws go into; Target(0) is the
// window.
func BindTarget(t Target) { C.sc_bind_target(C.uintptr_t(t)) }

// --- the frame and passes ---------------------------------------------------

// FrameBegin opens a frame: it acquires the next drawable and a command
// buffer. If it returns false there is no drawable this frame and nothing
// should be drawn; do not call FrameEnd.
func FrameBegin() bool { return C.sc_frame_begin() != 0 }

// FrameEnd presents the drawable and commits. Call it once for each
// FrameBegin that returned true.
func FrameEnd() { C.sc_frame_end() }

// ClearColor sets the colour Clear uses.
func ClearColor(r, g, b, a float64) {
	C.sc_clear_color(C.double(r), C.double(g), C.double(b), C.double(a))
}

// Clear clears the current pass: colour and/or depth (non-zero to clear).
func Clear(color, depth bool) {
	c, d := C.int(0), C.int(0)
	if color {
		c = 1
	}
	if depth {
		d = 1
	}
	C.sc_clear(c, d)
}

// Viewport sets the viewport in device pixels, in the OpenGL convention
// (the origin is the target's bottom-left).
func Viewport(x, y, w, h int) {
	C.sc_viewport(C.int(x), C.int(y), C.int(w), C.int(h))
}

// Scissor restricts drawing to a rectangle in device pixels, in the OpenGL
// convention; ScissorReset restores the whole target.
func Scissor(x, y, w, h int) {
	C.sc_scissor(C.int(x), C.int(y), C.int(w), C.int(h))
}
func ScissorReset() { C.sc_scissor_reset() }

// --- draw state -------------------------------------------------------------

func SetDepthTest(on bool)       { C.sc_set_depth_test(btod(on)) }
func SetDepthWrite(on bool)      { C.sc_set_depth_write(btod(on)) }
func SetDepthFuncLequal(on bool) { C.sc_set_depth_func_lequal(btod(on)) }
func SetBlend(on bool)           { C.sc_set_blend(btod(on)) }
func SetCull(on bool)            { C.sc_set_cull(btod(on)) }

func btod(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

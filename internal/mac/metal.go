//go:build darwin

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

// Pipeline, Buffer and Texture are handles to Metal objects owned by the C
// side. Each is released by its Free method; nothing else may be done with a
// handle after that.
type (
	Pipeline uintptr
	Buffer   uintptr
	Texture  uintptr
)

// DeviceReady reports whether the Metal device and command queue exist. They
// do once the window has been created, which is after Run has started; so this
// is false until the first frame.
func DeviceReady() bool { return C.sc_device_ready() != 0 }

// NewPipelineBatch compiles msl and builds the 2D batch pipeline from its
// vertex function vs and fragment function fs (see sc_pipeline_batch).
func NewPipelineBatch(msl, vs, fs string) (Pipeline, error) {
	cm := C.CString(msl)
	cv := C.CString(vs)
	cf := C.CString(fs)
	defer C.free(unsafe.Pointer(cm))
	defer C.free(unsafe.Pointer(cv))
	defer C.free(unsafe.Pointer(cf))
	p := C.sc_pipeline_batch(cm, cv, cf)
	if p == 0 {
		return 0, fmt.Errorf("mac: could not build the batch pipeline (%s/%s)", vs, fs)
	}
	return Pipeline(p), nil
}

// Free releases the pipeline.
func (p Pipeline) Free() { C.sc_pipeline_free(C.uintptr_t(p)) }

// NewBuffer makes a shared-storage vertex buffer of nbytes.
func NewBuffer(nbytes int) Buffer {
	return Buffer(C.sc_buffer_new(C.size_t(nbytes)))
}

// Update copies data into the buffer from offset 0.
func (b Buffer) Update(data []float32) {
	if b == 0 || len(data) == 0 {
		return
	}
	C.sc_buffer_update(C.uintptr_t(b), unsafe.Pointer(&data[0]), C.size_t(len(data)*4))
}

// Free releases the buffer.
func (b Buffer) Free() { C.sc_buffer_free(C.uintptr_t(b)) }

// NewTextureRGBA8 makes a w x h RGBA8 texture that shaders read and the CPU
// writes.
func NewTextureRGBA8(w, h int) Texture {
	return Texture(C.sc_texture_rgba8(C.int(w), C.int(h)))
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

// FrameBegin opens this frame's render pass. If it returns false there is no
// drawable this frame and nothing should be drawn; do not call FrameEnd.
func FrameBegin() bool { return C.sc_frame_begin() != 0 }

// FrameEnd ends the frame's render pass, presents it and commits. Call it once
// for each FrameBegin that returned true.
func FrameEnd() { C.sc_frame_end() }

// DrawSetPipeline selects the pipeline for the draws that follow.
func DrawSetPipeline(p Pipeline) { C.sc_draw_set_pipeline(C.uintptr_t(p)) }

// DrawSetVertexBuffer sets the vertex buffer read at buffer(0).
func DrawSetVertexBuffer(b Buffer) { C.sc_draw_set_vertex_buffer(C.uintptr_t(b)) }

// DrawSetTexture sets the fragment texture at texture(0), with a linear,
// clamp-to-edge sampler at sampler(0).
func DrawSetTexture(t Texture) { C.sc_draw_set_texture(C.uintptr_t(t)) }

// DrawSetScreen passes the screen size in logical pixels as the vertex uniform
// at buffer(1).
func DrawSetScreen(w, h float32) { C.sc_draw_set_screen(C.float(w), C.float(h)) }

// Scissor restricts drawing to a rectangle in device pixels, top-left origin.
func Scissor(x, y, w, h int) {
	C.sc_draw_scissor(C.int(x), C.int(y), C.int(w), C.int(h))
}

// ScissorReset restores the scissor to the whole drawable.
func ScissorReset() { C.sc_draw_scissor_reset() }

// DrawVertices draws count vertices as a triangle list from vertex 0.
func DrawVertices(count int) { C.sc_draw_vertices(C.int(count)) }

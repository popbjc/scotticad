// sc_app.h is the Cocoa/Metal host that the Go side drives. It creates one
// window whose content view is a MetalKit view, runs the application's event
// loop, and calls back into Go on the main thread. This is the darwin
// counterpart of the pure-Go pkg/window implementations and the only part of
// the program that needs cgo.
//
// It has two layers:
//
//   - the host: sc_run/sc_end/sc_set_title, the window and its event loop;
//   - the Metal backend the 2D and 3D renderers draw through: pipelines,
//     buffers, textures and the per-frame render pass (sc_frame_begin and
//     friends). The backend lives here because this is where the MTKView -
//     and so the frame's drawable - is owned.
//
// The Go side must provide two functions, both called on the main thread:
//
//	void goDraw(void);              // once per frame; drives sc_frame_begin/end
//	void goResize(int w, int h);    // when the drawable's size in pixels changes
//
// cgo generates those declarations in _cgo_export.h, which sc_app.m includes.
#ifndef SC_APP_H
#define SC_APP_H

#ifdef __cplusplus
extern "C" {
#endif

// --- the host ---------------------------------------------------------------

// sc_run creates the application, a window with a Metal-backed view, and runs
// its event loop until the application terminates. w and h are the window's
// size in logical pixels. It calls goDraw once per frame and goResize when the
// drawable's pixel size changes. It returns when the application terminates.
void sc_run(const char *title, int w, int h, int visible);

// sc_end terminates the application, so sc_run returns.
void sc_end(void);

// sc_set_title sets the window's title.
void sc_set_title(const char *title);

// --- device -----------------------------------------------------------------

// sc_device_ready reports whether a Metal device and command queue exist (they
// do once the window has been created in goDraw's host, i.e. inside the run).
int sc_device_ready(void);

// --- pipelines --------------------------------------------------------------

// sc_pipeline_batch compiles msl and builds a render pipeline from the vertex
// function vs and the fragment function fs. The pipeline expects the 2D batch's
// vertex layout - a 32-byte stride of float2 position, float2 uv, float4
// colour, read through buffer(0) by vertex id - blends with
// src-alpha/one-minus-src-alpha and writes to a BGRA8 target. Returns 0 on
// failure (the MSL failed to compile, or a function is missing).
uintptr_t sc_pipeline_batch(const char *msl, const char *vs, const char *fs);

// sc_pipeline_free releases a pipeline made by sc_pipeline_batch.
void sc_pipeline_free(uintptr_t p);

// --- buffers ----------------------------------------------------------------

// sc_buffer_new makes a shared-storage buffer of len bytes (0 if it fails).
uintptr_t sc_buffer_new(size_t len);

// sc_buffer_update copies len bytes into b at offset 0.
void sc_buffer_update(uintptr_t b, const void *data, size_t len);

// sc_buffer_free releases a buffer.
void sc_buffer_free(uintptr_t b);

// --- textures ---------------------------------------------------------------

// sc_texture_rgba8 makes a w x h RGBA8 texture that shaders can read and the
// CPU can write (0 if it fails).
uintptr_t sc_texture_rgba8(int w, int h);

// sc_texture_update replaces the whole texture with pix, whose rows are
// bytesPerRow bytes apart.
void sc_texture_update(uintptr_t t, const void *pix, int w, int h, int bytesPerRow);

// sc_texture_free releases a texture.
void sc_texture_free(uintptr_t t);

// --- the frame --------------------------------------------------------------

// sc_frame_begin opens a render pass on the view's current drawable, cleared to
// the colour set by sc_clear, and returns 1. It returns 0 if no drawable is
// available this frame, in which case nothing should be drawn. Every
// sc_frame_begin must be matched by sc_frame_end.
int sc_frame_begin(void);

// sc_frame_end ends the pass, presents the drawable and commits.
void sc_frame_end(void);

// sc_clear sets the colour the next frame is cleared to, components in 0..1.
// Call it before sc_frame_begin.
void sc_clear(double r, double g, double b, double a);

// sc_draw_set_pipeline selects the pipeline state for the draws that follow.
void sc_draw_set_pipeline(uintptr_t p);

// sc_draw_set_vertex_buffer sets the vertex buffer, read at buffer(0).
void sc_draw_set_vertex_buffer(uintptr_t b);

// sc_draw_set_texture sets the fragment texture at texture(0), sampled through
// a linear, clamp-to-edge sampler at sampler(0).
void sc_draw_set_texture(uintptr_t t);

// sc_draw_set_screen passes the screen size in logical pixels as the vertex
// uniform at buffer(1) (the batch shader's uScreen).
void sc_draw_set_screen(float w, float h);

// sc_draw_scissor restricts drawing to a rectangle in device pixels, as
// glScissor does; the origin is the top-left of the drawable.
void sc_draw_scissor(int x, int y, int w, int h);

// sc_draw_scissor_reset restores the scissor to the whole drawable.
void sc_draw_scissor_reset(void);

// sc_draw_vertices draws count vertices as a triangle list.
void sc_draw_vertices(int count);

#ifdef __cplusplus
}
#endif

#endif

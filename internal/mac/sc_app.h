// sc_app.h is the Cocoa/Metal host that the Go side drives. It creates one
// window whose content view is a MetalKit view, runs the application's event
// loop, forwards input events to Go, and provides the Metal backend the 2D and
// 3D renderers draw through. This is the darwin counterpart of the pure-Go
// pkg/window implementations and the only part of the program that needs cgo.
//
// It has three layers:
//
//   - the host: sc_run/sc_end/sc_set_title and the window/event plumbing;
//   - the system services the window layer needs: clipboard, cursor, icon,
//     drag and drop, and the file/message dialogs;
//   - the Metal backend the renderers draw through: programs, buffers,
//     textures, offscreen targets and the per-frame render passes.
//
// The Go side provides several exported functions (declared in the
// _cgo_export.h cgo generates, which sc_app.m includes), all called on the
// main thread:
//
//	void goDraw(void);                       // once per frame
//	void goResize(int w, int h, double scale); // drawable pixels + scale
//	void goEventMouseMove(double x, double y);
//	void goEventMouseDown(int btn, double x, double y, int mods, int clicks);
//	void goEventMouseUp(int btn, double x, double y, int mods);
//	void goEventWheel(double x, double y, double dx, double dy);
//	void goEventMouseLeave(void);
//	void goEventKeyDown(int vk, int mods, int repeat);
//	void goEventKeyUp(int vk, int mods);
//	void goEventChar(int r);
//	void goEventDrop(const char *paths, double x, double y);
//	int  goEventClose(void);                 // 1 to let the window close
//	void goEventIdle(void);
//	void goEventCapture(int lost);
//
// Positions are device pixels, the way the window layer takes them everywhere
// else. Paths in goEventDrop are separated by '\n' (a '\n' can never appear in
// one).
#ifndef SC_APP_H
#define SC_APP_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

// --- the host ---------------------------------------------------------------

// sc_set_scale fixes the interface scale before sc_start (a --scale
// override); 0 (the default) uses the display's own.
void sc_set_scale(double scale);

// sc_backing_scale is the window's backing scale (2 on a Retina display).
double sc_backing_scale(void);

// sc_start creates the application, its Metal device and a window with a
// Metal-backed view, without running anything. w and h are the window's
// content size in logical points. It must be called on the main thread before
// anything is drawn; the renderers build their pipelines once it has returned.
void sc_start(const char *title, int w, int h, int visible);

// sc_runloop runs the application's event loop until the application
// terminates. It must be called on the main thread, after sc_start, and
// returns when the application terminates.
void sc_runloop(void);

// sc_end terminates the application, so sc_runloop returns.
void sc_end(void);

// sc_set_title sets the window's title.
void sc_set_title(const char *title);

// sc_show orders the window to the front.
void sc_show(void);

// sc_set_min_size sets the smallest content size, in logical points.
void sc_set_min_size(int w, int h);

// sc_request_frame asks for a frame now; call it whenever the program has
// something new to draw (the loop only draws when asked).
void sc_request_frame(void);

// --- system services --------------------------------------------------------

// sc_set_clipboard replaces the pasteboard's string.
void sc_set_clipboard(const char *s);

// sc_get_clipboard returns the pasteboard's string as a malloc'd copy the
// caller frees, or NULL when there is no string.
char *sc_get_clipboard(void);

// sc_set_cursor sets the mouse cursor from a window.Cursor id.
void sc_set_cursor(int id);

// sc_set_icon sets the application's Dock icon from a size x size RGBA image
// (big is the larger of the two the window layer renders, both are passed so
// the host can pick).
void sc_set_icon(const void *rgba, int size, int big);

// sc_open_file shows an open dialog and returns a malloc'd path the caller
// frees, or NULL if it is cancelled. filters is a set of "Name|*.ext;*.ext"
// pairs separated by '\n', or NULL.
char *sc_open_file(const char *title, const char *filters);

// sc_save_file shows a save dialog and returns a malloc'd path the caller
// frees, or NULL if it is cancelled. defname may be NULL.
char *sc_save_file(const char *title, const char *filters, const char *defname);

// sc_message shows a message box and returns the button the user pressed as a
// window.Msg* result (a message box constant: 1 OK, 2 Cancel, 6 Yes, 7 No).
// kind is 0 for a plain message, 1 for a warning and 2 for an error; buttons
// is a window message box constant (0 OK, 3 YesNoCancel, 4 YesNo).
int sc_message(const char *title, const char *text, int kind, int buttons);

// sc_open_url opens a URL with the default browser.
void sc_open_url(const char *url);

// sc_accept_drop turns file-drop support on or off.
void sc_accept_drop(int on);

// --- the Metal backend ------------------------------------------------------
//
// Objects the Go side holds are passed back and forth as uintptr_t: an integer
// is outside cgo's pointer checks, and each object is owned by whoever holds
// its handle until the matching *_free.

int sc_device_ready(void);

// sc_program compiles msl and builds a render pipeline state from the vertex
// function vs and fragment function fs. uniforms and attrs describe the layout
// of the program's uniform bindings and its vertex attributes as
//
//	name:stage:index:offset:size      (uniforms, ;-separated)
//	stride:location:size:offset       (attrs, the first field is the stride)
//
// stage is 0 for the vertex function and 1 for the fragment function; index is
// the Metal buffer index the uniform is bound at. kind labels the program in
// diagnostics. Returns 0 if the MSL fails to compile or a function is missing.
uintptr_t sc_program(const char *kind, const char *msl, const char *vs, const char *fs,
                     const char *uniforms, const char *attrs);

void sc_program_free(uintptr_t p);

// sc_uniform returns a program's uniform slot by name, or -1.
int sc_uniform(uintptr_t p, const char *name);

void sc_use_program(uintptr_t p);

void sc_uniform1i(int loc, int v);
void sc_uniform1f(int loc, float v);
void sc_uniform2f(int loc, float a, float b);
void sc_uniform3f(int loc, float a, float b, float c);
void sc_uniform4f(int loc, float a, float b, float c, float d);
void sc_uniform_mat4(int loc, const float *m);

// --- buffers ----------------------------------------------------------------

uintptr_t sc_buffer_new(size_t len);
void sc_buffer_update(uintptr_t b, const void *data, size_t len);
void sc_buffer_free(uintptr_t b);

// sc_vertex_buffer binds b as the vertex buffer read at buffer(0).
void sc_vertex_buffer(uintptr_t b);

// sc_draw draws count vertices from first as a triangle list (mode 0) or a
// line list (mode 1).
void sc_draw(int mode, int first, int count);

// --- textures ---------------------------------------------------------------

// sc_texture_new makes a w x h RGBA8 texture the CPU can write and shaders can
// read; nearest picks nearest filtering (the outline mask), else linear.
uintptr_t sc_texture_new(int w, int h, int nearest);
void sc_texture_update(uintptr_t t, const void *pix, int w, int h, int bytesPerRow);
void sc_texture_free(uintptr_t t);

// sc_bind_texture binds t as the fragment texture at texture(0), sampled
// through the program's sampler.
void sc_bind_texture(uintptr_t t);

// --- offscreen targets ------------------------------------------------------

// sc_target_new makes a w x h colour target with samples samples (>= 1) and a
// depth attachment when depth is non-zero; nearest picks nearest filtering for
// the target's colour when it is sampled (the outline mask), else linear.
// Drawing into it goes through sc_bind_target; sc_target_blit copies it to the
// window and sc_target_read brings a rectangle back to the CPU.
uintptr_t sc_target_new(int w, int h, int samples, int depth, int nearest);
void sc_target_free(uintptr_t t);

// sc_target_width / sc_target_height are the target's size in pixels.
int sc_target_width(uintptr_t t);
int sc_target_height(uintptr_t t);

// sc_target_max_samples is how many samples the device supports.
int sc_target_max_samples(void);

// sc_target_blit copies the target's resolved colour to the window's drawable.
void sc_target_blit(uintptr_t t);

// sc_target_read resolves the target and copies the rectangle (x, y from the
// top-left) as w*h*4 RGBA bytes into out.
void sc_target_read(uintptr_t t, int x, int y, int w, int h, void *out);

// --- the frame and passes ---------------------------------------------------

// sc_frame_begin opens a frame: it acquires the next drawable and a command
// buffer. Returns 1, or 0 if no drawable is available and nothing should be
// drawn, in which case sc_frame_end must not be called.
int sc_frame_begin(void);

// sc_frame_end presents the drawable and commits.
void sc_frame_end(void);

// sc_bind_target makes target the pass draws into (0 is the window). Binding a
// different target ends the current pass and begins another.
void sc_bind_target(uintptr_t target);

// sc_clear_color sets the colour sc_clear uses.
void sc_clear_color(double r, double g, double b, double a);

// sc_clear clears the current pass: colour and/or depth (non-zero to clear).
void sc_clear(int color, int depth);

// sc_viewport sets the viewport in device pixels, in the OpenGL convention
// (the origin is the target's bottom-left; the backend flips it).
void sc_viewport(int x, int y, int w, int h);

// sc_scissor restricts drawing to a rectangle in device pixels, in the OpenGL
// convention (origin bottom-left); sc_scissor_reset restores it to the whole
// target.
void sc_scissor(int x, int y, int w, int h);
void sc_scissor_reset(void);

// --- draw state -------------------------------------------------------------
//
// These mirror the OpenGL state the renderers drive; the backend turns them
// into the matching pipeline state.

void sc_set_depth_test(int on);
void sc_set_depth_write(int on);
void sc_set_depth_func_lequal(int on);
void sc_set_blend(int on);
void sc_set_cull(int on);

#ifdef __cplusplus
}
#endif

#endif

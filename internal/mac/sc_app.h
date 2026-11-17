// sc_app.h is the Cocoa/Metal host that the Go side drives. It creates one
// window whose content view is a MetalKit view, runs the application's event
// loop, and calls back into Go on the main thread. This is the darwin
// counterpart of the pure-Go pkg/window implementations and the only part of
// the program that needs cgo.
//
// The Go side must provide two functions, both called on the main thread:
//
//	void goDraw(void);              // once per frame, before the frame is presented
//	void goResize(int w, int h);    // when the drawable's size in pixels changes
//
// cgo generates those declarations in _cgo_export.h, which sc_app.m includes.
#ifndef SC_APP_H
#define SC_APP_H

#ifdef __cplusplus
extern "C" {
#endif

// sc_run creates the application, a window with a Metal-backed view, and runs
// its event loop until the application terminates. w and h are the window's
// size in logical pixels. It calls goDraw once per frame and goResize when the
// drawable's pixel size changes. It returns when the application terminates.
void sc_run(const char *title, int w, int h, int visible);

// sc_end terminates the application, so sc_run returns.
void sc_end(void);

// sc_set_title sets the window's title.
void sc_set_title(const char *title);

// sc_clear sets the colour the current frame is cleared to, components in
// 0..1. Call it from inside goDraw; the frame is cleared and presented after
// goDraw returns.
void sc_clear(double r, double g, double b, double a);

#ifdef __cplusplus
}
#endif

#endif

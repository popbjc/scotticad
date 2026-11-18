//go:build darwin

// Command scmetalspike is a throwaway check, not part of ScottiCAD. It opens a
// Metal-backed Cocoa window and draws through the same primitives the 2D
// renderer will use: a vertex buffer of the batch's layout, one blended
// pipeline, the atlas-style texture and a scissored draw.
//
// Run it on a Mac after each change to internal/mac:
//
//	go run ./cmd/scmetalspike
//
// Expected: a dark window with a drifting translucent red square (blending), a
// blue checkerboard square (texture), and a green bar showing only through a
// band in the middle (scissor). Resizing prints the drawable's pixel size.
//
// Everything is drawn in device pixels, so on a Retina display the shapes are
// half their printed size in points; the real window layer will carry the
// logical size and scale. That does not affect what is being checked here.
package main

import (
	"math"
	"runtime"
	"time"

	"github.com/scotticad/scotticad/internal/mac"
)

func init() {
	// Cocoa runs on the process's main thread. Go does not promise the main
	// goroutine stays there, so pin it before anything else happens.
	runtime.LockOSThread()
}

// batchMSL is the 2D batch shader: a 32-byte vertex of float2 position (in
// logical pixels), float2 uv and float4 colour, transformed by the screen size.
const batchMSL = `
#include <metal_stdlib>
using namespace metal;

struct VIn {
	float2 pos;
	float2 uv;
	float4 col;
};

struct VOut {
	float4 pos [[position]];
	float2 uv;
	float4 col;
};

vertex VOut vs_batch(uint vid [[vertex_id]],
                     const device VIn *verts [[buffer(0)]],
                     constant float2 &uScreen [[buffer(1)]]) {
	VIn v = verts[vid];
	VOut o;
	o.pos = float4(v.pos.x / uScreen.x * 2.0 - 1.0,
	               1.0 - v.pos.y / uScreen.y * 2.0,
	               0.0, 1.0);
	o.uv = v.uv;
	o.col = v.col;
	return o;
}

fragment float4 fs_batch(VOut in [[stage_in]],
                         texture2d<float> uTex [[texture(0)]],
                         sampler uSamp [[sampler(0)]]) {
	return in.col * uTex.sample(uSamp, in.uv);
}
`

const texN = 64

var (
	pipe  mac.Pipeline
	buf   mac.Buffer
	tex   mac.Texture
	verts []float32
	ready bool
	start = time.Now()

	pw, ph = 1280, 800
	// wUV points at the centre of a 4x4 white block packed at the atlas's
	// top-left, the way Batch.addWhite samples a white pixel.
	wUV = float32(2.0 / texN)
)

func vert(x, y float64, u, v float32, c [4]float32) {
	verts = append(verts, float32(x), float32(y), u, v, c[0], c[1], c[2], c[3])
}

func quad(x, y, w, h float64, u0, v0, u1, v1 float32, c [4]float32) {
	vert(x, y, u0, v0, c)
	vert(x+w, y, u1, v0, c)
	vert(x+w, y+h, u1, v1, c)
	vert(x, y, u0, v0, c)
	vert(x+w, y+h, u1, v1, c)
	vert(x, y+h, u0, v1, c)
}

// flush uploads and draws what has been queued, then empties the queue (the
// batch does the same before it changes clip state).
func flush() {
	if len(verts) == 0 {
		return
	}
	buf.Update(verts)
	mac.DrawVertices(len(verts) / 8)
	verts = verts[:0]
}

// makeChecker is a stand-in atlas: a checkerboard with a 4x4 white block at the
// top-left for solid shapes to sample.
func makeChecker(n int) []byte {
	pix := make([]byte, n*n*4)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			i := (y*n + x) * 4
			switch {
			case x < 4 && y < 4:
				pix[i], pix[i+1], pix[i+2], pix[i+3] = 255, 255, 255, 255
			case ((x/8)+(y/8))%2 == 0:
				pix[i], pix[i+1], pix[i+2], pix[i+3] = 90, 170, 255, 255
			default:
				pix[i], pix[i+1], pix[i+2], pix[i+3] = 25, 60, 110, 255
			}
		}
	}
	return pix
}

func setup() error {
	var err error
	if pipe, err = mac.NewPipelineBatch(batchMSL, "vs_batch", "fs_batch"); err != nil {
		return err
	}
	buf = mac.NewBuffer(1 << 20) // room for many quads
	tex = mac.NewTextureRGBA8(texN, texN)
	tex.Update(makeChecker(texN), texN, texN, texN*4)
	return nil
}

func draw() {
	if !ready {
		if !mac.DeviceReady() {
			return
		}
		if err := setup(); err != nil {
			panic(err)
		}
		ready = true
	}

	mac.Clear(0.09, 0.10, 0.12, 1)
	if !mac.FrameBegin() {
		return
	}
	mac.DrawSetPipeline(pipe)
	mac.DrawSetVertexBuffer(buf)
	mac.DrawSetTexture(tex)
	mac.DrawSetScreen(float32(pw), float32(ph))
	mac.ScissorReset()

	sw, sh := float64(pw), float64(ph)
	t := time.Since(start).Seconds()

	// a drifting translucent square: blending
	bx := (sw-260)/2 + math.Sin(t)*120
	by := (sh-180)/2 + math.Cos(t*0.8)*80
	quad(bx, by, 260, 180, wUV, wUV, wUV, wUV, [4]float32{0.95, 0.35, 0.30, 0.5})
	flush()

	// a checkerboard square: the atlas texture path
	quad(40, 40, 320, 240, 0.10, 0.10, 0.90, 0.90, [4]float32{1, 1, 1, 1})
	flush()

	// a full-width bar clipped to a band in the middle: scissor
	mac.Scissor(pw/4, ph/2, pw/2, 90)
	quad(0, sh/2-40, sw, 160, wUV, wUV, wUV, wUV, [4]float32{0.25, 0.85, 0.45, 1})
	flush()
	mac.ScissorReset()

	mac.FrameEnd()
}

func main() {
	mac.OnResize = func(w, h int) {
		pw, ph = w, h
		println("drawable:", w, "x", h, "px")
	}
	mac.OnDraw = draw
	mac.Run("ScottiCAD Metal spike", 1280, 800, true)
}

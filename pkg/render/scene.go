package render

import (
	"math"
	"unsafe"

	"github.com/scotticad/scotticad/internal/mac"
	"github.com/scotticad/scotticad/pkg/geom"
)

const meshMSL = `
#include <metal_stdlib>
using namespace metal;

struct MeshIn {
	float3 pos [[attribute(0)]];
	float3 normal [[attribute(1)]];
	float3 color [[attribute(2)]];
};
struct MeshOut {
	float4 position [[position]];
	float3 normal;
	float3 color;
	float3 world;
};
float modGL(float x, float y) { return x - y * floor(x / y); }
vertex MeshOut vs_mesh(uint vid [[vertex_id]], const device MeshIn *verts [[buffer(0)]],
	constant float4x4 &uVP [[buffer(1)]], constant float4x4 &uModel [[buffer(2)]],
	constant float4x4 &uNormal [[buffer(3)]]) {
	MeshIn v = verts[vid];
	float4 world = uModel * float4(v.pos, 1.0);
	MeshOut o;
	o.position = uVP * world;
	o.normal = (uNormal * float4(v.normal, 0.0)).xyz;
	o.color = v.color;
	o.world = world.xyz;
	return o;
}
fragment float4 fs_mesh(MeshOut in [[stage_in]],
	constant float3 &uEye [[buffer(1)]], constant float4 &uTint [[buffer(2)]],
	constant float &uAlpha [[buffer(3)]], constant float &uStripes [[buffer(4)]],
	constant float &uFlat [[buffer(5)]], constant float &uPx [[buffer(6)]],
	constant float4 &uClip [[buffer(7)]], constant float &uCap [[buffer(8)]]) {
	if (dot(uClip.xyz, uClip.xyz) > 0.0 && dot(uClip.xyz, in.world) + uClip.w < 0.0) {
		discard_fragment(); return float4(0.0);
	}
	float3 c = mix(in.color, uTint.rgb, uTint.a);
	if (uFlat > 0.5) return float4(c, 1.0);
	float3 n = normalize(in.normal);
	float3 v = normalize(uEye - in.world);
	bool back = dot(n, v) < 0.0;
	if (back) n = -n;
	float3 l1 = normalize(float3(-0.35, -0.55, 1.0));
	float3 l2 = normalize(v + float3(0.0, 0.0, 0.4));
	if (uCap > 0.5 && back) {
		c *= 0.62;
		if (modGL(in.position.x + in.position.y, 7.0 * uPx) < 1.2 * uPx) c *= 0.72;
		return float4(c, uAlpha);
	}
	if (uStripes > 0.5) {
		float d = 0.86 + 0.08 * max(dot(n, l1), 0.0) + 0.06 * max(dot(n, l2), 0.0);
		if (back) d *= 0.80;
		c *= d;
		float stripe = modGL(in.position.y - in.position.x * 0.18, 4.0 * uPx);
		if (stripe < uPx) c *= 0.83;
		return float4(min(c, float3(1.0)), uAlpha);
	}
	float d = 0.50 + 0.30 * max(dot(n, l1), 0.0) + 0.28 * max(dot(n, l2), 0.0);
	return float4(min(c * d, float3(1.0)), uAlpha);
}
`

const lineMSL = `
#include <metal_stdlib>
using namespace metal;
struct LineIn { float3 pos [[attribute(0)]]; float4 color [[attribute(1)]]; };
struct LineOut {
	float4 position [[position]];
	float4 color;
	float3 world;
};
vertex LineOut vs_line(uint vid [[vertex_id]], const device LineIn *verts [[buffer(0)]],
	constant float4x4 &uVP [[buffer(1)]], constant float4x4 &uModel [[buffer(2)]]) {
	LineIn v = verts[vid];
	float4 world = uModel * float4(v.pos, 1.0);
	LineOut o;
	o.position = uVP * world;
	o.color = v.color;
	o.world = world.xyz;
	return o;
}
fragment float4 fs_line(LineOut in [[stage_in]], constant float &uAlpha [[buffer(1)]],
	constant float4 &uOver [[buffer(2)]], constant float4 &uClip [[buffer(3)]]) {
	if (dot(uClip.xyz, uClip.xyz) > 0.0 && dot(uClip.xyz, in.world) + uClip.w < 0.0) {
		discard_fragment(); return float4(0.0);
	}
	if (uOver.a > 0.0) return float4(uOver.rgb, uOver.a * uAlpha);
	return float4(in.color.rgb, in.color.a * uAlpha);
}
`

const planeMSL = `
#include <metal_stdlib>
using namespace metal;
struct PlaneOut { float4 position [[position]]; float2 uv; };
vertex PlaneOut vs_plane(uint vid [[vertex_id]], const device float3 *verts [[buffer(0)]],
	constant float4x4 &uVP [[buffer(1)]], constant float4 &uRect [[buffer(2)]]) {
	float3 p = verts[vid];
	PlaneOut o;
	o.position = uVP * float4(p, 1.0);
	o.uv = float2((p.x - uRect.x) / uRect.z, 1.0 - (p.y - uRect.y) / uRect.w);
	return o;
}
fragment float4 fs_plane(PlaneOut in [[stage_in]], texture2d<float> uShadow [[texture(0)]],
	sampler uSamp [[sampler(0)]], constant float4 &uColor [[buffer(1)]],
	constant float &uShadowOn [[buffer(2)]], constant float2 &uTexel [[buffer(3)]]) {
	float shadow = 0.0;
	if (uShadowOn > 0.5) {
		float total = 0.0;
		for (int x = -3; x <= 3; x++) for (int y = -3; y <= 3; y++) {
			float weight = 1.0 / (1.0 + float(x*x + y*y));
			shadow += weight * uShadow.sample(uSamp, in.uv + float2(x, y) * uTexel * 2.0).r;
			total += weight;
		}
		shadow /= total;
	}
	return float4(uColor.rgb * (1.0 - 0.22 * shadow), uColor.a);
}
`

const outlineMSL = `
#include <metal_stdlib>
using namespace metal;
struct QuadOut { float4 position [[position]]; float2 uv; };
vertex QuadOut vs_outline(uint vid [[vertex_id]], const device float2 *verts [[buffer(0)]]) {
	float2 p = verts[vid];
	QuadOut o;
	o.position = float4(p, 0.0, 1.0);
	o.uv = float2(p.x * 0.5 + 0.5, 0.5 - p.y * 0.5);
	return o;
}
fragment float4 fs_outline(QuadOut in [[stage_in]], texture2d<float> uMask [[texture(0)]],
	sampler uSamp [[sampler(0)]], constant float2 &uTexel [[buffer(1)]],
	constant float &uRadius [[buffer(2)]], constant float &uRadiusB [[buffer(3)]],
	constant float &uGlow [[buffer(4)]], constant float &uGlowAlpha [[buffer(5)]],
	constant float3 &uColR [[buffer(6)]], constant float3 &uColG [[buffer(7)]],
	constant float3 &uColB [[buffer(8)]]) {
	float3 c = uMask.sample(uSamp, in.uv).rgb;
	float3 m = float3(0.0);
	int r = int(uRadius), rb = int(uRadiusB);
	for (int x = -r; x <= r; x++) for (int y = -r; y <= r; y++) {
		int d2 = x*x + y*y;
		if (d2 > r*r + 1) continue;
		float3 sampleColor = uMask.sample(uSamp, in.uv + float2(x, y) * uTexel).rgb;
		if (d2 > rb*rb + 1) sampleColor.b = 0.0;
		m = max(m, sampleColor);
	}
	float3 edge = m * (1.0 - step(float3(0.5), c));
	if (edge.g > 0.5) return float4(uColG, 1.0);
	if (edge.r > 0.5) return float4(uColR, 1.0);
	if (edge.b > 0.5) return float4(uColB, 1.0);
	if (uGlow <= uRadius || c.r > 0.5) { discard_fragment(); return float4(0.0); }
	float best = 1e9;
	for (int i = 0; i < 16; i++) {
		float angle = float(i) * 0.39269908;
		float2 direction = float2(cos(angle), sin(angle));
		for (int j = 1; j <= 3; j++) {
			float distance = uRadius + (uGlow - uRadius) * float(j) / 3.0;
			if (uMask.sample(uSamp, in.uv + direction * distance * uTexel).r > 0.5)
				best = min(best, distance);
		}
	}
	if (best > uGlow) { discard_fragment(); return float4(0.0); }
	float t = (best - uRadius) / (uGlow - uRadius);
	return float4(uColR, uGlowAlpha * (1.0 - t) * (1.0 - t));
}
`

const (
	meshUniforms = "uVP:0:1:0:64;uModel:0:2:0:64;uNormal:0:3:0:64;" +
		"uEye:1:1:0:16;uTint:1:2:0:16;uAlpha:1:3:0:4;uStripes:1:4:0:4;" +
		"uFlat:1:5:0:4;uPx:1:6:0:4;uClip:1:7:0:16;uCap:1:8:0:4"
	lineUniforms = "uVP:0:1:0:64;uModel:0:2:0:64;" +
		"uAlpha:1:1:0:4;uOver:1:2:0:16;uClip:1:3:0:16"
	planeUniforms = "uVP:0:1:0:64;uRect:0:2:0:16;" +
		"uColor:1:1:0:16;uShadowOn:1:2:0:4;uTexel:1:3:0:8"
	outlineUniforms = "uTexel:1:1:0:8;uRadius:1:2:0:4;uRadiusB:1:3:0:4;" +
		"uGlow:1:4:0:4;uGlowAlpha:1:5:0:4;uColR:1:6:0:16;uColG:1:7:0:16;uColB:1:8:0:16"
	meshAttrs    = "36:0:3:0;36:1:3:12;36:2:3:24"
	lineAttrs    = "28:0:3:0;28:1:4:12"
	planeAttrs   = "12:0:3:0"
	outlineAttrs = "8:0:2:0"
)

type gpuMesh struct {
	vbo, evbo mac.Buffer
	n, en     int
	used      int
	adj       []adjEdge // for silhouettes, built on first use
}

// adjEdge is a mesh edge with the normals of its two faces.
type adjEdge struct {
	a, b   geom.Vec3
	n0, n1 geom.Vec3
}

// Lines is a dynamic line list (world space).
type Lines struct {
	data []float32
}

func (l *Lines) Add(a, b geom.Vec3, c [4]float32) {
	l.data = append(l.data, float32(a.X), float32(a.Y), float32(a.Z), c[0], c[1], c[2], c[3],
		float32(b.X), float32(b.Y), float32(b.Z), c[0], c[1], c[2], c[3])
}

func (l *Lines) Reset() { l.data = l.data[:0] }

// Scene renders 3D content.
type Scene struct {
	meshProg, lineProg, planeProg, outlineProg mac.Program
	mu                                         map[string]int32
	lu, pu, ou                                 map[string]int32
	cache                                      map[*geom.Mesh]*gpuMesh
	frame                                      int
	lineVBO, quadVBO, planeVBO                 mac.Buffer
	lineVBOSize                                int

	// shadow map over the workplane
	shadowTarget mac.Target
	shadowSize   int32

	// outline mask
	maskTarget   mac.Target
	maskW, maskH int32

	VP  geom.Mat4
	Eye geom.Vec3
	// PixelScale is device pixels per logical pixel (stripe spacing).
	PixelScale float32
}

func uniforms(p mac.Program, names ...string) map[string]int32 {
	m := map[string]int32{}
	for _, n := range names {
		m[n] = p.Uniform(n)
	}
	return m
}

func float32Bytes(data []float32) []byte {
	if len(data) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&data[0])), len(data)*4)
}

func NewScene() (*Scene, error) {
	s := &Scene{cache: map[*geom.Mesh]*gpuMesh{}}
	var err error
	if s.meshProg, err = mac.NewProgram("mesh", meshMSL, "vs_mesh", "fs_mesh", meshUniforms, meshAttrs); err != nil {
		return nil, err
	}
	s.mu = uniforms(s.meshProg, "uVP", "uModel", "uNormal", "uEye", "uTint", "uAlpha", "uStripes", "uFlat", "uPx", "uClip", "uCap")
	if s.lineProg, err = mac.NewProgram("line", lineMSL, "vs_line", "fs_line", lineUniforms, lineAttrs); err != nil {
		return nil, err
	}
	s.lu = uniforms(s.lineProg, "uVP", "uModel", "uAlpha", "uOver", "uClip")
	if s.planeProg, err = mac.NewProgram("plane", planeMSL, "vs_plane", "fs_plane", planeUniforms, planeAttrs); err != nil {
		return nil, err
	}
	s.pu = uniforms(s.planeProg, "uVP", "uRect", "uColor", "uShadowOn", "uTexel")
	if s.outlineProg, err = mac.NewProgram("outline", outlineMSL, "vs_outline", "fs_outline", outlineUniforms, outlineAttrs); err != nil {
		return nil, err
	}
	s.ou = uniforms(s.outlineProg, "uTexel", "uRadius", "uRadiusB", "uGlow", "uGlowAlpha", "uColR", "uColG", "uColB")

	s.lineVBOSize = 1 << 16
	s.lineVBO = mac.NewBuffer(s.lineVBOSize)
	quad := []float32{-1, -1, 1, -1, 1, 1, -1, -1, 1, 1, -1, 1}
	s.quadVBO = mac.NewBuffer(len(quad) * 4)
	s.quadVBO.Update(float32Bytes(quad))
	s.planeVBO = mac.NewBuffer(6 * 3 * 4)

	s.shadowSize = 1024
	s.shadowTarget = mac.NewTarget(int(s.shadowSize), int(s.shadowSize), 1, 0, false)
	return s, nil
}

func (s *Scene) upload(m *geom.Mesh) *gpuMesh {
	// keyed by the mesh itself: the key keeps it alive, so its address
	// can't be reused by another mesh while its upload is here (meshes are
	// never changed once made)
	g, ok := s.cache[m]
	if ok {
		g.used = s.frame
		return g
	}
	data := make([]float32, 0, len(m.Tris)*27)
	for _, t := range m.Tris {
		n := t.Normal()
		for _, p := range t.P {
			data = append(data, float32(p.X), float32(p.Y), float32(p.Z), float32(n.X), float32(n.Y), float32(n.Z), t.C.R, t.C.G, t.C.B)
		}
	}
	g = &gpuMesh{vbo: mac.NewBuffer(len(data) * 4), n: len(m.Tris) * 3, used: s.frame}
	g.vbo.Update(float32Bytes(data))

	// feature edges (dihedral > 25°), darker than the face colour
	edges := featureEdges(m, 25)
	if len(edges) > 0 {
		g.evbo = mac.NewBuffer(len(edges) * 4)
		g.evbo.Update(float32Bytes(edges))
		g.en = len(edges) / 7
	}
	s.cache[m] = g
	return g
}

// featureEdges returns the creases of a mesh (faces meeting at more than
// deg degrees) as coloured line vertices. Only edges shared by exactly two
// faces count: primitives are closed, so an unshared edge comes from a CSG
// seam (a T-junction, where one long edge meets several short ones) and
// drawing it would scratch hairlines across flat faces. Sliver triangles
// from CSG have meaningless normals and are ignored.
func featureEdges(m *geom.Mesh, deg float64) []float32 {
	type info struct {
		a, b  geom.Vec3
		n     []geom.Vec3 // normals of reliable (non-sliver) faces
		faces int
		c     geom.Color
	}
	edges := map[edgeKey]*info{}
	var order []*info
	for _, t := range m.Tris {
		e1, e2, e3 := t.P[1].Sub(t.P[0]), t.P[2].Sub(t.P[0]), t.P[2].Sub(t.P[1])
		cr := e1.Cross(e2)
		long := math.Max(e1.Dot(e1), math.Max(e2.Dot(e2), e3.Dot(e3)))
		reliable := cr.Len() > 1e-5*long
		n := cr.Norm()
		for i := 0; i < 3; i++ {
			a, b := t.P[i], t.P[(i+1)%3]
			k, ok := edgeKeyOf(a, b)
			if !ok {
				continue
			}
			e := edges[k]
			if e == nil {
				e = &info{a: a, b: b, c: t.C}
				edges[k] = e
				order = append(order, e)
			}
			e.faces++
			if reliable {
				e.n = append(e.n, n)
			}
		}
	}
	cosT := math.Cos(deg * math.Pi / 180)
	var out []float32
	for _, e := range order {
		if e.faces != 2 || len(e.n) != 2 || e.n[0].Dot(e.n[1]) >= cosT {
			continue
		}
		c := [4]float32{e.c.R * 0.55, e.c.G * 0.55, e.c.B * 0.55, 0.7}
		out = append(out, float32(e.a.X), float32(e.a.Y), float32(e.a.Z), c[0], c[1], c[2], c[3],
			float32(e.b.X), float32(e.b.Y), float32(e.b.Z), c[0], c[1], c[2], c[3])
	}
	return out
}

// Begin sets up the frame's camera.
func (s *Scene) Begin(vp geom.Mat4, eye geom.Vec3) {
	s.frame++
	s.VP = vp
	s.Eye = eye
}

// Cached is how many meshes are uploaded to the GPU at the moment.
func (s *Scene) Cached() int { return len(s.cache) }

// Evict frees GPU meshes not drawn for a while.
func (s *Scene) Evict() {
	for m, g := range s.cache {
		if s.frame-g.used > 120 {
			s.free(g)
			delete(s.cache, m)
		}
	}
}

// free releases an upload's GPU buffers.
func (s *Scene) free(g *gpuMesh) {
	g.vbo.Free()
	if g.evbo != 0 {
		g.evbo.Free()
	}
}

// Style controls how a mesh is drawn.
type Style struct {
	Model   geom.Mat4
	Tint    [4]float32 // rgb + amount
	Alpha   float32
	Stripes bool
	Flat    bool
	Edges   bool
	// Outline, when its alpha is > 0, draws feature edges and the
	// view-dependent silhouette in this colour (hole style).
	Outline [4]float32
	// Clip, when its normal (xyz) is non-zero, hides the part of the mesh
	// where dot(normal, p) + w < 0 (a world-space plane).
	Clip [4]float32
	// Cap draws the inside of a clipped closed mesh as its cut face.
	Cap bool
}

// DrawMesh draws a mesh (in world space unless Model is set).
func (s *Scene) DrawMesh(m *geom.Mesh, st Style) {
	if m == nil || len(m.Tris) == 0 {
		return
	}
	if st.Model == (geom.Mat4{}) {
		st.Model = geom.Identity()
	}
	if st.Alpha == 0 {
		st.Alpha = 1
	}
	g := s.upload(m)
	s.meshProg.Use()
	setMat4(s.mu["uVP"], s.VP)
	setMat4(s.mu["uModel"], st.Model)
	setMat4(s.mu["uNormal"], st.Model.NormalMatrix())
	mac.Uniform3f(s.mu["uEye"], float32(s.Eye.X), float32(s.Eye.Y), float32(s.Eye.Z))
	mac.Uniform4f(s.mu["uTint"], st.Tint[0], st.Tint[1], st.Tint[2], st.Tint[3])
	mac.Uniform1f(s.mu["uAlpha"], st.Alpha)
	mac.Uniform1f(s.mu["uStripes"], bf(st.Stripes))
	mac.Uniform1f(s.mu["uFlat"], bf(st.Flat))
	mac.Uniform4f(s.mu["uClip"], st.Clip[0], st.Clip[1], st.Clip[2], st.Clip[3])
	mac.Uniform1f(s.mu["uCap"], bf(st.Cap))
	px := s.PixelScale
	if px <= 0 {
		px = 1
	}
	mac.Uniform1f(s.mu["uPx"], px)
	mac.VertexBuffer(g.vbo)
	mac.Draw(mac.Triangles, 0, g.n)
	outline := st.Outline[3] > 0 && !st.Flat
	if (st.Edges || outline) && g.en > 0 && !st.Flat {
		s.lineProg.Use()
		setMat4(s.lu["uVP"], s.VP)
		setMat4(s.lu["uModel"], st.Model)
		mac.Uniform4f(s.lu["uClip"], st.Clip[0], st.Clip[1], st.Clip[2], st.Clip[3])
		mac.Uniform1f(s.lu["uAlpha"], 1)
		if outline {
			mac.Uniform4f(s.lu["uOver"], st.Outline[0], st.Outline[1], st.Outline[2], st.Outline[3])
		} else {
			mac.Uniform1f(s.lu["uAlpha"], st.Alpha)
			mac.Uniform4f(s.lu["uOver"], 0, 0, 0, 0)
		}
		mac.VertexBuffer(g.evbo)
		mac.Draw(mac.Lines, 0, g.en*2)
	}
	if outline {
		s.drawSilhouette(m, g, st)
	}
}

func setMat4(loc int32, m geom.Mat4) {
	values := m.Float32()
	mac.UniformMat4(loc, &values)
}

// drawSilhouette draws edges between a front- and a back-facing triangle
// as seen from the eye.
func (s *Scene) drawSilhouette(m *geom.Mesh, g *gpuMesh, st Style) {
	if g.adj == nil {
		g.adj = adjacency(m)
	}
	inv, ok := st.Model.Inverse()
	if !ok {
		return
	}
	eye := inv.Point(s.Eye)
	ortho := s.VP[15] == 1 && s.VP[3] == 0 && s.VP[7] == 0 && s.VP[11] == 0
	var dir geom.Vec3
	if ortho {
		// orthographic: one view direction for every edge, found by
		// unprojecting the near and far points at the screen's centre
		inv2, _ := s.VP.Inverse()
		p0, _ := inv2.Project(geom.V(0, 0, -1))
		p1, _ := inv2.Project(geom.V(0, 0, 1))
		dir = inv.Dir(p1.Sub(p0))
	}
	var data []float32
	c := st.Outline
	for _, e := range g.adj {
		var v geom.Vec3
		if ortho {
			v = dir.Neg()
		} else {
			v = eye.Sub(e.a)
		}
		if (e.n0.Dot(v) > 0) == (e.n1.Dot(v) > 0) {
			continue
		}
		data = append(data, float32(e.a.X), float32(e.a.Y), float32(e.a.Z), c[0], c[1], c[2], c[3],
			float32(e.b.X), float32(e.b.Y), float32(e.b.Z), c[0], c[1], c[2], c[3])
	}
	if len(data) == 0 {
		return
	}
	s.lineProg.Use()
	setMat4(s.lu["uVP"], s.VP)
	setMat4(s.lu["uModel"], st.Model)
	mac.Uniform4f(s.lu["uClip"], st.Clip[0], st.Clip[1], st.Clip[2], st.Clip[3])
	mac.Uniform1f(s.lu["uAlpha"], 1)
	mac.Uniform4f(s.lu["uOver"], 0, 0, 0, 0)
	s.uploadLines(data)
	mac.Draw(mac.Lines, 0, len(data)/7)
}

// edgeKey is an undirected edge by its end points rounded to 0.1 µm, the
// lesser first, so the copies of a shared edge in its two faces match.
type edgeKey [6]int64

// edgeKeyOf is a-b's key; ok is false when the ends round to one point.
func edgeKeyOf(a, b geom.Vec3) (k edgeKey, ok bool) {
	q := func(p geom.Vec3) [3]int64 {
		return [3]int64{int64(math.Round(p.X * 1e4)), int64(math.Round(p.Y * 1e4)), int64(math.Round(p.Z * 1e4))}
	}
	qa, qb := q(a), q(b)
	if qa == qb {
		return k, false
	}
	if qa[0] < qb[0] || (qa[0] == qb[0] && (qa[1] < qb[1] || (qa[1] == qb[1] && qa[2] < qb[2]))) {
		return edgeKey{qa[0], qa[1], qa[2], qb[0], qb[1], qb[2]}, true
	}
	return edgeKey{qb[0], qb[1], qb[2], qa[0], qa[1], qa[2]}, true
}

func adjacency(m *geom.Mesh) []adjEdge {
	idx := map[edgeKey]int{}
	var out []adjEdge
	var count []int
	for _, t := range m.Tris {
		n := t.Normal()
		for i := 0; i < 3; i++ {
			a, b := t.P[i], t.P[(i+1)%3]
			k, ok := edgeKeyOf(a, b)
			if !ok {
				continue
			}
			if j, ok := idx[k]; ok {
				if count[j] == 1 {
					out[j].n1 = n
				}
				count[j]++
				continue
			}
			idx[k] = len(out)
			out = append(out, adjEdge{a: a, b: b, n0: n, n1: n})
			count = append(count, 1)
		}
	}
	// keep only edges shared by exactly two faces: an open or non-manifold
	// edge has no pair of normals to turn between front and back
	keep := out[:0]
	for i, e := range out {
		if count[i] == 2 {
			keep = append(keep, e)
		}
	}
	return keep
}

func bf(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// DrawLines draws a line list.
func (s *Scene) DrawLines(l *Lines, alpha float32) { s.DrawLinesM(l, alpha, geom.Identity()) }

// DrawLinesM is DrawLines with the points given in model space.
func (s *Scene) DrawLinesM(l *Lines, alpha float32, model geom.Mat4) {
	s.drawLineProg(mac.Lines, l.data, alpha, model)
}

// DrawTris draws world-space coloured triangles (pos3+col4 per vertex)
// through the line program.
func (s *Scene) DrawTris(data []float32, alpha float32) { s.DrawTrisM(data, alpha, geom.Identity()) }

// DrawTrisM is DrawTris with the vertices given in model space.
func (s *Scene) DrawTrisM(data []float32, alpha float32, model geom.Mat4) {
	s.drawLineProg(mac.Triangles, data, alpha, model)
}

// drawLineProg draws pos3+col4 vertices with the line program, unclipped;
// alpha 0 is opaque.
func (s *Scene) drawLineProg(mode int, data []float32, alpha float32, model geom.Mat4) {
	if len(data) == 0 {
		return
	}
	s.lineProg.Use()
	setMat4(s.lu["uVP"], s.VP)
	setMat4(s.lu["uModel"], model)
	mac.Uniform4f(s.lu["uClip"], 0, 0, 0, 0)
	if alpha == 0 {
		alpha = 1
	}
	mac.Uniform1f(s.lu["uAlpha"], alpha)
	mac.Uniform4f(s.lu["uOver"], 0, 0, 0, 0)
	s.uploadLines(data)
	mac.Draw(mode, 0, len(data)/7)
}

func (s *Scene) uploadLines(data []float32) {
	n := len(data) * 4
	if n > s.lineVBOSize {
		s.lineVBO.Free()
		s.lineVBOSize = 1 << 16
		for s.lineVBOSize < n {
			s.lineVBOSize *= 2
		}
		s.lineVBO = mac.NewBuffer(s.lineVBOSize)
	}
	s.lineVBO.Update(float32Bytes(data))
	mac.VertexBuffer(s.lineVBO)
}

// RenderShadows draws the given meshes, seen from straight above, into the
// shadow map covering rect (minx, miny, w, h). It restores fb afterwards.
func (s *Scene) RenderShadows(parts []ShadowPart, rect [4]float64, fb mac.Target, vx, vy, vw, vh int32) {
	mac.BindTarget(s.shadowTarget)
	mac.Viewport(0, 0, int(s.shadowSize), int(s.shadowSize))
	mac.SetDepthTest(false)
	mac.SetBlend(false)
	mac.ClearColor(0, 0, 0, 0)
	mac.Clear(true, false)
	saved := s.VP
	s.VP = geom.Ortho(rect[0], rect[0]+rect[2], rect[1], rect[1]+rect[3], -10000, 10000)
	// faint shadows first so solid ones overwrite them
	for _, light := range []bool{true, false} {
		k := float32(1)
		if light {
			k = 0.4
		}
		for _, p := range parts {
			if p.Light == light {
				s.DrawMesh(p.Mesh, Style{Model: p.M, Flat: true, Tint: [4]float32{k, k, k, 1}})
			}
		}
	}
	s.VP = saved
	mac.BindTarget(fb)
	mac.Viewport(int(vx), int(vy), int(vw), int(vh))
}

// DrawPlane draws the workplane fill (with shadows) as a quad.
func (s *Scene) DrawPlane(minX, minY, maxX, maxY, z float64, color [4]float32, shadowRect [4]float64, shadows bool) {
	q := []float32{
		float32(minX), float32(minY), float32(z), float32(maxX), float32(minY), float32(z), float32(maxX), float32(maxY), float32(z),
		float32(minX), float32(minY), float32(z), float32(maxX), float32(maxY), float32(z), float32(minX), float32(maxY), float32(z),
	}
	s.planeProg.Use()
	setMat4(s.pu["uVP"], s.VP)
	mac.Uniform4f(s.pu["uRect"], float32(shadowRect[0]), float32(shadowRect[1]), float32(shadowRect[2]), float32(shadowRect[3]))
	mac.Uniform4f(s.pu["uColor"], color[0], color[1], color[2], color[3])
	mac.Uniform1f(s.pu["uShadowOn"], bf(shadows))
	mac.Uniform2f(s.pu["uTexel"], 1/float32(s.shadowSize), 1/float32(s.shadowSize))
	mac.BindTexture(mac.Texture(s.shadowTarget))
	s.planeVBO.Update(float32Bytes(q))
	mac.VertexBuffer(s.planeVBO)
	mac.Draw(mac.Triangles, 0, 6)
}

// MaskPart identifies geometry for the outline mask: channel 0 = red
// (selection), 1 = green (bundle child), 2 = blue (hover).
type MaskPart struct {
	Mesh    *geom.Mesh
	M       geom.Mat4
	Channel int
	Clip    [4]float32 // see Style.Clip
}

// ShadowPart is a mesh placed in the world for the shadow pass.
type ShadowPart struct {
	Mesh *geom.Mesh
	M    geom.Mat4
	// Light casts a fainter shadow (holes).
	Light bool
}

// DrawOutlines renders parts into an offscreen mask and composites
// outlines onto framebuffer fb: a line radius px wide (radiusB for channel
// 2, the hover), and for channel 0 (the selection) a glow fading out to
// glow px at glowAlpha.
func (s *Scene) DrawOutlines(parts []MaskPart, fb mac.Target, x, y, w, h int32, radius, radiusB, glow, glowAlpha float32, colR, colG, colB [3]float32) {
	if len(parts) == 0 || w <= 0 || h <= 0 {
		return
	}
	if s.maskTarget == 0 || s.maskW != w || s.maskH != h {
		if s.maskTarget != 0 {
			s.maskTarget.Free()
		}
		s.maskTarget = mac.NewTarget(int(w), int(h), 1, 1, true)
		s.maskW, s.maskH = w, h
	}
	mac.BindTarget(s.maskTarget)
	mac.Viewport(0, 0, int(w), int(h))
	mac.SetDepthTest(false)
	mac.SetBlend(false)
	mac.ClearColor(0, 0, 0, 0)
	mac.Clear(true, true)
	// draw lower-priority channels first so selection wins
	for _, ch := range []int{2, 0, 1} {
		for _, p := range parts {
			if p.Channel != ch {
				continue
			}
			t := [4]float32{0, 0, 0, 1}
			t[ch] = 1
			s.DrawMesh(p.Mesh, Style{Model: p.M, Flat: true, Tint: t, Clip: p.Clip})
		}
	}
	mac.BindTarget(fb)
	mac.Viewport(int(x), int(y), int(w), int(h))
	mac.SetBlend(true)
	s.outlineProg.Use()
	mac.BindTexture(mac.Texture(s.maskTarget))
	mac.Uniform2f(s.ou["uTexel"], 1/float32(w), 1/float32(h))
	mac.Uniform1f(s.ou["uRadius"], radius)
	mac.Uniform1f(s.ou["uRadiusB"], radiusB)
	mac.Uniform1f(s.ou["uGlow"], glow)
	mac.Uniform1f(s.ou["uGlowAlpha"], glowAlpha)
	mac.Uniform3f(s.ou["uColR"], colR[0], colR[1], colR[2])
	mac.Uniform3f(s.ou["uColG"], colG[0], colG[1], colG[2])
	mac.Uniform3f(s.ou["uColB"], colB[0], colB[1], colB[2])
	mac.VertexBuffer(s.quadVBO)
	mac.Draw(mac.Triangles, 0, 6)
}

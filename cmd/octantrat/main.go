// Command rat renders a rotating 3D rat in the terminal using octant graphics.
//
// Loads a Wavefront OBJ mesh and a JPEG UV texture (defaults: rat.obj, rat.jpg)
// and animates the model rotating around any combination of axes.
//
// Keyboard controls:
//
//	q / ESC / Ctrl-C   Quit
//	w                  Toggle wireframe mode
//	x                  Toggle X-axis rotation (pitch)
//	y                  Toggle Y-axis rotation (yaw/spin)  [on by default]
//	z                  Toggle Z-axis rotation (roll)
//	+  /  =            Speed up rotation
//	-                  Slow down rotation
//	r                  Reset rotation speed
//	h                  Toggle debug overlay (controls + model info + rotation state)
//	[  /  ]            Scale model down / up
package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/reynoldsme/octant"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"golang.org/x/term"
)

//go:embed rat.obj
var embeddedOBJ []byte

//go:embed rat.jpg
var embeddedTex []byte

// ── OBJ types ──────────────────────────────────────────────────────────────

type vec3 struct{ x, y, z float64 }
type vec2 struct{ u, v float64 }
type face struct {
	v  [3]int // vertex indices (0-based)
	vt [3]int // UV indices (0-based)
}

type mesh struct {
	verts []vec3
	uvs   []vec2
	faces []face
}

// camVert is a vertex in camera space (z negative = in front of camera) with
// its UV coordinates carried along for clipping and interpolation.
type camVert struct {
	x, y, z float64
	u, v    float64
}

// ── OBJ parser ─────────────────────────────────────────────────────────────

// parseOBJ reads a Wavefront OBJ stream, extracting positions, UV coordinates,
// and triangulated faces.  N-gons are fan-triangulated.  Normal indices are
// accepted in the v/vt/vn token format but ignored.
func parseOBJ(r io.Reader) (*mesh, error) {
	parseFloat := func(s string) float64 {
		v, _ := strconv.ParseFloat(s, 64)
		return v
	}
	// parseToken handles v, v/vt, v/vt/vn, v//vn  → returns 0-based indices.
	parseToken := func(s string) (vi, vti int) {
		parts := strings.SplitN(s, "/", 3)
		if n, _ := strconv.Atoi(parts[0]); n > 0 {
			vi = n - 1
		}
		if len(parts) > 1 {
			if n, _ := strconv.Atoi(parts[1]); n > 0 {
				vti = n - 1
			}
		}
		return
	}

	m := &mesh{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "v":
			if len(fields) >= 4 {
				m.verts = append(m.verts, vec3{
					parseFloat(fields[1]),
					parseFloat(fields[2]),
					parseFloat(fields[3]),
				})
			}
		case "vt":
			if len(fields) >= 3 {
				m.uvs = append(m.uvs, vec2{
					parseFloat(fields[1]),
					parseFloat(fields[2]),
				})
			}
		case "f":
			if len(fields) < 4 {
				continue
			}
			v0, vt0 := parseToken(fields[1])
			for i := 2; i < len(fields)-1; i++ {
				v1, vt1 := parseToken(fields[i])
				v2, vt2 := parseToken(fields[i+1])
				m.faces = append(m.faces, face{
					v:  [3]int{v0, v1, v2},
					vt: [3]int{vt0, vt1, vt2},
				})
			}
		}
	}
	return m, sc.Err()
}

// ── Geometry helpers ────────────────────────────────────────────────────────

func modelBounds(m *mesh) (min, max vec3) {
	min, max = m.verts[0], m.verts[0]
	for _, v := range m.verts[1:] {
		if v.x < min.x {
			min.x = v.x
		}
		if v.y < min.y {
			min.y = v.y
		}
		if v.z < min.z {
			min.z = v.z
		}
		if v.x > max.x {
			max.x = v.x
		}
		if v.y > max.y {
			max.y = v.y
		}
		if v.z > max.z {
			max.z = v.z
		}
	}
	return
}

func mini(a, b int) int {
	if b < a {
		return b
	}
	return a
}
func maxi(a, b int) int {
	if b > a {
		return b
	}
	return a
}
func min3i(a, b, c int) int { return mini(mini(a, b), c) }
func max3i(a, b, c int) int { return maxi(maxi(a, b), c) }

func absi(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

// drawLine plots a line from (x0,y0) to (x1,y1) using Bresenham's algorithm,
// clipping to the canvas bounds [0,w) × [0,h).
func drawLine(img *image.RGBA, x0, y0, x1, y1, w, h int, c color.RGBA) {
	dx := absi(x1 - x0)
	dy := absi(y1 - y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy
	for {
		if x0 >= 0 && x0 < w && y0 >= 0 && y0 < h {
			img.SetRGBA(x0, y0, c)
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

// clipNear clips a triangle against the camera near plane (z = -near in camera
// space) using Sutherland-Hodgman and returns 0, 1, or 2 replacement triangles.
// UV coordinates are linearly interpolated at each new clip vertex.
func clipNear(tri [3]camVert, near float64) [][3]camVert {
	in := [3]bool{tri[0].z < -near, tri[1].z < -near, tri[2].z < -near}
	n := 0
	for _, b := range in {
		if b {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	if n == 3 {
		return [][3]camVert{tri}
	}

	// Interpolate a new vertex on edge a→b where it crosses z = -near.
	cut := func(a, b camVert) camVert {
		t := (-near - a.z) / (b.z - a.z)
		return camVert{
			x: a.x + t*(b.x-a.x),
			y: a.y + t*(b.y-a.y),
			z: -near,
			u: a.u + t*(b.u-a.u),
			v: a.v + t*(b.v-a.v),
		}
	}

	if n == 1 {
		// Rotate so the single inside vertex is tri[0].
		for !in[0] {
			tri[0], tri[1], tri[2] = tri[1], tri[2], tri[0]
			in[0], in[1], in[2] = in[1], in[2], in[0]
		}
		// Clip edges 0→1 and 0→2; result is one triangle.
		return [][3]camVert{{tri[0], cut(tri[0], tri[1]), cut(tri[0], tri[2])}}
	}

	// n == 2: rotate so the single outside vertex is tri[0].
	for in[0] {
		tri[0], tri[1], tri[2] = tri[1], tri[2], tri[0]
		in[0], in[1], in[2] = in[1], in[2], in[0]
	}
	// tri[1] and tri[2] are inside; clip edges 1→0 and 2→0.
	// The clipped quad [tri[1], tri[2], q, p] becomes two triangles.
	p := cut(tri[1], tri[0])
	q := cut(tri[2], tri[0])
	return [][3]camVert{
		{tri[1], tri[2], q},
		{tri[1], q, p},
	}
}

// ── Renderer ────────────────────────────────────────────────────────────────

const (
	fovY        = 55.0 * math.Pi / 180.0 // vertical field of view
	initAngleX  = 18.0 * math.Pi / 180.0 // initial X tilt so the rat isn't viewed flat-on
	initAngleY  = math.Pi / 2             // initial Y angle shows the side profile first
)

// wireColor is the edge color used in wireframe mode.
var wireColor = color.RGBA{0, 210, 70, 255}

// renderMesh draws the mesh onto img using a software rasteriser.
// angleX/Y/Z are the current rotation angles (radians) around each axis.
// Rotation is applied in Y → X → Z order (yaw, pitch, roll).
// scale is a uniform scale factor applied around the model centroid.
// cx/cy/cz is the model centroid (translated to origin before rendering).
// maxExt is the largest half-extent of the model's bounding box.
// wireframe selects edge-only rendering instead of filled triangles.
func renderMesh(img *image.RGBA, m *mesh, tex image.Image, angleX, angleY, angleZ, scale float64, cx, cy, cz, maxExt float64, wireframe bool) {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	aspect := float64(w) / float64(h)

	// Clear canvas to opaque black.
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = 0
		img.Pix[i+1] = 0
		img.Pix[i+2] = 0
		img.Pix[i+3] = 255
	}

	tanHalfFovY := math.Tan(fovY / 2.0)
	tanHalfFovX := tanHalfFovY * aspect
	camDist := maxExt / (tanHalfFovX * 0.80)

	zbuf := make([]float64, w*h)
	for i := range zbuf {
		zbuf[i] = math.MaxFloat64
	}

	sinX, cosX := math.Sin(angleX), math.Cos(angleX)
	sinY, cosY := math.Sin(angleY), math.Cos(angleY)
	sinZ, cosZ := math.Sin(angleZ), math.Cos(angleZ)

	texBounds := tex.Bounds()
	texW := texBounds.Dx()
	texH := texBounds.Dy()

	// transform maps a world-space vertex + UV into camera space.
	// Camera sits at (0,0,camDist); camera-space z is negative for visible points.
	transform := func(wv vec3, uv vec2) camVert {
		x := (wv.x - cx) * scale
		y := (wv.y - cy) * scale
		z := (wv.z - cz) * scale
		x, z = x*cosY-z*sinY, x*sinY+z*cosY // Y rotation (yaw)
		y, z = y*cosX-z*sinX, y*sinX+z*cosX // X rotation (pitch)
		x, y = x*cosZ-y*sinZ, x*sinZ+y*cosZ // Z rotation (roll)
		return camVert{x: x, y: y, z: z - camDist, u: uv.u, v: uv.v}
	}

	// projectCV perspective-projects a camera-space vertex to screen pixels + NDC.
	// Caller must ensure cv.z < 0 (clipNear guarantees this for all emitted verts).
	projectCV := func(cv camVert) (sx, sy int, ndcX, ndcY, depth float64) {
		depth = -cv.z
		ndcX = cv.x / (depth * tanHalfFovX)
		ndcY = cv.y / (depth * tanHalfFovY)
		sx = int((ndcX+1.0)*0.5*float64(w) + 0.5)
		sy = int((1.0-ndcY)*0.5*float64(h) + 0.5)
		return
	}

	for _, fc := range m.faces {
		cv0 := transform(m.verts[fc.v[0]], m.uvs[fc.vt[0]])
		cv1 := transform(m.verts[fc.v[1]], m.uvs[fc.vt[1]])
		cv2 := transform(m.verts[fc.v[2]], m.uvs[fc.vt[2]])

		for _, tri := range clipNear([3]camVert{cv0, cv1, cv2}, 1.0) {
			x0, y0, nx0, ny0, d0 := projectCV(tri[0])
			x1, y1, nx1, ny1, d1 := projectCV(tri[1])
			x2, y2, nx2, ny2, d2 := projectCV(tri[2])

			// Backface culling in float NDC space (Y-up).
			// Front-facing (CCW in NDC) has positive area; skip back-faces and
			// degenerate triangles that collapsed to zero area after clipping.
			ndcArea := (nx1-nx0)*(ny2-ny0) - (ny1-ny0)*(nx2-nx0)
			if ndcArea <= 0 {
				continue
			}

			if wireframe {
				drawLine(img, x0, y0, x1, y1, w, h, wireColor)
				drawLine(img, x1, y1, x2, y2, w, h, wireColor)
				drawLine(img, x2, y2, x0, y0, w, h, wireColor)
				continue
			}

			// Integer screen-space area for barycentric weights.
			// Front-facing in screen space (Y-down) is CW = negative area.
			area := (x1-x0)*(y2-y0) - (y1-y0)*(x2-x0)
			if area == 0 {
				continue // pixel-scale degenerate; skip to avoid divide-by-zero
			}
			areaF := float64(area)

			// Perspective-correct UV: pre-divide by depth at each vertex.
			w0inv, w1inv, w2inv := 1.0/d0, 1.0/d1, 1.0/d2
			u0w := tri[0].u * w0inv
			v0w := tri[0].v * w0inv
			u1w := tri[1].u * w1inv
			v1w := tri[1].v * w1inv
			u2w := tri[2].u * w2inv
			v2w := tri[2].v * w2inv

			// Bounding box clipped to canvas.
			minX := max(min3i(x0, x1, x2), 0)
			maxX := min(max3i(x0, x1, x2), w-1)
			minY := max(min3i(y0, y1, y2), 0)
			maxY := min(max3i(y0, y1, y2), h-1)
			if minX > maxX || minY > maxY {
				continue
			}

			for py := minY; py <= maxY; py++ {
				for px := minX; px <= maxX; px++ {
					bw0 := float64((x2-x1)*(py-y1)-(y2-y1)*(px-x1)) / areaF
					bw1 := float64((x0-x2)*(py-y2)-(y0-y2)*(px-x2)) / areaF
					bw2 := 1.0 - bw0 - bw1
					if bw0 < 0 || bw1 < 0 || bw2 < 0 {
						continue
					}

					depthInv := bw0*w0inv + bw1*w1inv + bw2*w2inv
					pixDepth := 1.0 / depthInv

					idx := py*w + px
					if pixDepth >= zbuf[idx] {
						continue
					}
					zbuf[idx] = pixDepth

					u := (bw0*u0w + bw1*u1w + bw2*u2w) * pixDepth
					v := (bw0*v0w + bw1*v1w + bw2*v2w) * pixDepth

					u = math.Mod(u, 1.0)
					if u < 0 {
						u += 1.0
					}
					v = math.Mod(v, 1.0)
					if v < 0 {
						v += 1.0
					}

					tx := int(u * float64(texW))
					ty := int((1.0-v)*float64(texH))
					if tx >= texW {
						tx = texW - 1
					}
					if ty >= texH {
						ty = texH - 1
					}

					r, g, b, a := tex.At(texBounds.Min.X+tx, texBounds.Min.Y+ty).RGBA()
					img.SetRGBA(px, py, color.RGBA{
						uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8),
					})
				}
			}
		}
	}
}

// ── Debug overlay ───────────────────────────────────────────────────────────

// drawDebugOverlay renders a heads-up panel in the top-left corner of img
// showing model statistics, per-axis rotation state, and a key-binding reminder.
func drawDebugOverlay(img *image.RGBA, m *mesh, objFile, texFile string,
	angleX, angleY, angleZ, rotSpeed, scale float64,
	rotX, rotY, rotZ, wireframe bool) {

	const (
		padX  = 8
		padY  = 7
		lineH = 16 // ~13 px font + 3 px gap
	)

	// Colours.
	label := color.RGBA{150, 150, 165, 255} // muted key labels
	value := color.RGBA{220, 220, 220, 255} // bright values
	on    := color.RGBA{0, 210, 70, 255}    // active axis (matches wireColor)
	off   := color.RGBA{75, 75, 85, 255}    // inactive axis
	hint  := color.RGBA{95, 95, 110, 255}   // key-binding hints

	stateCol := func(active bool) color.RGBA {
		if active {
			return on
		}
		return off
	}
	stateStr := func(active bool) string {
		if active {
			return "SPIN"
		}
		return "off"
	}
	deg := func(r float64) string { return fmt.Sprintf("%7.1f°", r*180/math.Pi) }

	// Each row is a sequence of (text, colour) segments drawn left-to-right,
	// with the font Dot advancing naturally between segments.
	type seg struct {
		t string
		c color.RGBA
	}
	rows := [][]seg{
		{
			{"OBJ: ", label}, {objFile, value},
			{"   TEX: ", label}, {texFile, value},
		},
		{
			{"Verts: ", label}, {fmt.Sprintf("%d", len(m.verts)), value},
			{"   UVs: ", label}, {fmt.Sprintf("%d", len(m.uvs)), value},
			{"   Faces: ", label}, {fmt.Sprintf("%d", len(m.faces)), value},
		},
		nil, // blank row
		{
			{"X:", label}, {deg(angleX), value}, {" [" + stateStr(rotX) + "]", stateCol(rotX)},
			{"   Y:", label}, {deg(angleY), value}, {" [" + stateStr(rotY) + "]", stateCol(rotY)},
			{"   Z:", label}, {deg(angleZ), value}, {" [" + stateStr(rotZ) + "]", stateCol(rotZ)},
		},
		{
			{"Speed: ", label}, {fmt.Sprintf("%.2f rad/s", rotSpeed), value},
			{"   Scale: ", label}, {fmt.Sprintf("%.2fx", scale), value},
			{"   Wireframe: ", label}, {stateStr(wireframe), stateCol(wireframe)},
		},
		nil, // blank row
		{{"[q]quit  [w]wire  [h]debug  [x/y/z]axes  [+/-/r]speed  [[/]]scale", hint}},
	}

	imgH := img.Bounds().Dy()
	face := basicfont.Face7x13

	// Draw each row's segments.
	d := &font.Drawer{Dst: img, Face: face}
	for i, row := range rows {
		if len(row) == 0 {
			continue
		}
		baseline := padY + (i+1)*lineH - 3
		if baseline >= imgH {
			break
		}
		d.Dot = fixed.P(padX, baseline)
		for _, s := range row {
			d.Src = image.NewUniform(s.c)
			d.DrawString(s.t)
		}
	}
}

// ── Key handling ────────────────────────────────────────────────────────────

func readKeys(keys chan<- byte) {
	br := bufio.NewReader(os.Stdin)
	for {
		b, err := br.ReadByte()
		if err != nil {
			return
		}
		keys <- b
	}
}

// ── Main ────────────────────────────────────────────────────────────────────

func main() {
	objFile := flag.String("obj", "", "Wavefront OBJ file (default: embedded rat.obj)")
	texFile := flag.String("tex", "", "UV texture image (default: embedded rat.jpg)")
	speed := flag.Float64("speed", 1.0, "rotation speed in radians per second")
	flag.Parse()

	// Resolve OBJ source: embedded by default, external file if -obj is given.
	objName := "rat.obj"
	var objReader io.Reader = bytes.NewReader(embeddedOBJ)
	if *objFile != "" {
		f, err := os.Open(*objFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "rat: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		objReader = f
		objName = *objFile
	}

	m, err := parseOBJ(objReader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rat: %v\n", err)
		os.Exit(1)
	}
	if len(m.verts) == 0 || len(m.faces) == 0 {
		fmt.Fprintf(os.Stderr, "rat: no geometry in %s\n", objName)
		os.Exit(1)
	}

	// Resolve texture source: embedded by default, external file if -tex is given.
	texName := "rat.jpg"
	var texReader io.Reader = bytes.NewReader(embeddedTex)
	if *texFile != "" {
		f, err := os.Open(*texFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "rat: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		texReader = f
		texName = *texFile
	}

	tex, _, err := image.Decode(texReader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rat: decode texture: %v\n", err)
		os.Exit(1)
	}

	// Compute model centroid and largest half-extent.
	minB, maxB := modelBounds(m)
	cx := (minB.x + maxB.x) / 2
	cy := (minB.y + maxB.y) / 2
	cz := (minB.z + maxB.z) / 2
	extX := (maxB.x - minB.x) / 2
	extY := (maxB.y - minB.y) / 2
	extZ := (maxB.z - minB.z) / 2
	maxExt := math.Max(extX, math.Max(extY, extZ))

	// Switch stdin to raw mode.
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rat: raw mode: %v\n", err)
		os.Exit(1)
	}
	defer term.Restore(fd, oldState)

	fmt.Print("\x1b[2J\x1b[H\x1b[?25l")
	defer fmt.Print("\x1b[0m\x1b[?25h\r\n")

	keys := make(chan byte, 128)
	go readKeys(keys)

	t := &octant.Terminal{W: os.Stdout}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(time.Second / 15)
	defer ticker.Stop()

	rotSpeed := *speed
	// Per-axis angles. X starts at the viewing tilt; Y starts at the side profile.
	angleX := initAngleX
	angleY := initAngleY
	angleZ := 0.0
	// Per-axis rotation enable. Y spins by default; X and Z start frozen.
	rotX, rotY, rotZ := false, true, false
	scale := 1.0
	last := time.Now()
	wireframe := false
	debug := false

	for {
		select {
		case <-sigs:
			return

		case b, ok := <-keys:
			if !ok {
				return
			}
			switch b {
			case 'q', 'Q', '\x1b', '\x03':
				return
			case 'w', 'W':
				wireframe = !wireframe
			case 'h', 'H':
				debug = !debug
			case 'x', 'X':
				rotX = !rotX
			case 'y', 'Y':
				rotY = !rotY
			case 'z', 'Z':
				rotZ = !rotZ
			case '+', '=':
				rotSpeed *= 1.2
			case '-':
				rotSpeed /= 1.2
			case 'r':
				rotSpeed = *speed
			case ']':
				scale = min(scale*1.1, 10.0)
			case '[':
				scale = max(scale/1.1, 0.05)
			}

		case now := <-ticker.C:
			dt := now.Sub(last).Seconds()
			last = now
			if rotX {
				angleX += rotSpeed * dt
			}
			if rotY {
				angleY += rotSpeed * dt
			}
			if rotZ {
				angleZ += rotSpeed * dt
			}

			termW, termH, err := term.GetSize(int(os.Stdout.Fd()))
			if err != nil || termW <= 0 || termH <= 0 {
				continue
			}
			if termH > 1 {
				termH--
			}
			canvasW := termW * 2
			canvasH := termH * 4

			canvas := image.NewRGBA(image.Rect(0, 0, canvasW, canvasH))
			renderMesh(canvas, m, tex, angleX, angleY, angleZ, scale, cx, cy, cz, maxExt, wireframe)
			if debug {
				drawDebugOverlay(canvas, m, objName, texName,
					angleX, angleY, angleZ, rotSpeed, scale,
					rotX, rotY, rotZ, wireframe)
			}
			t.DrawFrame(canvas)
		}
	}
}

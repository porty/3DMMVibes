package mm

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"sort"
)

// ibsetColors is a fixed palette of distinct colors for body-part-set rendering.
// Body parts are colored by their ibset index (ibset % len(ibsetColors)).
var ibsetColors = []color.NRGBA{
	{R: 200, G: 160, B: 120, A: 255}, // skin tone
	{R: 80, G: 100, B: 180, A: 255},  // blue
	{R: 60, G: 130, B: 60, A: 255},   // green
	{R: 220, G: 80, B: 80, A: 255},   // red
	{R: 180, G: 180, B: 60, A: 255},  // yellow
	{R: 140, G: 80, B: 160, A: 255},  // purple
	{R: 80, G: 180, B: 180, A: 255},  // cyan
	{R: 220, G: 140, B: 60, A: 255},  // orange
}

// RenderParams holds the parameters needed to render a single actor template.
type RenderParams struct {
	Width      int
	Height     int
	ActionCHID uint32
	CelIdx     int
}

// worldTriangle is one projected triangle ready for rasterization.
type worldTriangle struct {
	sx, sy [3]int  // screen pixel coords of the 3 vertices
	avgZ   float64 // mean camera-space Z (used for painter's algorithm sort)
	col    color.NRGBA
}

// RenderTemplate loads and renders one TMPL chunk into an NRGBA image.
func RenderTemplate(cf *ChunkyFile, r *os.File, cno uint32, p RenderParams) (*image.NRGBA, error) {
	tmpl, err := LoadTemplate(cf, r, cno)
	if err != nil {
		return nil, err
	}

	// Select action.
	ad, ok := tmpl.Actions[p.ActionCHID]
	if !ok {
		for _, a := range tmpl.Actions {
			ad = a
			break
		}
	}
	if ad == nil {
		return nil, fmt.Errorf("no actions found")
	}
	if len(ad.Cels) == 0 {
		return nil, fmt.Errorf("action has no cels")
	}
	celIdx := p.CelIdx
	if celIdx >= len(ad.Cels) {
		celIdx = 0
	}
	cel := ad.Cels[celIdx]

	// Collect world-space triangles for all body parts.
	type triData struct {
		world [3]Vec3
		ibset int
	}
	var allTris []triData

	for partIdx, cps := range cel.Parts {
		model, ok := tmpl.Models[uint32(cps.ChidModl)]
		if !ok || model == nil || len(model.Faces) == 0 {
			continue
		}

		var mat BMAT34
		if cps.IMat34 >= 0 && cps.IMat34 < len(ad.Transforms) {
			mat = ad.Transforms[cps.IMat34]
		}

		ibset := 0
		if partIdx < len(tmpl.IBSets) {
			ibset = tmpl.IBSets[partIdx]
		}

		worldVerts := make([]Vec3, len(model.Verts))
		for vi, bv := range model.Verts {
			worldVerts[vi] = applyBMAT34(bv.Pos, mat)
		}

		for _, face := range model.Faces {
			v0, v1, v2 := face.V[0], face.V[1], face.V[2]
			if v0 >= len(worldVerts) || v1 >= len(worldVerts) || v2 >= len(worldVerts) {
				continue
			}
			allTris = append(allTris, triData{
				world: [3]Vec3{worldVerts[v0], worldVerts[v1], worldVerts[v2]},
				ibset: ibset,
			})
		}
	}

	if len(allTris) == 0 {
		return nil, fmt.Errorf("no renderable geometry")
	}

	// Compute world bounding box for auto-camera.
	minX, minY, minZ := allTris[0].world[0].X, allTris[0].world[0].Y, allTris[0].world[0].Z
	maxX, maxY, maxZ := minX, minY, minZ
	for _, td := range allTris {
		for _, v := range td.world {
			minX = math.Min(minX, v.X)
			minY = math.Min(minY, v.Y)
			minZ = math.Min(minZ, v.Z)
			maxX = math.Max(maxX, v.X)
			maxY = math.Max(maxY, v.Y)
			maxZ = math.Max(maxZ, v.Z)
		}
	}
	cx := (minX + maxX) / 2
	cy := (minY + maxY) / 2
	cz := (minZ + maxZ) / 2
	diag := math.Sqrt((maxX-minX)*(maxX-minX) + (maxY-minY)*(maxY-minY) + (maxZ-minZ)*(maxZ-minZ))
	if diag == 0 {
		diag = 1
	}

	dist := diag * 2.0
	camPos := Vec3{X: cx, Y: cy + diag*0.2, Z: cz + dist}

	var camM [4][3]float64
	camM[0] = [3]float64{1, 0, 0}
	camM[1] = [3]float64{0, 1, 0}
	camM[2] = [3]float64{0, 0, 1}
	camM[3] = [3]float64{camPos.X, camPos.Y, camPos.Z}

	fovRad := 40.0 * math.Pi / 180.0
	cam := CamParams{M: camM, FOVRad: fovRad, W: p.Width, H: p.Height}

	// Project triangles.
	var screenTris []worldTriangle
	for _, td := range allTris {
		col := ibsetColors[td.ibset%len(ibsetColors)]

		sx0, sy0, ok0 := projectWorldPoint(cam, td.world[0].X, td.world[0].Y, td.world[0].Z)
		sx1, sy1, ok1 := projectWorldPoint(cam, td.world[1].X, td.world[1].Y, td.world[1].Z)
		sx2, sy2, ok2 := projectWorldPoint(cam, td.world[2].X, td.world[2].Y, td.world[2].Z)
		if !ok0 && !ok1 && !ok2 {
			continue
		}

		avgZ := (td.world[0].Z + td.world[1].Z + td.world[2].Z) / 3.0
		screenTris = append(screenTris, worldTriangle{
			sx:   [3]int{sx0, sx1, sx2},
			sy:   [3]int{sy0, sy1, sy2},
			avgZ: avgZ,
			col:  col,
		})
	}

	// Sort back-to-front.
	sort.Slice(screenTris, func(i, j int) bool {
		return screenTris[i].avgZ > screenTris[j].avgZ
	})

	// Rasterize.
	img := image.NewNRGBA(image.Rect(0, 0, p.Width, p.Height))
	black := color.NRGBA{0, 0, 0, 255}
	for y := range p.Height {
		for x := range p.Width {
			img.SetNRGBA(x, y, black)
		}
	}
	for _, tri := range screenTris {
		fillTriangle(img, tri.sx, tri.sy, tri.col)
	}
	return img, nil
}

// fillTriangle rasterizes a flat-shaded triangle onto img using a scanline fill.
func fillTriangle(img *image.NRGBA, sx, sy [3]int, col color.NRGBA) {
	// Sort vertices by Y.
	v := [3][2]int{{sx[0], sy[0]}, {sx[1], sy[1]}, {sx[2], sy[2]}}
	if v[0][1] > v[1][1] {
		v[0], v[1] = v[1], v[0]
	}
	if v[1][1] > v[2][1] {
		v[1], v[2] = v[2], v[1]
	}
	if v[0][1] > v[1][1] {
		v[0], v[1] = v[1], v[0]
	}

	bounds := img.Bounds()

	fillSpan := func(y, x0, x1 int) {
		if y < bounds.Min.Y || y >= bounds.Max.Y {
			return
		}
		if x0 > x1 {
			x0, x1 = x1, x0
		}
		x0 = max(x0, bounds.Min.X)
		x1 = min(x1, bounds.Max.X-1)
		for x := x0; x <= x1; x++ {
			img.SetNRGBA(x, y, col)
		}
	}

	edgeX := func(ax, ay, bx, by, y int) int {
		if ay == by {
			return ax
		}
		return ax + (bx-ax)*(y-ay)/(by-ay)
	}

	y0, y1, y2 := v[0][1], v[1][1], v[2][1]
	x0, x1, x2 := v[0][0], v[1][0], v[2][0]

	for y := y0; y <= y1; y++ {
		fillSpan(y, edgeX(x0, y0, x1, y1, y), edgeX(x0, y0, x2, y2, y))
	}
	for y := y1 + 1; y <= y2; y++ {
		fillSpan(y, edgeX(x1, y1, x2, y2, y), edgeX(x0, y0, x2, y2, y))
	}
}

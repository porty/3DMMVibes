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
	YawDeg     float64 // Y-axis rotation of actor in degrees (0 = default view)
	// Palette is the GLCR palette used for texture index → RGB lookup.
	// A nil/zero palette disables texture rendering (ibsetColors fallback is used).
	Palette Palette
}

// ActorPalette returns the color palette used for actor template rendering:
// black background plus one color per body-part-set index.
func ActorPalette() color.Palette {
	p := color.Palette{color.NRGBA{R: 0, G: 0, B: 0, A: 255}}
	for _, c := range ibsetColors {
		p = append(p, c)
	}
	return p
}

// worldTriangle is one projected triangle ready for rasterization.
type worldTriangle struct {
	sx, sy [3]int     // screen pixel coords of the 3 vertices
	su, sv [3]float64 // texture UV at each vertex (only used when mat != nil && mat.HasTexture)
	avgZ   float64    // mean camera-space Z (used for painter's algorithm sort)
	col    color.NRGBA
	mat    *Material // nil → use col; non-nil textured → UV-sample TMAP
	pal    Palette   // palette for texture index → RGB (copied from RenderParams)
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
		uv    [3][2]float64 // UV at each vertex (texture coords, not world positions)
		ibset int
		mat   *Material
	}
	var allTris []triData

	for partIdx, cps := range cel.Parts {
		model, ok := tmpl.Models[uint32(cps.ChidModl)]
		if !ok || model == nil || len(model.Faces) == 0 {
			continue
		}

		ibset := 0
		if partIdx < len(tmpl.IBSets) {
			ibset = tmpl.IBSets[partIdx]
		}

		// Look up material for this body part.
		var mat *Material
		if cmtl, ok := tmpl.Costumes[ibset]; ok {
			pos := 0
			if partIdx < len(tmpl.IBSetPartIndex) {
				pos = tmpl.IBSetPartIndex[partIdx]
			}
			if pos < len(cmtl.Parts) {
				mat = cmtl.Parts[pos]
			}
		}

		worldVerts := make([]Vec3, len(model.Verts))
		for vi, bv := range model.Verts {
			// Walk up the body-part hierarchy, applying each local-to-parent
			// transform in order (innermost part first, up to the root part).
			// Each BMAT34 in GLXF is relative to the parent body part's space,
			// so we must compose all ancestors' transforms to get world space.
			pos := bv.Pos
			cur := partIdx
			for cur >= 0 && cur < len(cel.Parts) {
				imat := cel.Parts[cur].IMat34
				if imat >= 0 && imat < len(ad.Transforms) {
					pos = applyBMAT34(pos, ad.Transforms[imat])
				}
				if cur < len(tmpl.ParentParts) {
					cur = tmpl.ParentParts[cur]
				} else {
					cur = -1
				}
			}
			worldVerts[vi] = pos
		}

		for _, face := range model.Faces {
			v0, v1, v2 := face.V[0], face.V[1], face.V[2]
			if v0 >= len(worldVerts) || v1 >= len(worldVerts) || v2 >= len(worldVerts) {
				continue
			}
			allTris = append(allTris, triData{
				world: [3]Vec3{worldVerts[v0], worldVerts[v1], worldVerts[v2]},
				uv: [3][2]float64{
					{model.Verts[v0].U, model.Verts[v0].V},
					{model.Verts[v1].U, model.Verts[v1].V},
					{model.Verts[v2].U, model.Verts[v2].V},
				},
				ibset: ibset,
				mat:   mat,
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

	// Apply Y-axis rotation around the bounding-box center before projecting.
	if p.YawDeg != 0 {
		theta := p.YawDeg * math.Pi / 180.0
		cosT, sinT := math.Cos(theta), math.Sin(theta)
		for i := range allTris {
			for j := range allTris[i].world {
				wx := allTris[i].world[j].X - cx
				wz := allTris[i].world[j].Z - cz
				allTris[i].world[j].X = wx*cosT - wz*sinT + cx
				allTris[i].world[j].Z = wx*sinT + wz*cosT + cz
			}
		}
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
		// Determine fill color. Textured materials set mat but use col as fallback
		// (e.g. when palette is nil or texture lookup fails).
		var col color.NRGBA
		switch {
		case td.mat != nil && !td.mat.HasTexture:
			col = color.NRGBA{R: td.mat.R, G: td.mat.G, B: td.mat.B, A: 255}
		default:
			col = ibsetColors[td.ibset%len(ibsetColors)]
		}

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
			su:   [3]float64{td.uv[0][0], td.uv[1][0], td.uv[2][0]},
			sv:   [3]float64{td.uv[0][1], td.uv[1][1], td.uv[2][1]},
			avgZ: avgZ,
			col:  col,
			mat:  td.mat,
			pal:  p.Palette,
		})
	}

	// Sort back-to-front.
	sort.Slice(screenTris, func(i, j int) bool {
		return screenTris[i].avgZ > screenTris[j].avgZ
	})

	// Rasterize.
	img := image.NewNRGBA(image.Rect(0, 0, p.Width, p.Height))
	blank := color.NRGBA{0, 0, 0, 0}
	for y := range p.Height {
		for x := range p.Width {
			img.SetNRGBA(x, y, blank)
		}
	}
	for _, tri := range screenTris {
		fillTriangle(img, tri.sx, tri.sy, tri.su, tri.sv, tri.col, tri.mat, tri.pal)
	}
	return img, nil
}

// fillTriangle rasterizes a triangle onto img using a scanline fill.
// For solid-color triangles (mat == nil or !mat.HasTexture) it uses col directly.
// For textured triangles it interpolates UV linearly and samples mat.Tex via pal.
func fillTriangle(img *image.NRGBA, sx, sy [3]int, su, sv [3]float64, col color.NRGBA, mat *Material, pal Palette) {
	textured := mat != nil && mat.HasTexture && len(pal.Colors) > 0

	// Sort vertices by Y (bubble sort on the 3-element array).
	type vt struct {
		x, y int
		u, v float64
	}
	verts := [3]vt{
		{sx[0], sy[0], su[0], sv[0]},
		{sx[1], sy[1], su[1], sv[1]},
		{sx[2], sy[2], su[2], sv[2]},
	}
	if verts[0].y > verts[1].y {
		verts[0], verts[1] = verts[1], verts[0]
	}
	if verts[1].y > verts[2].y {
		verts[1], verts[2] = verts[2], verts[1]
	}
	if verts[0].y > verts[1].y {
		verts[0], verts[1] = verts[1], verts[0]
	}

	bounds := img.Bounds()

	// edgeX interpolates the X position along an edge at scanline y.
	edgeX := func(ax, ay, bx, by, y int) int {
		if ay == by {
			return ax
		}
		return ax + (bx-ax)*(y-ay)/(by-ay)
	}
	// edgeF interpolates a float attribute along an edge at scanline y.
	edgeF := func(af float64, ay int, bf float64, by int, y int) float64 {
		if ay == by {
			return af
		}
		t := float64(y-ay) / float64(by-ay)
		return af + (bf-af)*t
	}

	fillSpan := func(y, x0, x1 int, u0, u1, v0, v1 float64) {
		if y < bounds.Min.Y || y >= bounds.Max.Y {
			return
		}
		if x0 > x1 {
			x0, x1 = x1, x0
			u0, u1 = u1, u0
			v0, v1 = v1, v0
		}
		x0 = max(x0, bounds.Min.X)
		x1 = min(x1, bounds.Max.X-1)
		if !textured {
			for x := x0; x <= x1; x++ {
				img.SetNRGBA(x, y, col)
			}
			return
		}
		// Interpolate UV across the span and sample the texture.
		spanW := x1 - x0
		for x := x0; x <= x1; x++ {
			var u, v float64
			if spanW > 0 {
				t := float64(x-x0) / float64(spanW)
				u = u0 + (u1-u0)*t
				v = v0 + (v1-v0)*t
			} else {
				u, v = u0, v0
			}
			// Wrap UV to [0,1).
			u -= math.Floor(u)
			v -= math.Floor(v)
			ui := int(u * float64(mat.Tex.Width))
			vi := int(v * float64(mat.Tex.Height))
			if ui >= mat.Tex.Width {
				ui = mat.Tex.Width - 1
			}
			if vi >= mat.Tex.Height {
				vi = mat.Tex.Height - 1
			}
			palIdx := int(mat.Tex.Pixels[vi*mat.Tex.RowBytes+ui])
			if palIdx < len(pal.Colors) {
				r, g, b, a := pal.Colors[palIdx].RGBA()
				img.SetNRGBA(x, y, color.NRGBA{
					R: uint8(r >> 8),
					G: uint8(g >> 8),
					B: uint8(b >> 8),
					A: uint8(a >> 8),
				})
			} else {
				img.SetNRGBA(x, y, col)
			}
		}
	}

	y0, y1, y2 := verts[0].y, verts[1].y, verts[2].y
	x0, x1, x2 := verts[0].x, verts[1].x, verts[2].x
	u0, u1, u2 := verts[0].u, verts[1].u, verts[2].u
	v0, v1, v2 := verts[0].v, verts[1].v, verts[2].v

	for y := y0; y <= y1; y++ {
		xa := edgeX(x0, y0, x1, y1, y)
		xb := edgeX(x0, y0, x2, y2, y)
		ua := edgeF(u0, y0, u1, y1, y)
		ub := edgeF(u0, y0, u2, y2, y)
		va := edgeF(v0, y0, v1, y1, y)
		vb := edgeF(v0, y0, v2, y2, y)
		fillSpan(y, xa, xb, ua, ub, va, vb)
	}
	for y := y1 + 1; y <= y2; y++ {
		xa := edgeX(x1, y1, x2, y2, y)
		xb := edgeX(x0, y0, x2, y2, y)
		ua := edgeF(u1, y1, u2, y2, y)
		ub := edgeF(u0, y0, u2, y2, y)
		va := edgeF(v1, y1, v2, y2, y)
		vb := edgeF(v0, y0, v2, y2, y)
		fillSpan(y, xa, xb, ua, ub, va, vb)
	}
}

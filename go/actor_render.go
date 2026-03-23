package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sort"

	"github.com/urfave/cli/v2"
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

// actorCommand returns the `actor` top-level command.
func actorCommand() *cli.Command {
	return &cli.Command{
		Name:  "actor",
		Usage: "Tools for working with actor templates",
		Subcommands: []*cli.Command{
			actorRenderCommand(),
		},
	}
}

// actorRenderCommand returns the `actor render` subcommand.
func actorRenderCommand() *cli.Command {
	return &cli.Command{
		Name:      "render",
		Usage:     "Render an actor template as a flat-shaded PNG",
		ArgsUsage: "<TMPLS.3CN>",
		Description: "Renders one cel of an actor template as a flat-shaded PNG.\n" +
			"Body parts are colored by their body-part-set index (ibset).\n" +
			"Character actor CNOs: 0x2010–0x203C in TMPLS.3CN.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "cno",
				Usage:    "CNO of the TMPL chunk (hex, e.g. 0x2010)",
				Required: true,
			},
			&cli.StringFlag{Name: "o", Usage: "Output PNG file (default: stdout)"},
			&cli.IntFlag{Name: "width", Value: 512, Usage: "Output image width in pixels"},
			&cli.IntFlag{Name: "height", Value: 512, Usage: "Output image height in pixels"},
			&cli.IntFlag{Name: "actn", Value: 0, Usage: "Action CHID to render"},
			&cli.IntFlag{Name: "cel", Value: 0, Usage: "Cel index within the action"},
		},
		Action: actorRenderAction,
	}
}

// worldTriangle is one projected triangle ready for rasterization.
type worldTriangle struct {
	sx, sy [3]int    // screen pixel coords of the 3 vertices
	avgZ   float64   // mean camera-space Z (used for painter's algorithm sort)
	col    color.NRGBA
}

func actorRenderAction(c *cli.Context) error {
	if c.NArg() < 1 {
		_ = cli.ShowSubcommandHelp(c)
		return cli.Exit("", 1)
	}

	// Parse --cno flag (hex string).
	cnoStr := c.String("cno")
	var cno uint32
	if _, err := fmt.Sscanf(cnoStr, "0x%x", &cno); err != nil {
		if _, err2 := fmt.Sscanf(cnoStr, "%x", &cno); err2 != nil {
			return fmt.Errorf("invalid --cno %q: expected hex like 0x2010", cnoStr)
		}
	}

	w, h := c.Int("width"), c.Int("height")
	actnCHID := uint32(c.Int("actn"))
	celIdx := c.Int("cel")

	// Open and parse the chunky file.
	path := c.Args().First()
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	cf, err := ParseChunkyFile(f)
	if err != nil {
		return err
	}

	// Load template.
	tmpl, err := LoadTemplate(cf, f, cno)
	if err != nil {
		return fmt.Errorf("loading TMPL/0x%08X: %w", cno, err)
	}

	// Select action.
	ad, ok := tmpl.Actions[actnCHID]
	if !ok {
		// Fall back to the first available action.
		for _, a := range tmpl.Actions {
			ad = a
			break
		}
		if ad == nil {
			return fmt.Errorf("no actions found in TMPL/0x%08X", cno)
		}
	}
	if len(ad.Cels) == 0 {
		return fmt.Errorf("action has no cels")
	}
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

		// Transform vertices to world space.
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
		return fmt.Errorf("no renderable geometry found in TMPL/0x%08X", cno)
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

	// Camera: positioned in front of the actor looking backward (-Z direction).
	// In BRender row-vector: cam.M[2] is the local Z axis (backward/forward),
	// cam.M[3] is the camera position, camera looks in the -local-Z direction.
	// We want the camera at (cx, cy + dy, cz + dist), looking toward center.
	// Local axes: X=right, Y=up, Z=backward.
	dist := diag * 2.0
	camPos := Vec3{X: cx, Y: cy + diag*0.2, Z: cz + dist}

	// Build camera matrix (identity rotation, translated position).
	var camM [4][3]float64
	camM[0] = [3]float64{1, 0, 0} // local X = world X (right)
	camM[1] = [3]float64{0, 1, 0} // local Y = world Y (up)
	camM[2] = [3]float64{0, 0, 1} // local Z = world Z (backward); camera looks toward -Z
	camM[3] = [3]float64{camPos.X, camPos.Y, camPos.Z}

	fovRad := 40.0 * math.Pi / 180.0
	cam := CamParams{M: camM, FOVRad: fovRad, W: w, H: h}

	// Project triangles.
	var screenTris []worldTriangle
	for _, td := range allTris {
		col := ibsetColors[td.ibset%len(ibsetColors)]

		sx0, sy0, ok0 := projectWorldPoint(cam, td.world[0].X, td.world[0].Y, td.world[0].Z)
		sx1, sy1, ok1 := projectWorldPoint(cam, td.world[1].X, td.world[1].Y, td.world[1].Z)
		sx2, sy2, ok2 := projectWorldPoint(cam, td.world[2].X, td.world[2].Y, td.world[2].Z)
		if !ok0 && !ok1 && !ok2 {
			continue // all verts behind camera
		}

		// Average camera-space Z for painter's algorithm (use world Z as proxy).
		avgZ := (td.world[0].Z + td.world[1].Z + td.world[2].Z) / 3.0

		screenTris = append(screenTris, worldTriangle{
			sx:   [3]int{sx0, sx1, sx2},
			sy:   [3]int{sy0, sy1, sy2},
			avgZ: avgZ,
			col:  col,
		})
	}

	// Sort back-to-front (painter's algorithm: larger Z = farther from cam = draw first).
	sort.Slice(screenTris, func(i, j int) bool {
		return screenTris[i].avgZ > screenTris[j].avgZ
	})

	// Rasterize.
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	// Black background.
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{0, 0, 0, 255})
		}
	}
	for _, tri := range screenTris {
		fillTriangle(img, tri.sx, tri.sy, tri.col)
	}

	// Write output.
	var out *os.File
	if outPath := c.String("o"); outPath == "" {
		out = os.Stdout
	} else {
		out, err = os.Create(outPath)
		if err != nil {
			return fmt.Errorf("create %s: %w", outPath, err)
		}
		defer out.Close()
	}
	return png.Encode(out, img)
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

	// scanline fill between two edges for a Y range.
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

	// interpolate X along an edge from (ax,ay) to (bx,by) at scan line y.
	edgeX := func(ax, ay, bx, by, y int) int {
		if ay == by {
			return ax
		}
		return ax + (bx-ax)*(y-ay)/(by-ay)
	}

	y0, y1, y2 := v[0][1], v[1][1], v[2][1]
	x0, x1, x2 := v[0][0], v[1][0], v[2][0]

	// Top half: v[0] → v[1] and v[0] → v[2].
	for y := y0; y <= y1; y++ {
		xa := edgeX(x0, y0, x1, y1, y)
		xb := edgeX(x0, y0, x2, y2, y)
		fillSpan(y, xa, xb)
	}
	// Bottom half: v[1] → v[2] and v[0] → v[2].
	for y := y1 + 1; y <= y2; y++ {
		xa := edgeX(x1, y1, x2, y2, y)
		xb := edgeX(x0, y0, x2, y2, y)
		fillSpan(y, xa, xb)
	}
}

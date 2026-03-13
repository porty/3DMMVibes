package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// actorColors is a fixed palette of 8 distinct semi-transparent NRGBA colors
// used to mark actor positions. Index by arid % 8.
var actorColors = [8]color.NRGBA{
	{R: 255, G: 0, B: 0, A: 220},
	{R: 0, G: 220, B: 0, A: 220},
	{R: 0, G: 100, B: 255, A: 220},
	{R: 255, G: 220, B: 0, A: 220},
	{R: 255, G: 0, B: 200, A: 220},
	{R: 0, G: 230, B: 220, A: 220},
	{R: 255, G: 140, B: 0, A: 220},
	{R: 160, G: 0, B: 255, A: 220},
}

// CamParams holds the camera parameters needed for 3D→2D projection.
type CamParams struct {
	M      [4][3]float64 // BMAT34 rows (world→view transform, row-vector convention)
	FOVRad float64       // horizontal field of view in radians
	W, H   int           // output image pixel dimensions
}

// projectWorldPoint projects a world-space point to screen pixel coordinates
// using BRender's row-vector, negative-Z-forward convention.
// Returns (sx, sy, inView). inView is false when the point is behind the camera.
func projectWorldPoint(cam CamParams, wx, wy, wz float64) (sx, sy int, inView bool) {
	M := cam.M
	vx := wx*M[0][0] + wy*M[1][0] + wz*M[2][0] + M[3][0]
	vy := wx*M[0][1] + wy*M[1][1] + wz*M[2][1] + M[3][1]
	vz := wx*M[0][2] + wy*M[1][2] + wz*M[2][2] + M[3][2]
	if vz >= 0 {
		return 0, 0, false // behind camera
	}
	halfW := float64(cam.W) / 2.0
	halfH := float64(cam.H) / 2.0
	focal := halfW / math.Tan(cam.FOVRad/2.0)
	sx = int(math.Round(vx/(-vz)*focal + halfW))
	sy = int(math.Round(-vy/(-vz)*focal + halfH)) // Y-flip: screen Y increases downward
	inView = sx >= 0 && sx < cam.W && sy >= 0 && sy < cam.H
	return
}

// drawCircle draws a filled circle of the given radius centred at (cx, cy).
func drawCircle(img *image.NRGBA, cx, cy, radius int, c color.NRGBA) {
	b := img.Bounds()
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			if dx*dx+dy*dy <= radius*radius {
				px, py := cx+dx, cy+dy
				if px >= b.Min.X && px < b.Max.X && py >= b.Min.Y && py < b.Max.Y {
					img.SetNRGBA(px, py, c)
				}
			}
		}
	}
}

// mbmpToNRGBA converts a palette-indexed MBMPImage to true-colour NRGBA.
// The output rect always starts at (0, 0) regardless of the MBMP's origin.
func mbmpToNRGBA(img *MBMPImage, pal Palette) *image.NRGBA {
	bounds := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			mc := img.At(x, y).(MBMPColor)
			pc := pal.Colors[mc.Index]
			r, g, b, _ := pc.RGBA()
			out.SetNRGBA(x-bounds.Min.X, y-bounds.Min.Y,
				color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: mc.A})
		}
	}
	return out
}

// blankFrame returns a solid mid-gray 640×480 NRGBA image used as a fallback
// when no background is available.
func blankFrame() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 640, 480))
	gray := color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	for y := range 480 {
		for x := range 640 {
			img.SetNRGBA(x, y, gray)
		}
	}
	return img
}

// defaultCamParams returns a 60° perspective camera for a 640×480 frame with
// the camera at the origin looking down −Z (identity view matrix).
func defaultCamParams(w, h int) CamParams {
	return CamParams{
		M:      [4][3]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}, {0, 0, 0}},
		FOVRad: 60.0 * math.Pi / 180.0,
		W:      w,
		H:      h,
	}
}

// LoadCamParams parses the CAM chunk header for camera index icam inside
// bkgdChunk and returns the projection parameters.
//
// CAM struct layout (76 bytes, from inc/bkgd.h):
//
//	[0:2]   int16 bo
//	[2:4]   int16 osk
//	[4:8]   BRS   zrHither
//	[8:12]  BRS   zrYon
//	[12:14] BRA   aFov  (uint16: 0..65535 = 0..2π → rad = v×π/32768)
//	[14:16] int16 swPad
//	[16:28] APOS  (3 × BRS)
//	[28:76] BMAT34 bmat34Cam (4×3 int32 BRS, row-major)
func LoadCamParams(bkgdCF *ChunkyFile, bkgdR io.ReaderAt, bkgdChunk Chunk, icam, w, h int) (*CamParams, error) {
	camChunk, ok := bkgdCF.FindChildByChidCTG(bkgdChunk, uint32(icam), ctgCAM)
	if !ok {
		return nil, fmt.Errorf("cam: CAM chunk with CHID=%d not found in BKGD 0x%08X", icam, bkgdChunk.CNO)
	}
	data, err := ChunkData(bkgdR, camChunk)
	if err != nil {
		return nil, fmt.Errorf("cam: reading CAM chunk: %w", err)
	}
	if len(data) < 76 {
		return nil, fmt.Errorf("cam: CAM data too short (%d bytes, need 76)", len(data))
	}
	bo := int16(binary.LittleEndian.Uint16(data[0:2]))
	if bo != kboCur {
		return nil, fmt.Errorf("cam: unsupported byte order 0x%04X", uint16(bo))
	}

	aFov := binary.LittleEndian.Uint16(data[12:14])
	fovRad := float64(aFov) * math.Pi / 32768.0
	if fovRad <= 0 || fovRad >= math.Pi {
		fovRad = 60.0 * math.Pi / 180.0
	}

	var m [4][3]float64
	for row, off := 0, 28; row < 4; row++ {
		for col := range 3 {
			v := int32(binary.LittleEndian.Uint32(data[off : off+4]))
			m[row][col] = brsToFloat64(v)
			off += 4
		}
	}
	return &CamParams{M: m, FOVRad: fovRad, W: w, H: h}, nil
}

// bkgdCache caches loaded BackgroundScenes so repeated BKGD loads are avoided.
type bkgdCache struct {
	cf    *ChunkyFile
	file  *os.File
	scene *BackgroundScene
}

// FindBKGDInDir scans dir for chunky files (.3cn, .3th, .chk) that contain a
// BKGD chunk with the given CNO. Returns the parsed ChunkyFile, an open
// *os.File (caller must close), and the BKGD Chunk on success.
func FindBKGDInDir(dir string, cno uint32) (*ChunkyFile, *os.File, Chunk, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, Chunk{}, fmt.Errorf("FindBKGDInDir: reading %q: %w", dir, err)
	}
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(de.Name())) {
		case ".3cn", ".3th", ".chk", ".3mm":
		default:
			continue
		}
		path := filepath.Join(dir, de.Name())
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		cf, err := ParseChunkyFile(f)
		if err != nil {
			f.Close()
			continue
		}
		if chunk, ok := cf.FindChunk(ctgBKGD, cno); ok {
			return cf, f, chunk, nil
		}
		f.Close()
	}
	return nil, nil, Chunk{}, fmt.Errorf("FindBKGDInDir: no BKGD/0x%08X in %q", cno, dir)
}

// renderMain is the entry point for the `render` subcommand.
func renderMain(args []string) {
	fs := flag.NewFlagSet("render", flag.ExitOnError)
	outDir := fs.String("outdir", "frames", "Output directory for PNG frames")
	sceneN := fs.Int("scene", -1, "Render only scene N (0-based); -1 = all scenes")
	bkgdDir := fs.String("bkgddir", "", "Directory containing background content files (.3cn/.3th/.chk)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: 3dmm-go render [-outdir DIR] [-scene N] [-bkgddir DIR] <movie.3mm>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Render each frame of a .3MM movie as a PNG image.")
		fmt.Fprintln(os.Stderr, "Backgrounds use the correct camera angle per frame.")
		fmt.Fprintln(os.Stderr, "Actors are shown as colored circles (full 3D rendering is future work).")
		fmt.Fprintln(os.Stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}
	if fs.NArg() < 1 {
		fs.Usage()
		os.Exit(1)
	}

	f, err := os.Open(fs.Arg(0))
	if err != nil {
		fatalf("open %s: %v", fs.Arg(0), err)
	}
	defer f.Close()

	cf, err := ParseChunkyFile(f)
	if err != nil {
		fatalf("parsing %s: %v", fs.Arg(0), err)
	}

	if err := RenderMovie(*outDir, *sceneN, *bkgdDir, cf, f); err != nil {
		fatalf("%v", err)
	}
}

// RenderMovie renders all (or one) scene from cf to outDir.
func RenderMovie(outDir string, sceneFilter int, bkgdDir string, cf *ChunkyFile, r io.ReaderAt) error {
	movie, err := LoadMovie(cf, r)
	if err != nil {
		return err
	}
	if len(movie.Scenes) == 0 {
		return fmt.Errorf("movie has no scenes")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	// Cache for BKGD content files opened during rendering.
	cache := map[uint32]*bkgdCache{}
	defer func() {
		for _, bc := range cache {
			bc.file.Close()
		}
	}()

	openBKGD := func(tag ChunkTAG) *bkgdCache {
		if bkgdDir == "" {
			return nil
		}
		if bc, ok := cache[tag.CNO]; ok {
			return bc
		}
		bkgdCF, bkgdFile, bkgdChunk, err := FindBKGDInDir(bkgdDir, tag.CNO)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: loading BKGD 0x%08X: %v\n", tag.CNO, err)
			return nil
		}
		basePal, _, _ := FindGLCR(bkgdCF, bkgdFile)
		scene, err := LoadBackgroundScene(bkgdFile, bkgdCF, bkgdChunk.CTG, bkgdChunk.CNO, basePal)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: decoding BKGD 0x%08X: %v\n", tag.CNO, err)
			bkgdFile.Close()
			return nil
		}
		bc := &bkgdCache{cf: bkgdCF, file: bkgdFile, scene: scene}
		cache[tag.CNO] = bc
		return bc
	}

	for i, sr := range movie.Scenes {
		if sceneFilter >= 0 && i != sceneFilter {
			continue
		}
		scenChunk, ok := cf.FindChunk(ctgSCEN, sr.CNO)
		if !ok {
			fmt.Fprintf(os.Stderr, "warning: scene %d (CNO 0x%08X) chunk not found, skipping\n", i, sr.CNO)
			continue
		}
		sd, err := ParseScene(cf, r, scenChunk)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: scene %d: %v, skipping\n", i, err)
			continue
		}
		if err := renderScene(outDir, i, sd, cf, r, openBKGD); err != nil {
			return fmt.Errorf("scene %d: %w", i, err)
		}
	}
	return nil
}

// renderScene renders all frames of one scene.
func renderScene(
	outDir string,
	sceneIdx int,
	sd *SceneData,
	cf *ChunkyFile,
	r io.ReaderAt,
	openBKGD func(ChunkTAG) *bkgdCache,
) error {
	currentCam := 0
	var currentBkgd *bkgdCache
	var currentBkgdTag ChunkTAG

	applyEvent := func(ev SceneEvent) {
		switch ev.Sevt {
		case sevtSetBkgd:
			if tag, err := ParseSEVBkgdTag(ev.VarData); err == nil {
				currentBkgdTag = tag
				currentBkgd = openBKGD(tag)
			}
		case sevtChngCamera:
			if icam, err := ParseSEVCamera(ev.VarData); err == nil {
				currentCam = int(icam)
			}
		}
	}

	// Fire GGST (start) events once.
	for _, ev := range sd.StartEvents {
		applyEvent(ev)
	}

	// Load all actors for this scene.
	var actors []*Actor
	for _, cno := range sd.ActorCNOs {
		a, err := LoadActor(cf, r, cno)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: scene %d: %v\n", sceneIdx, err)
			continue
		}
		actors = append(actors, a)
	}

	nfrmFirst, nfrmLast := sd.NfrmFirst, sd.NfrmLast
	if nfrmFirst > nfrmLast {
		fmt.Fprintf(os.Stderr, "warning: scene %d: empty frame range [%d, %d]\n", sceneIdx, nfrmFirst, nfrmLast)
		return nil
	}

	fmt.Printf("scene %d: frames %d..%d  actors: %d\n", sceneIdx, nfrmFirst, nfrmLast, len(actors))

	fevIdx := 0
	for nfrm := nfrmFirst; nfrm <= nfrmLast; nfrm++ {
		// Advance GGFR frame events up to this frame.
		for fevIdx < len(sd.FrameEvents) && sd.FrameEvents[fevIdx].Nfrm <= nfrm {
			applyEvent(sd.FrameEvents[fevIdx])
			fevIdx++
		}

		// Build background image and camera params.
		var frame *image.NRGBA
		var cam CamParams

		if currentBkgd != nil && currentCam < len(currentBkgd.scene.Angles) {
			angle := currentBkgd.scene.Angles[currentCam]
			frame = mbmpToNRGBA(angle.Img, currentBkgd.scene.Palette)
			w, h := frame.Bounds().Dx(), frame.Bounds().Dy()

			// Find the BKGD chunk by CNO in the content file.
			if bkgdChunk, ok := currentBkgd.cf.FindChunk(ctgBKGD, currentBkgdTag.CNO); ok {
				cp, err := LoadCamParams(currentBkgd.cf, currentBkgd.file, bkgdChunk, currentCam, w, h)
				if err == nil {
					cam = *cp
				} else {
					cam = defaultCamParams(w, h)
				}
			} else {
				cam = defaultCamParams(w, h)
			}
		} else {
			frame = blankFrame()
			cam = defaultCamParams(640, 480)
			if currentBkgd != nil {
				fmt.Fprintf(os.Stderr, "warning: scene %d frame %d: camera %d out of range (%d angles)\n",
					sceneIdx, nfrm, currentCam, len(currentBkgd.scene.Angles))
			}
		}

		// Project and draw actor position markers.
		for _, a := range actors {
			wx, wy, wz, onStage := ActorWorldPos(a.Def, a.Path, a.Events, nfrm)
			if !onStage {
				continue
			}
			sx, sy, inView := projectWorldPoint(cam, wx, wy, wz)
			if !inView {
				continue
			}
			drawCircle(frame, sx, sy, 8, actorColors[a.Def.ARID%8])
		}

		name := fmt.Sprintf("frame_%04d_%04d.png", sceneIdx, nfrm)
		if err := writePNG(filepath.Join(outDir, name), frame); err != nil {
			return err
		}
	}
	return nil
}

// writePNG encodes img as a PNG file at path.
func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	return nil
}

package mm

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/urfave/cli/v2"
)

const (
	DefaultWidth  = 544
	DefaultHeight = 306
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
	M [4][3]float64 // BMAT34 rows — camera-to-world model matrix (row-vector convention).
	// Row 3 is the camera position in world space; rows 0–2 are the camera's
	// local X/Y/Z axes expressed in world space.  To project a world point,
	// invert this matrix: subtract M[3] then dot-product with each row.
	FOVRad float64 // horizontal field of view in radians
	W, H   int     // output image pixel dimensions
}

// projectWorldPoint projects a world-space point to screen pixel coordinates
// using BRender's row-vector, negative-Z-forward convention.
// Returns (sx, sy, inView). inView is false when the point is behind the camera.
//
// cam.M is the camera's model matrix (camera-to-world). To go world-to-camera
// we invert it: for an orthonormal matrix this is transpose(rotation) applied
// to (world − translation). In row-vector terms:
//
//	v_cam = (w − M[3]) · M_rot  where M_rot is the 3×3 upper block of M.
func projectWorldPoint(cam CamParams, wx, wy, wz float64) (sx, sy int, inView bool) {
	M := cam.M
	dx := wx - M[3][0]
	dy := wy - M[3][1]
	dz := wz - M[3][2]
	vx := dx*M[0][0] + dy*M[0][1] + dz*M[0][2]
	vy := dx*M[1][0] + dy*M[1][1] + dz*M[1][2]
	vz := dx*M[2][0] + dy*M[2][1] + dz*M[2][2]
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

// audioCue records a single sound event for overlay display purposes.
type audioCue struct {
	label     string
	color     color.NRGBA
	startNfrm int32
}

// collectAudioCues scans all actor events and returns one audioCue per aetSnd
// event that is not silenced (FNoSound == 0). Cues are returned in event order.
func collectAudioCues(actors []*Actor) []audioCue {
	var cues []audioCue
	for _, a := range actors {
		c := actorColors[a.Def.ARID%8]
		for _, ev := range a.Events {
			if ev.AET != aetSnd {
				continue
			}
			snd, err := ParseAEVSND(ev.VarData)
			if err != nil || snd.FNoSound != 0 {
				continue
			}
			label := fmt.Sprintf("Actor %d: %s", a.Def.ARID, StyLabel(snd.Sty))
			cues = append(cues, audioCue{label: label, color: c, startNfrm: ev.Nfrm})
		}
	}
	return cues
}

// activeAudioCues returns cues whose display window covers nfrm.
// Each cue is shown for 3 frames starting at its startNfrm.
func activeAudioCues(cues []audioCue, nfrm int32) []audioCue {
	var active []audioCue
	for _, c := range cues {
		if nfrm >= c.startNfrm && nfrm < c.startNfrm+3 {
			active = append(active, c)
		}
	}
	return active
}

// drawText renders text onto img at pixel position (x, y) using the basicfont
// 7×13 bitmap font. y is the baseline coordinate.
func drawText(img *image.NRGBA, x, y int, text string, c color.NRGBA) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(c),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(text)
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

// blankFrame returns a solid mid-gray 544x306 NRGBA image used as a fallback
// when no background is available.
func blankFrame() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, DefaultWidth, DefaultHeight))
	gray := color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	for y := range DefaultHeight {
		for x := range DefaultWidth {
			img.SetNRGBA(x, y, gray)
		}
	}
	return img
}

// defaultCamParams returns a 60° perspective camera for a 544x306 frame with
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
	camChunk, ok := bkgdCF.FindChildByChidCTG(bkgdChunk, uint32(icam), TagCAM)
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

// tmplLoader indexes TMPL chunks found in an assets directory and caches
// loaded templates. It also holds the global colour palette (GLCR) used for
// actor texture rendering. A nil *tmplLoader is safe to use: all lookups
// return (nil, false).
type tmplLoader struct {
	// sources maps TMPL CNO → the chunky file containing it.
	sources map[uint32]tmplSource
	// loaded caches the result of LoadTemplate per CNO (nil value = load failed).
	loaded map[uint32]*LoadedTemplate
	// openFiles owns all *os.File handles opened for TMPL content.
	openFiles []*os.File
	// pal is the first GLCR palette found in the assets directory.
	pal Palette
}

type tmplSource struct {
	cf *ChunkyFile
	f  *os.File
}

// close releases all file handles held by the loader.
func (tl *tmplLoader) close() {
	if tl == nil {
		return
	}
	for _, f := range tl.openFiles {
		f.Close()
	}
}

// get returns the LoadedTemplate for cno, loading it on first access.
// Returns (nil, false) when the template is not found or fails to load.
func (tl *tmplLoader) get(cno uint32) (*LoadedTemplate, bool) {
	if tl == nil {
		return nil, false
	}
	if tmpl, ok := tl.loaded[cno]; ok {
		return tmpl, tmpl != nil
	}
	src, ok := tl.sources[cno]
	if !ok {
		tl.loaded[cno] = nil
		return nil, false
	}
	tmpl, err := LoadTemplate(src.cf, src.f, cno)
	if err != nil {
		tl.loaded[cno] = nil
		return nil, false
	}
	tl.loaded[cno] = tmpl
	return tmpl, true
}

// openAssetsLoader scans assetsDir for chunky files, indexes all TMPL chunks
// by CNO, and loads the first GLCR palette it finds. Returns nil (not an error)
// when assetsDir is empty — callers treat nil as "no templates available".
func openAssetsLoader(assetsDir string, logger *log.Logger) *tmplLoader {
	if assetsDir == "" {
		return nil
	}
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		logger.Printf("warning: reading assets dir %q: %v", assetsDir, err)
		return nil
	}
	tl := &tmplLoader{
		sources: make(map[uint32]tmplSource),
		loaded:  make(map[uint32]*LoadedTemplate),
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
		path := filepath.Join(assetsDir, de.Name())
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		cf, err := ParseChunkyFile(f)
		if err != nil {
			f.Close()
			continue
		}
		hasTMPL := false
		for _, chunk := range cf.Chunks {
			if chunk.CTG == TagTMPL {
				hasTMPL = true
				tl.sources[chunk.CNO] = tmplSource{cf: cf, f: f}
			}
		}
		// Load the global palette from the first file that has a GLCR chunk.
		if len(tl.pal.Colors) == 0 {
			if p, ok, _ := FindGLCR(cf, f); ok {
				tl.pal = p
			}
		}
		if hasTMPL {
			tl.openFiles = append(tl.openFiles, f)
		} else {
			f.Close()
		}
	}
	return tl
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
		if chunk, ok := cf.FindChunk(TagBKGD, cno); ok {
			return cf, f, chunk, nil
		}
		f.Close()
	}
	return nil, nil, Chunk{}, fmt.Errorf("FindBKGDInDir: no BKGD/0x%08X in %q", cno, dir)
}

// renderCommand returns the `render` command with png and rgb24 subcommands.
func renderCommand() *cli.Command {
	commonFlags := []cli.Flag{
		&cli.IntFlag{Name: "scene", Value: -1, Usage: "Render only scene N (0-based); -1 = all scenes"},
		&cli.StringFlag{Name: "assets", Usage: "Directory containing game content files (.3cn/.3th/.chk) for backgrounds and actor templates"},
	}
	return &cli.Command{
		Name:      "render",
		Usage:     "Render a .3MM movie to image frames",
		ArgsUsage: "<movie.3mm>",
		Subcommands: []*cli.Command{
			{
				Name:      "png",
				Usage:     "Render frames as PNG image files",
				ArgsUsage: "<movie.3mm>",
				Description: "Render each frame of a .3MM movie as a PNG image.\n" +
					"Backgrounds use the correct camera angle per frame.\n" +
					"Actors are rendered in 3D when --assets points to the game content directory.",
				Flags: append(commonFlags,
					&cli.StringFlag{Name: "outdir", Value: "frames", Usage: "Output directory for PNG frames"},
				),
				Action: renderPNGAction,
			},
			{
				Name:      "rgb24",
				Usage:     "Render frames as raw 24-bit RGB (pipe to ffmpeg)",
				ArgsUsage: "<movie.3mm>",
				Description: fmt.Sprintf("Outputs raw RGB24 video data with no header. Each pixel is 3 bytes (R, G, B),\n"+
					"written row by row, left to right, top to bottom. Frame dimensions match the\n"+
					"background image (typically %[1]dx%[2]d). 3D Movie Maker runs at 12 frames per second.\n\n"+
					"Pass these values to ffmpeg via -video_size, -framerate, and -pixel_format rgb24.\n\n"+
					"Example:\n"+
					"  3dmm render rgb24 --assets ./content movie.3mm \\\n"+
					"    | ffmpeg -f rawvideo -video_size %[1]dx%[2]d -pixel_format rgb24 -framerate 12 -i - output.mp4",
					DefaultWidth, DefaultHeight),
				Flags: append(commonFlags,
					&cli.StringFlag{Name: "output", Value: "-", Usage: `Output file path; "-" writes to stdout`},
				),
				Action: renderRGB24Action,
			},
			{
				Name:      "ffmpeg",
				Usage:     "Render frames and pipe directly to ffmpeg",
				ArgsUsage: "<movie.3mm> <output>",
				Description: fmt.Sprintf("Renders raw RGB24 frames and pipes them to ffmpeg as a subprocess.\n"+
					"--video-size must match the background resolution of the movie (typically %[1]dx%[2]d).\n"+
					"3D Movie Maker movies run at 12 frames per second.\n\n"+
					"Example:\n"+
					"  3dmm render ffmpeg --assets ./content --video-size %[1]dx%[2]d movie.3mm output.mp4",
					DefaultWidth, DefaultHeight),
				Flags: append(commonFlags,
					&cli.StringFlag{Name: "video-size", Value: fmt.Sprintf("%dx%d", DefaultWidth, DefaultHeight), Usage: "Frame dimensions WxH (must match background resolution)"},
					&cli.IntFlag{Name: "framerate", Value: 12, Usage: "Output framerate passed to ffmpeg"},
					&cli.StringFlag{Name: "ffmpeg-bin", Value: "ffmpeg", Usage: "Path to ffmpeg binary"},
				),
				Action: renderFFmpegAction,
			},
		},
	}
}

func renderPNGAction(c *cli.Context) error {
	if c.NArg() < 1 {
		_ = cli.ShowSubcommandHelp(c)
		return cli.Exit("", 1)
	}

	path := c.Args().First()
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	cf, err := ParseChunkyFile(f)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	return RenderMovie(c.String("outdir"), c.Int("scene"), c.String("assets"), cf, f, log.New(os.Stderr, "", 0))
}

func renderRGB24Action(c *cli.Context) error {
	if c.NArg() < 1 {
		_ = cli.ShowSubcommandHelp(c)
		return cli.Exit("", 1)
	}

	path := c.Args().First()
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	cf, err := ParseChunkyFile(f)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	outPath := c.String("output")
	var out *os.File
	if outPath == "-" {
		out = os.Stdout
	} else {
		out, err = os.Create(outPath)
		if err != nil {
			return fmt.Errorf("creating %s: %w", outPath, err)
		}
		defer out.Close()
	}

	return RenderMovieRGB24(out, c.Int("scene"), c.String("assets"), cf, f, log.New(os.Stderr, "", 0))
}

func renderFFmpegAction(c *cli.Context) error {
	if c.NArg() < 2 {
		_ = cli.ShowSubcommandHelp(c)
		return cli.Exit("", 1)
	}

	moviePath := c.Args().Get(0)
	outputPath := c.Args().Get(1)

	f, err := os.Open(moviePath)
	if err != nil {
		return fmt.Errorf("open %s: %w", moviePath, err)
	}
	defer f.Close()

	cf, err := ParseChunkyFile(f)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", moviePath, err)
	}

	ffmpegBin := c.String("ffmpeg-bin")
	cmd := exec.Command(ffmpegBin,
		"-f", "rawvideo",
		"-video_size", c.String("video-size"),
		"-pixel_format", "rgb24",
		"-framerate", fmt.Sprintf("%d", c.Int("framerate")),
		"-i", "pipe:0",
		"-y", // always overwrite: stdin is the pipe so ffmpeg can't prompt interactively
		outputPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("creating ffmpeg stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting ffmpeg: %w", err)
	}

	renderErr := RenderMovieRGB24(stdin, c.Int("scene"), c.String("assets"), cf, f, log.New(os.Stderr, "", 0))
	stdin.Close()
	cmdErr := cmd.Wait()

	if renderErr != nil {
		return renderErr
	}
	return cmdErr
}

// RenderMovie renders all (or one) scene from cf to outDir.
func RenderMovie(outDir string, sceneFilter int, assetsDir string, cf *ChunkyFile, r io.ReaderAt, logger *log.Logger) error {
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

	// Load template content and global palette from the assets directory.
	tl := openAssetsLoader(assetsDir, logger)
	defer tl.close()

	// Cache for BKGD content files opened during rendering.
	cache := map[uint32]*bkgdCache{}
	defer func() {
		for _, bc := range cache {
			bc.file.Close()
		}
	}()

	openBKGD := func(tag ChunkTAG) *bkgdCache {
		if assetsDir == "" {
			return nil
		}
		if bc, ok := cache[tag.CNO]; ok {
			return bc
		}
		bkgdCF, bkgdFile, bkgdChunk, err := FindBKGDInDir(assetsDir, tag.CNO)
		if err != nil {
			logger.Printf("warning: loading BKGD 0x%08X: %v", tag.CNO, err)
			return nil
		}
		basePal, _, _ := FindGLCR(bkgdCF, bkgdFile)
		scene, err := LoadBackgroundScene(bkgdFile, bkgdCF, bkgdChunk.CTG, bkgdChunk.CNO, basePal)
		if err != nil {
			logger.Printf("warning: decoding BKGD 0x%08X: %v", tag.CNO, err)
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
			logger.Printf("warning: scene %d (CNO 0x%08X) chunk not found, skipping", i, sr.CNO)
			continue
		}
		sd, err := ParseScene(cf, r, scenChunk)
		if err != nil {
			logger.Printf("warning: scene %d: %v, skipping", i, err)
			continue
		}
		if err := renderScene(outDir, i, sd, cf, r, openBKGD, tl, logger); err != nil {
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
	tl *tmplLoader,
	logger *log.Logger,
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
			logger.Printf("warning: scene %d: %v", sceneIdx, err)
			continue
		}
		actors = append(actors, a)
	}

	nfrmFirst, nfrmLast := sd.NfrmFirst, sd.NfrmLast
	if nfrmFirst > nfrmLast {
		logger.Printf("warning: scene %d: empty frame range [%d, %d]", sceneIdx, nfrmFirst, nfrmLast)
		return nil
	}

	logger.Printf("scene %d: frames %d..%d  actors: %d", sceneIdx, nfrmFirst, nfrmLast, len(actors))

	audioCues := collectAudioCues(actors)

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
			if bkgdChunk, ok := currentBkgd.cf.FindChunk(TagBKGD, currentBkgdTag.CNO); ok {
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
			// cam = defaultCamParams(640, 480)
			cam = defaultCamParams(DefaultWidth, DefaultHeight)
			if currentBkgd != nil {
				logger.Printf("warning: scene %d frame %d: camera %d out of range (%d angles)",
					sceneIdx, nfrm, currentCam, len(currentBkgd.scene.Angles))
			}
		}

		// Render actors. Warn once per TMPL CNO when geometry is unavailable.
		warnedTMPL := map[uint32]bool{}
		for _, a := range actors {
			state := ActorStateAtFrame(a.Def, a.Path, a.Events, nfrm)
			if !state.OnStage {
				continue
			}
			tmpl, ok := tl.get(a.Def.TagTmplCNO)
			if ok {
				RenderActorOnFrame(frame, cam, tmpl, state, tl.pal)
				continue
			}
			if !warnedTMPL[a.Def.TagTmplCNO] {
				warnedTMPL[a.Def.TagTmplCNO] = true
				logger.Printf("warning: scene %d: TMPL/0x%08X not found in assets, rendering actors as circles", sceneIdx, a.Def.TagTmplCNO)
			}
			sx, sy, inView := projectWorldPoint(cam, state.Pos[0], state.Pos[1], state.Pos[2])
			if inView {
				drawCircle(frame, sx, sy, 8, actorColors[a.Def.ARID%8])
			}
		}

		// Draw audio cue labels for any sounds firing in this frame window.
		lineY := 13
		for _, cue := range activeAudioCues(audioCues, nfrm) {
			drawText(frame, 4, lineY, cue.label, cue.color)
			lineY += 14
		}

		name := fmt.Sprintf("frame_%04d_%04d.png", sceneIdx, nfrm)
		if err := writePNG(filepath.Join(outDir, name), frame); err != nil {
			return err
		}
	}
	return nil
}

// RenderMovieRGB24 renders all (or one) scene from cf and writes raw RGB24
// frames to w. Each frame is width×height×3 bytes (R, G, B per pixel, row-major).
func RenderMovieRGB24(w io.Writer, sceneFilter int, assetsDir string, cf *ChunkyFile, r io.ReaderAt, logger *log.Logger) error {
	movie, err := LoadMovie(cf, r)
	if err != nil {
		return err
	}
	if len(movie.Scenes) == 0 {
		return fmt.Errorf("movie has no scenes")
	}

	tl := openAssetsLoader(assetsDir, logger)
	defer tl.close()

	cache := map[uint32]*bkgdCache{}
	defer func() {
		for _, bc := range cache {
			bc.file.Close()
		}
	}()

	openBKGD := func(tag ChunkTAG) *bkgdCache {
		if assetsDir == "" {
			return nil
		}
		if bc, ok := cache[tag.CNO]; ok {
			return bc
		}
		bkgdCF, bkgdFile, bkgdChunk, err := FindBKGDInDir(assetsDir, tag.CNO)
		if err != nil {
			logger.Printf("warning: loading BKGD 0x%08X: %v", tag.CNO, err)
			return nil
		}
		basePal, _, _ := FindGLCR(bkgdCF, bkgdFile)
		scene, err := LoadBackgroundScene(bkgdFile, bkgdCF, bkgdChunk.CTG, bkgdChunk.CNO, basePal)
		if err != nil {
			logger.Printf("warning: decoding BKGD 0x%08X: %v", tag.CNO, err)
			bkgdFile.Close()
			return nil
		}
		bc := &bkgdCache{cf: bkgdCF, file: bkgdFile, scene: scene}
		cache[tag.CNO] = bc
		return bc
	}

	bw := bufio.NewWriterSize(w, 1<<20)
	for i, sr := range movie.Scenes {
		if sceneFilter >= 0 && i != sceneFilter {
			continue
		}
		scenChunk, ok := cf.FindChunk(ctgSCEN, sr.CNO)
		if !ok {
			logger.Printf("warning: scene %d (CNO 0x%08X) chunk not found, skipping", i, sr.CNO)
			continue
		}
		sd, err := ParseScene(cf, r, scenChunk)
		if err != nil {
			logger.Printf("warning: scene %d: %v, skipping", i, err)
			continue
		}
		if err := renderSceneRGB24(bw, i, sd, cf, r, openBKGD, tl, logger); err != nil {
			return fmt.Errorf("scene %d: %w", i, err)
		}
	}
	return bw.Flush()
}

// renderSceneRGB24 writes raw RGB24 bytes for every frame of one scene.
func renderSceneRGB24(
	w *bufio.Writer,
	sceneIdx int,
	sd *SceneData,
	cf *ChunkyFile,
	r io.ReaderAt,
	openBKGD func(ChunkTAG) *bkgdCache,
	tl *tmplLoader,
	logger *log.Logger,
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

	for _, ev := range sd.StartEvents {
		applyEvent(ev)
	}

	var actors []*Actor
	for _, cno := range sd.ActorCNOs {
		a, err := LoadActor(cf, r, cno)
		if err != nil {
			logger.Printf("warning: scene %d: %v", sceneIdx, err)
			continue
		}
		actors = append(actors, a)
	}

	nfrmFirst, nfrmLast := sd.NfrmFirst, sd.NfrmLast
	if nfrmFirst > nfrmLast {
		return nil
	}

	logger.Printf("scene %d: frames %d..%d  actors: %d", sceneIdx, nfrmFirst, nfrmLast, len(actors))

	audioCues := collectAudioCues(actors)

	fevIdx := 0
	for nfrm := nfrmFirst; nfrm <= nfrmLast; nfrm++ {
		for fevIdx < len(sd.FrameEvents) && sd.FrameEvents[fevIdx].Nfrm <= nfrm {
			applyEvent(sd.FrameEvents[fevIdx])
			fevIdx++
		}

		var frame *image.NRGBA
		var cam CamParams

		if currentBkgd != nil && currentCam < len(currentBkgd.scene.Angles) {
			angle := currentBkgd.scene.Angles[currentCam]
			frame = mbmpToNRGBA(angle.Img, currentBkgd.scene.Palette)
			fw, fh := frame.Bounds().Dx(), frame.Bounds().Dy()
			if bkgdChunk, ok := currentBkgd.cf.FindChunk(TagBKGD, currentBkgdTag.CNO); ok {
				cp, err := LoadCamParams(currentBkgd.cf, currentBkgd.file, bkgdChunk, currentCam, fw, fh)
				if err == nil {
					cam = *cp
				} else {
					cam = defaultCamParams(fw, fh)
				}
			} else {
				cam = defaultCamParams(fw, fh)
			}
		} else {
			frame = blankFrame()
			cam = defaultCamParams(DefaultWidth, DefaultHeight)
			if currentBkgd != nil {
				logger.Printf("warning: scene %d frame %d: camera %d out of range (%d angles)",
					sceneIdx, nfrm, currentCam, len(currentBkgd.scene.Angles))
			}
		}

		warnedTMPL := map[uint32]bool{}
		for _, a := range actors {
			state := ActorStateAtFrame(a.Def, a.Path, a.Events, nfrm)
			if !state.OnStage {
				continue
			}
			tmpl, ok := tl.get(a.Def.TagTmplCNO)
			if ok {
				RenderActorOnFrame(frame, cam, tmpl, state, tl.pal)
				continue
			}
			if !warnedTMPL[a.Def.TagTmplCNO] {
				warnedTMPL[a.Def.TagTmplCNO] = true
				logger.Printf("warning: scene %d: TMPL/0x%08X not found in assets, rendering actors as circles", sceneIdx, a.Def.TagTmplCNO)
			}
			sx, sy, inView := projectWorldPoint(cam, state.Pos[0], state.Pos[1], state.Pos[2])
			if inView {
				drawCircle(frame, sx, sy, 8, actorColors[a.Def.ARID%8])
			}
		}

		// Draw audio cue labels for any sounds firing in this frame window.
		lineY := 13
		for _, cue := range activeAudioCues(audioCues, nfrm) {
			drawText(frame, 4, lineY, cue.label, cue.color)
			lineY += 14
		}

		b := frame.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c := frame.NRGBAAt(x, y)
				if _, err := w.Write([]byte{c.R, c.G, c.B}); err != nil {
					return err
				}
			}
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

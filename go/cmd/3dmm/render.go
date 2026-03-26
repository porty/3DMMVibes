package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	mm "github.com/porty/3dmm-go"
	"github.com/urfave/cli/v2"
)

// renderCommand returns the `render` command with png and rgb24 subcommands.
func renderCommand() *cli.Command {
	commonFlags := []cli.Flag{
		&cli.IntFlag{Name: "scene", Value: -1, Usage: "Render only scene N (0-based); -1 = all scenes"},
		&cli.StringFlag{Name: "bkgddir", Usage: "Directory containing background content files (.3cn/.3th/.chk)"},
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
					"Actors are shown as colored circles (full 3D rendering is future work).",
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
					"  3dmm render rgb24 --bkgddir ./content movie.3mm \\\n"+
					"    | ffmpeg -f rawvideo -video_size %[1]dx%[2]d -pixel_format rgb24 -framerate 12 -i - output.mp4",
					mm.DefaultWidth, mm.DefaultHeight),
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
					"  3dmm render ffmpeg --bkgddir ./content --video-size %[1]dx%[2]d movie.3mm output.mp4",
					mm.DefaultWidth, mm.DefaultHeight),
				Flags: append(commonFlags,
					&cli.StringFlag{Name: "video-size", Value: fmt.Sprintf("%dx%d", mm.DefaultWidth, mm.DefaultHeight), Usage: "Frame dimensions WxH (must match background resolution)"},
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

	cf, err := mm.ParseChunkyFile(f)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	return mm.RenderMovie(c.String("outdir"), c.Int("scene"), c.String("bkgddir"), cf, f, log.New(os.Stderr, "", 0))
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

	cf, err := mm.ParseChunkyFile(f)
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

	return mm.RenderMovieRGB24(out, c.Int("scene"), c.String("bkgddir"), cf, f, log.New(os.Stderr, "", 0))
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

	cf, err := mm.ParseChunkyFile(f)
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

	renderErr := mm.RenderMovieRGB24(stdin, c.Int("scene"), c.String("bkgddir"), cf, f, log.New(os.Stderr, "", 0))
	stdin.Close()
	cmdErr := cmd.Wait()

	if renderErr != nil {
		return renderErr
	}
	return cmdErr
}

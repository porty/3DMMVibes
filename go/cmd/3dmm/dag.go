package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	mm "github.com/porty/3dmm-go"
	"github.com/urfave/cli/v2"
)

// dagCommand returns the `dag` command.
func dagCommand() *cli.Command {
	return &cli.Command{
		Name:      "dag",
		Usage:     "Write a Graphviz DOT file of the chunk parent→child graph",
		ArgsUsage: "<file.chk>",
		Description: "Write a Graphviz DOT digraph of chunk parent→child relationships.\n" +
			"Render with: dot -Tpng -o graph.png output.dot",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "o", Usage: "Output DOT file (default: stdout)"},
			&cli.StringFlag{Name: "ctg", Usage: `Filter by chunk type (4 chars, e.g. "MVIE")`},
			&cli.StringFlag{Name: "cno", Usage: `Filter by chunk number (hex e.g. 0x2010)`},
			&cli.StringFlag{Name: "name", Aliases: []string{"n"}, Usage: `Filter by chunk name (e.g. "Keesha"); alternative to --cno`},
		},
		Action: dagAction,
	}
}

func dagAction(c *cli.Context) error {
	if c.NArg() < 1 {
		_ = cli.ShowSubcommandHelp(c)
		return cli.Exit("", 1)
	}

	f, err := os.Open(c.Args().First())
	if err != nil {
		return fmt.Errorf("open %s: %w", c.Args().First(), err)
	}
	defer f.Close()

	cf, err := mm.ParseChunkyFile(f)
	if err != nil {
		return err
	}

	if c.String("cno") != "" && c.String("name") != "" {
		return cli.Exit("--cno and --name are mutually exclusive", 1)
	}
	cf.Chunks = applyDagFilters(cf, c.String("ctg"), c.String("cno"), c.String("name"))

	var w io.Writer
	if outFile := c.String("o"); outFile == "" {
		w = os.Stdout
	} else {
		df, err := os.Create(outFile)
		if err != nil {
			return fmt.Errorf("create %s: %w", outFile, err)
		}
		defer df.Close()
		w = df
	}

	mm.ChunkDAG(cf, w)
	return nil
}

// applyDagFilters narrows the chunk list by CTG, CNO, and/or name.
// cnoStr must be a hex number with 0x prefix (e.g. "0x2010").
// nameStr is matched case-insensitively against chunk names.
func applyDagFilters(cf *mm.ChunkyFile, ctgStr, cnoStr, nameStr string) []mm.Chunk {
	if ctgStr == "" && cnoStr == "" && nameStr == "" {
		return cf.Chunks
	}

	cnoVal := int64(-1)
	if cnoStr != "" {
		var parsed uint32
		if _, err := fmt.Sscanf(cnoStr, "0x%x", &parsed); err == nil {
			cnoVal = int64(parsed)
		} else if _, err := fmt.Sscanf(cnoStr, "%d", &parsed); err == nil {
			cnoVal = int64(parsed)
		}
	}
	nameLower := strings.ToLower(nameStr)

	var out []mm.Chunk
	for _, c := range cf.Chunks {
		if ctgStr != "" && c.CTG != parseCTGString(ctgStr) {
			continue
		}
		if cnoVal >= 0 && c.CNO != uint32(cnoVal) {
			continue
		}
		if nameLower != "" && strings.ToLower(c.Name) != nameLower {
			continue
		}
		out = append(out, c)
	}
	return out
}

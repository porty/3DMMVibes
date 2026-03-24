package main

import (
	"fmt"
	"io"
	"os"

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
			&cli.IntFlag{Name: "cno", Value: -1, Usage: "Filter by chunk number (-1 = all chunks)"},
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

	if ctg := c.String("ctg"); ctg != "" || c.Int("cno") >= 0 {
		cf.Chunks = applyFilters(cf.Chunks, ctg, c.Int("cno"))
	}

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

package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "3dmm",
		Usage: "Tools for working with 3D Movie Maker files",
		Commands: []*cli.Command{
			chunkyCommand(),
			mbmpCommand(),
			bkgdCommand(),
			dagCommand(),
			renderCommand(),
			actorCommand(),
		},
	}
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

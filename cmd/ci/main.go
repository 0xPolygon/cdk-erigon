package main

import (
	"fmt"
	"github.com/erigontech/erigon/cmd/ci/selfdestruct"
	"github.com/erigontech/erigon/params"
	"github.com/urfave/cli/v2"
	"os"
)

func main() {
	app := cli.NewApp()
	app.Name = "ci"
	app.Version = params.VersionWithCommit(params.GitCommit)
	app.UsageText = app.Name + ` [command] [flags]`
	app.Flags = []cli.Flag{}

	app.Commands = []*cli.Command{
		&selfdestruct.Command,
	}

	if err := app.Run(os.Args); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

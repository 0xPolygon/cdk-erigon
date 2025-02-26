package main

import (
	"fmt"
	"github.com/ledgerwatch/erigon/params"
	cli2 "github.com/ledgerwatch/erigon/turbo/cli"
	"github.com/ledgerwatch/erigon/turbo/debug"
	"github.com/ledgerwatch/erigon/zk/replayer"
	"github.com/ledgerwatch/log/v3"
	"github.com/urfave/cli/v2"
	"net"
	"os"
	"strings"
)

var (
	remoteUrlFlag = &cli.StringFlag{
		Name:        "remote-url",
		Usage:       "Url of the remote datastream server",
		Destination: &remoteUrl,
	}
	rpcUrlFlag = &cli.StringFlag{
		Name:        "rpc-url",
		Usage:       "Url of the RPC server",
		Destination: &rpcUrl,
	}
	remoteUrl string
	rpcUrl    string
)

func main() {
	app := cli2.NewApp(params.GitCommit, "Datastream Replayer")
	app.Name = "ds-replayer"
	app.UsageText = app.Name + ` [command] [flags]`
	app.Flags = []cli.Flag{
		remoteUrlFlag,
		rpcUrlFlag,
	}
	app.Before = preStartReplayer
	app.After = finishReplayer
	app.Action = runReplayer
	app.Commands = []*cli.Command{}

	if err := app.Run(os.Args); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func preStartReplayer(cliCtx *cli.Context) error {
	logLvl := log.LvlInfo
	logger := log.Root()
	consoleHandler := log.LvlFilterHandler(logLvl, log.StreamHandler(os.Stdout, log.TerminalFormat()))
	logger.SetHandler(consoleHandler)

	dsUrl := cliCtx.String(remoteUrlFlag.Name)
	if strings.Count(dsUrl, ":") == 0 {
		return fmt.Errorf("invalid address for flag %s: %s", remoteUrlFlag.Name, dsUrl)
	}

	_, _, err := net.SplitHostPort(dsUrl)
	if err != nil {
		return fmt.Errorf("invalid address for flag %s: %s", remoteUrlFlag.Name, dsUrl)
	}

	log.Info("Starting Datastream Replayer")
	return nil
}

func finishReplayer(cliCtx *cli.Context) error {
	log.Info("Exiting Datastream Replayer")
	debug.Exit()
	return nil
}

func runReplayer(cliCtx *cli.Context) error {
	log.Info("Running Datastream Replayer")
	ds := cliCtx.String(remoteUrlFlag.Name)
	rpc := cliCtx.String(rpcUrlFlag.Name)
	return replayer.New(ds, rpc).Run(cliCtx.Context)
}

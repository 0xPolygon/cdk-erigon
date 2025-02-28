package main

import (
	"fmt"
	"github.com/ledgerwatch/erigon/params"
	cli2 "github.com/ledgerwatch/erigon/turbo/cli"
	"github.com/ledgerwatch/erigon/turbo/debug"
	"github.com/ledgerwatch/erigon/zk/relay"
	"github.com/ledgerwatch/log/v3"
	"github.com/urfave/cli/v2"
	"net"
	"os"
	"strings"
)

var (
	remoteDsUrlFlag = &cli.StringFlag{
		Name:  "remote-ds-url",
		Usage: "Url of the remote datastream server",
	}
	relayPortFlag = &cli.UintFlag{
		Name:  "relay-port",
		Usage: "Define the port used for the zkevm data stream",
	}
	streamDirFlag = &cli.StringFlag{
		Name:  "stream-dir",
		Usage: "dir of the stream",
	}
	rpcUrlFlag = &cli.StringFlag{
		Name:  "rpc-url",
		Usage: "Url of the RPC server",
	}
	txBinFlag = &cli.StringFlag{
		Name:  "tx-bin",
		Usage: "Location of the tx binary file",
	}

	Relay = cli.Command{
		Action: runRelay,
		Name:   "relay",
		Usage:  "Replay datastream",
		Flags: []cli.Flag{
			remoteDsUrlFlag,
			relayPortFlag,
			streamDirFlag,
		},
	}
	Replayer = cli.Command{
		Action: runReplayer,
		Name:   "replayer",
		Usage:  "Replay datastream",
		Flags: []cli.Flag{
			remoteDsUrlFlag,
			rpcUrlFlag,
		},
	}
	Send = cli.Command{
		Action: runSend,
		Name:   "send",
		Usage:  "Read from tx binary file.",
		Flags: []cli.Flag{
			txBinFlag,
		},
	}
)

func main() {
	app := cli2.NewApp(params.GitCommit, "Datastream Relay")
	app.Name = "ds-relay"
	app.UsageText = app.Name + ` [command] [flags]`
	app.Before = preStartRelay
	app.After = finishRelay
	app.Commands = []*cli.Command{
		&Relay,
		&Replayer,
		&Send,
	}

	if err := app.Run(os.Args); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func preStartRelay(cliCtx *cli.Context) error {
	logLvl := log.LvlInfo
	logger := log.Root()
	consoleHandler := log.LvlFilterHandler(logLvl, log.StreamHandler(os.Stdout, log.TerminalFormat()))
	logger.SetHandler(consoleHandler)
	return nil
}

func finishRelay(cliCtx *cli.Context) error {
	debug.Exit()
	return nil
}

func runRelay(cliCtx *cli.Context) error {
	log.Info("Starting Datastream Relay...")

	dsUrl := cliCtx.String(remoteDsUrlFlag.Name)
	port := cliCtx.Uint(relayPortFlag.Name)
	streamDir := cliCtx.String(streamDirFlag.Name)

	if strings.Count(dsUrl, ":") == 0 {
		return fmt.Errorf("invalid address for flag %s: %s", remoteDsUrlFlag.Name, dsUrl)
	}

	_, _, err := net.SplitHostPort(dsUrl)
	if err != nil {
		return fmt.Errorf("invalid address for flag %s: %s", remoteDsUrlFlag.Name, dsUrl)
	}

	r, err := relay.NewRelay(cliCtx.Context, dsUrl, port, streamDir)
	if err != nil {
		return err
	}

	return r.Run()
}

func runReplayer(cliCtx *cli.Context) error {
	log.Info("Running Datastream Replayer")
	dsUrl := cliCtx.String(remoteDsUrlFlag.Name)
	if strings.Count(dsUrl, ":") == 0 {
		return fmt.Errorf("invalid address for flag %s: %s", remoteDsUrlFlag.Name, dsUrl)
	}

	_, _, err := net.SplitHostPort(dsUrl)
	if err != nil {
		return fmt.Errorf("invalid address for flag %s: %s", remoteDsUrlFlag.Name, dsUrl)
	}

	rpc := cliCtx.String(rpcUrlFlag.Name)
	return relay.NewReplayer(dsUrl, rpc).Run(cliCtx.Context)
}

func runSend(cliCtx *cli.Context) error {
	log.Info("Running Send")
	binLocation := cliCtx.String(txBinFlag.Name)
	return relay.NewSender(binLocation).Run(cliCtx.Context)
}

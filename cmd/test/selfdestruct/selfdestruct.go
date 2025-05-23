package selfdestruct

import (
	"fmt"
	ethereum "github.com/erigontech/erigon"
	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/common/hexutil"
	"github.com/erigontech/erigon-lib/crypto"
	"github.com/erigontech/erigon/accounts/abi"
	"github.com/erigontech/erigon/cmd/test/helpers"
	"github.com/erigontech/erigon/core/types"
	"github.com/erigontech/erigon/ethclient"
	"github.com/holiman/uint256"
	"github.com/urfave/cli/v2"
	"os"
)

var (
	rpcUrl  string
	privKey string
	addr    string
)

var Command = cli.Command{
	Action: run,
	Name:   "selfdestruct",
	Usage:  "Test SELFDESTRUCT opcode in type-1 mode",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:        "rpc-url",
			Usage:       "L2 RPC URL",
			Destination: &rpcUrl,
		},
		&cli.StringFlag{
			Name:        "priv-key",
			Usage:       "Private key to use for signing transactions",
			Destination: &privKey,
		},
		&cli.StringFlag{
			Name:        "address",
			Usage:       "Address to use for signing transactions",
			Destination: &addr,
		},
	},
}

func run(cliCtx *cli.Context) error {
	if cliCtx.String("rpc-url") == "" {
		return cli.Exit("L2 RPC URL is required", 1)
	}
	if cliCtx.String("priv-key") == "" {
		return cli.Exit("Private key is required", 1)
	}
	if cliCtx.String("address") == "" {
		return cli.Exit("Address is required", 1)
	}

	client, err := ethclient.Dial(rpcUrl)
	if err != nil {
		return cli.Exit("Failed to connect to the Ethereum client: "+err.Error(), 1)
	}

	logBalance := func(label string, addr common.Address) {
		bal, _ := client.BalanceAt(cliCtx.Context, addr, nil)
		fmt.Printf("%s: %s\n", label, bal)
	}

	keyBytes, err := hexutil.Decode(privKey)
	if err != nil {
		return cli.Exit("Invalid hex private key: "+err.Error(), 1)
	}

	privateKey, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		return cli.Exit("Failed to create private key: "+err.Error(), 1)
	}

	c, err := helpers.CompileContract("cmd/test/contracts/selfdestruct.sol")
	if err != nil {
		return cli.Exit("Failed to compile contract: "+err.Error(), 1)
	}

	deployReceipt, err := helpers.DeployContract(cliCtx.Context, client, privateKey, c)
	if err != nil {
		return cli.Exit("Failed to deploy contract: "+err.Error(), 1)
	}

	contractAddr := deployReceipt.ContractAddress

	codeDeployed, err := client.CodeAt(cliCtx.Context, contractAddr, deployReceipt.BlockNumber)
	if err != nil {
		return cli.Exit("Failed to get contract code: "+err.Error(), 1)
	}

	if len(codeDeployed) == 0 {
		return cli.Exit("Contract code is empty after deployment!", 1)
	}

	fmt.Println("--------------STARTING BALANCES--------------")

	logBalance("Deployer balance", crypto.PubkeyToAddress(privateKey.PublicKey))
	logBalance("Receiver balance", common.HexToAddress(addr))
	logBalance("Contract balance", contractAddr)

	_, err = helpers.FundContract(cliCtx.Context, client, privateKey, contractAddr, uint256.NewInt(1000000000000000000))
	if err != nil {
		return cli.Exit("Failed to fund contract: "+err.Error(), 1)
	}

	fmt.Println("--------------BALANCES AFTER FUNDING--------------")

	logBalance("Deployer balance", crypto.PubkeyToAddress(privateKey.PublicKey))
	logBalance("Receiver balance", common.HexToAddress(addr))
	logBalance("Contract balance", contractAddr)

	abiFile, err := os.Open("cmd/test/abi/SelfDestruct.abi")
	if err != nil {
		return err
	}
	defer abiFile.Close()

	parsed, err := abi.JSON(abiFile)
	if err != nil {
		return cli.Exit("Failed to parse ABI: "+err.Error(), 1)
	}

	txData, err := parsed.Pack("kill", common.HexToAddress(addr))
	if err != nil {
		return cli.Exit("Failed to pack transaction data: "+err.Error(), 1)
	}

	gasPrice, err := client.SuggestGasPrice(cliCtx.Context)
	if err != nil {
		return cli.Exit("Error fetching gas price: "+err.Error(), 1)
	}

	msg := ethereum.CallMsg{
		From:     crypto.PubkeyToAddress(privateKey.PublicKey),
		To:       &contractAddr,
		GasPrice: uint256.NewInt(gasPrice.Uint64()),
		Value:    uint256.NewInt(0),
		Data:     txData,
	}

	gasLimit, err := client.EstimateGas(cliCtx.Context, msg)
	if err != nil {
		return cli.Exit("Error estimating gas: "+err.Error(), 1)
	}

	nonce, err := client.PendingNonceAt(cliCtx.Context, crypto.PubkeyToAddress(privateKey.PublicKey))
	if err != nil {
		return cli.Exit("Failed to get nonce: "+err.Error(), 1)
	}

	tx := types.NewTransaction(nonce, contractAddr, uint256.NewInt(0), gasLimit, uint256.NewInt(gasPrice.Uint64()), txData)

	chainID, err := client.ChainID(cliCtx.Context)
	if err != nil {
		return cli.Exit("Failed to get chain ID: "+err.Error(), 1)
	}

	signer := types.LatestSignerForChainID(chainID)

	signedTx, err := types.SignTx(tx, *signer, privateKey)
	if err != nil {
		return cli.Exit("Failed to sign transaction: "+err.Error(), 1)
	}

	if err = client.SendTransaction(cliCtx.Context, signedTx); err != nil {
		return cli.Exit("Failed to send transaction: "+err.Error(), 1)
	}

	_, err = helpers.WaitForReceipt(client, signedTx.Hash())
	if err != nil {
		return cli.Exit("Failed to wait for transaction receipt: "+err.Error(), 1)
	}

	fmt.Println("--------------BALANCES AFTER SELFDESTRUCT--------------")

	logBalance("Deployer balance", crypto.PubkeyToAddress(privateKey.PublicKey))
	logBalance("Receiver balance", common.HexToAddress(addr))
	logBalance("Contract balance", contractAddr)

	codeAfter, err := client.CodeAt(cliCtx.Context, contractAddr, deployReceipt.BlockNumber)
	if err != nil {
		return cli.Exit("Failed to get contract code: "+err.Error(), 1)
	}

	if len(codeAfter) == 0 {
		return cli.Exit("Contract code is empty after selfdestruct!", 1)
	}

	fmt.Println("Contract code still exists after SELFDESTRUCT was called.")

	fmt.Println("--------------END OF TEST--------------")
	return nil
}

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"slices"
	"strings"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/ethclient"
	"github.com/schollz/progressbar/v3"
)

var (
	dumpStateFile  = flag.String("dump-state-file", "", "dump state JSON file")
	ignoreListFile = flag.String("ignore-list-file", "", "ignore accounts or contract addresses in the JSON file")
	rpcURL         = flag.String("rpc-url", "", "rpc url")
	progressBar    = flag.Bool("progress-bar", true, "show progress bar")
)

// AccountState represents the structure of account data in the state dump
type AccountState struct {
	Balance string            `json:"balance"`
	Nonce   string            `json:"nonce"`
	Code    string            `json:"code"`
	Storage map[string]string `json:"storage"`
}

// verifyAccountState verifies a single account's state against the Ethereum node
func verifyAccountState(address string, accountData AccountState, client *ethclient.Client) error {
	ctx := context.Background()

	// Parse the address
	addr := common.HexToAddress(address)

	// 1. Verify balance
	balanceRPC, err := client.BalanceAt(ctx, addr, nil)
	if err != nil {
		return fmt.Errorf("address: %s balance is invalid: %v", address, err)
	}

	var balanceDump *big.Int
	var ok bool
	if strings.HasPrefix(accountData.Balance, "0x") {
		balanceStr := strings.TrimPrefix(accountData.Balance, "0x")
		balanceDump, ok = new(big.Int).SetString(balanceStr, 16)
	} else {
		balanceDump, ok = new(big.Int).SetString(accountData.Balance, 10)
	}
	if !ok {
		return fmt.Errorf("address: %s invalid balance format in dump: %s", address, accountData.Balance)
	}

	if balanceRPC.ToBig().Cmp(balanceDump) != 0 {
		return fmt.Errorf("address: %s balances do not match: %s (RPC) != %s (dump)", address, balanceRPC.ToBig().String(), balanceDump.String())
	}

	// 2. Verify nonce
	nonceRPC, err := client.NonceAt(ctx, addr, nil)
	if err != nil {
		return fmt.Errorf("address: %s nonce is invalid: %v", address, err)
	}

	nonceStr := strings.TrimPrefix(accountData.Nonce, "0x")
	nonceDump, ok := new(big.Int).SetString(nonceStr, 16)
	if !ok {
		return fmt.Errorf("address: %s invalid nonce format in dump: %s", address, accountData.Nonce)
	}

	if new(big.Int).SetUint64(nonceRPC).Cmp(nonceDump) != 0 {
		return fmt.Errorf("address: %s nonce not match: %v (RPC) != %s (dump)", address, nonceRPC, nonceDump.String())
	}

	// 3. Verify code (if not empty)
	if accountData.Code != "0x" {
		code, err := client.CodeAt(ctx, addr, nil)
		if err != nil {
			return fmt.Errorf("address: %s code is invalid: %v", address, err)
		}

		codeHex := "0x" + hex.EncodeToString(code)
		if codeHex != accountData.Code {
			return fmt.Errorf("address: %s code does not match", address)
		}

		// 4. Verify storage slots
		for storageKey, valueDump := range accountData.Storage {
			// Parse storage key
			key := common.HexToHash(storageKey)

			// Get storage value
			storageValue, err := client.StorageAt(ctx, addr, key, nil)
			if err != nil {
				return fmt.Errorf("address: %s storage is invalid for key %s: %v", address, storageKey, err)
			}

			valueRPC := "0x" + hex.EncodeToString(storageValue)

			if valueRPC != valueDump {
				return fmt.Errorf("address: %s storage not match for key %s: %s (RPC) != %s (dump)", address, storageKey, valueRPC, valueDump)
			}
		}
	}
	return nil
}

// checkState performs the main state verification logic
func checkState(dumpStateFile, rpcURL, ignoreListFile string) error {
	// Read and parse the state dump file
	fileContent, err := os.ReadFile(dumpStateFile)
	if err != nil {
		return fmt.Errorf("failed to read state file: %s", dumpStateFile)
	}

	var fileData map[string]interface{}
	var stateDump map[string]AccountState
	if err := json.Unmarshal(fileContent, &fileData); err != nil {
		return fmt.Errorf("failed to parse JSON: %v", err)
	}
	if _, ok := fileData["alloc"]; !ok {
		fmt.Println("No alloc field found in state dump!")
		os.Exit(1)
	} else {
		allocBytes, err := json.Marshal(fileData["alloc"])
		if err != nil {
			return fmt.Errorf("failed to marshal alloc: %v", err)
		}
		if err := json.Unmarshal(allocBytes, &stateDump); err != nil {
			return fmt.Errorf("failed to unmarshal alloc: %v", err)
		}
	}

	var ignoreList []string
	if ignoreListFile != "" {
		fileContent, err := os.ReadFile(ignoreListFile)
		if err != nil {
			return fmt.Errorf("failed to read ignore list file: %s", ignoreList)
		}
		if err := json.Unmarshal(fileContent, &ignoreList); err != nil {
			return fmt.Errorf("failed to unmarshal ignore list: %v", err)
		}
	}

	fmt.Println("Finish loading state dump file.")

	// Log CPU information
	// cpus := runtime.NumCPU()
	// fmt.Printf("CPUs available: %d\n", cpus)

	// Connect to Ethereum client
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return fmt.Errorf("failed to connect to Ethereum client: %v", err)
	}
	defer client.Close()

	// Verify each account
	ok := true
	var bar *progressbar.ProgressBar
	if *progressBar {
		bar = progressbar.NewOptions(len(stateDump), progressbar.OptionSetPredictTime(true))
	}
	for address, accountData := range stateDump {
		if slices.Contains(ignoreList, address) {
			fmt.Printf("\nIgnoring address: %s\n", address)
			continue
		}
		if err := verifyAccountState(address, accountData, client); err != nil {
			fmt.Printf("\nverification failed: %v\n", err)
			ok = false
		}
		if *progressBar {
			bar.Add(1)
		}
	}
	if *progressBar {
		bar.Finish()
	}
	fmt.Println()

	if !ok {
		return fmt.Errorf("verification failed")
	}
	return nil
}

func main() {
	flag.Parse()

	if *dumpStateFile == "" {
		fmt.Println("dump-state-file is required")
		os.Exit(1)
	}
	if *rpcURL == "" {
		fmt.Println("rpc-url is required")
		os.Exit(1)
	}

	fmt.Printf("dump state file: %s\n", *dumpStateFile)
	fmt.Printf("rpc url: %s\n", *rpcURL)

	if err := checkState(*dumpStateFile, *rpcURL, *ignoreListFile); err != nil {
		fmt.Printf("check fail: %s\n", err)
		os.Exit(1)
	} else {
		fmt.Println("check pass")
	}
}

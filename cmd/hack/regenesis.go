package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/holiman/uint256"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/hexutil"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/mdbx"
	"github.com/ledgerwatch/erigon/common"
	"github.com/ledgerwatch/erigon/core/types/accounts"
	db2 "github.com/ledgerwatch/erigon/smt/pkg/db"
	"github.com/ledgerwatch/erigon/smt/pkg/smt"
	"maps"
	"os"
)

// VerifySmtWithStateDiff given two genesis files, generate a state diff file
//
// `preSmtData` is the previous smt data path
// `preChainData` is the previous chain data path
// `preStateSnapshotFilePath` is the path to the pre-state snapshot file.
// `postSmtData` is the post smt data path
// `postStateSnapshotFilePath` is the path to the post-state snapshot file.
// `outputStateDiffFilePath` is the path to the output state diff file.
//
// It returns an error if the operation fails.
func VerifySmtWithStateDiff(
	preSmtData,
	preChainData,
	preStateSnapshotFilePath,
	postSmtData,
	postStateSnapshotFilePath,
	outputStateDiffFilePath string) error {

	if preSmtData == "" || preChainData == "" || preStateSnapshotFilePath == "" {
		panic("pre data is empty")
	}

	if postStateSnapshotFilePath == "" || postSmtData == "" {
		panic("post data is empty")
	}

	// Read pre-state snapshot
	preStateData, err := os.ReadFile(preStateSnapshotFilePath)
	if err != nil {
		return fmt.Errorf("failed to read pre-state snapshot file: %w", err)
	}

	var preState map[string]map[string]AccInfo
	if err := json.Unmarshal(preStateData, &preState); err != nil {
		return fmt.Errorf("failed to unmarshal pre-state snapshot: %w", err)
	}

	// Read post-state snapshot
	postStateData, err := os.ReadFile(postStateSnapshotFilePath)
	if err != nil {
		return fmt.Errorf("failed to read post-state snapshot file: %w", err)
	}

	var postState map[string]map[string]AccInfo
	if err := json.Unmarshal(postStateData, &postState); err != nil {
		return fmt.Errorf("failed to unmarshal post-state snapshot: %w", err)
	}

	// Extract alloc data from both states
	preAlloc, preExists := preState["alloc"]
	if !preExists {
		return fmt.Errorf("pre-state snapshot does not contain 'alloc' field")
	}

	postAlloc, postExists := postState["alloc"]
	if !postExists {
		return fmt.Errorf("post-state snapshot does not contain 'alloc' field")
	}

	ctx := context.Background()
	var txPreSmt kv.RwTx = nil

	fmt.Printf("Start open pre smt data: %s\n", preSmtData)
	dbPreSmt := mdbx.MustOpen(preSmtData)
	defer dbPreSmt.Close()
	txPreSmt, err = dbPreSmt.BeginRw(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Start open pre chain data: %s\n", preSmtData)
	dbPreChain := mdbx.MustOpen(preChainData)
	defer dbPreChain.Close()
	txPreChain, err := dbPreChain.BeginRw(ctx)

	fmt.Printf("Start open post smt data: %s\n", postSmtData)
	dbPostSmt := mdbx.MustOpen(postSmtData)
	defer dbPostSmt.Close()
	txPostSmt, err := dbPostSmt.BeginRw(ctx)
	if err != nil {
		panic(err)
	}

	// Initialize change maps similar to checkStateRoot
	accChanges := make(map[libcommon.Address]*accounts.Account)
	codeChanges := make(map[libcommon.Address]string)
	storageChanges := make(map[libcommon.Address]map[string]string)

	// Process all accounts in post-state (insertions and modifications)
	for addr, postAccValue := range postAlloc {
		accBytes := common.FromHex(addr)
		if err != nil {
			panic(fmt.Sprintf("preAlloc failed with addr: %s", addr))
		}
		address := libcommon.BytesToAddress(accBytes)
		acc := accounts.NewAccount()
		preAccValue, exists := preAlloc[addr]
		if !exists || postAccValue.Balance != preAccValue.Balance || postAccValue.Nonce != preAccValue.Nonce {
			balance, err := uint256.FromHex(postAccValue.Balance)
			if err != nil {
				panic(fmt.Sprintf("acc decoding balance error for acct: %s, err: %v", address, err))
			}
			acc.Balance = *balance

			nonce, err := hexutil.DecodeUint64(postAccValue.Nonce)
			if err != nil {
				panic(fmt.Sprintf("acc decoding nonce error for acct: %s, err: %v", address, err))
			}
			acc.Nonce = nonce
			accChanges[address] = &acc
		}

		if !exists || postAccValue.Code != postAccValue.Code {
			codeChanges[address] = postAccValue.Code
		}

		if postAccValue.Storage != nil {
			if !exists || preAccValue.Storage == nil { // use new to override
				storageChanges[address] = maps.Clone(postAccValue.Storage)
			} else { // only apply diff
				for k, v := range postAccValue.Storage {
					if preAccValue.Storage[k] != v {
						storageChanges[address][k] = v
					}
				}

			}
		}
	}

	// Check for deletions (accounts that exist in pre-state but not in post-state)
	for addr, preAccValue := range preAlloc {
		accBytes := common.FromHex(addr)
		if err != nil {
			panic(fmt.Sprintf("preAlloc addr hex decode failed: %s, err: %v", addr, err))
		}
		address := libcommon.BytesToAddress(accBytes)
		acc := accounts.NewAccount()

		if _, exists := postAlloc[addr]; !exists {
			acc.Balance = uint256.Int{}
			acc.Nonce = 0

			accChanges[address] = &acc
			codeChanges[address] = "0x"

			if preAccValue.Storage != nil {
				for k, _ := range preAccValue.Storage {
					storageChanges[address][k] = "0x"
				}
			}

		}
	}

	// Create output structure
	outputData := map[string]interface{}{
		"accountChange": accChanges,
		"codeChange":    codeChanges,
		"storageChange": storageChanges,
	}

	// Marshal to JSON with indentation
	outputJSON, err := json.MarshalIndent(outputData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state diff: %w", err)
	}

	// Write to output file
	if err := os.WriteFile(outputStateDiffFilePath, outputJSON, 0644); err != nil {
		return fmt.Errorf("failed to write state diff file: %w", err)
	}

	fmt.Printf("State diff generated successfully. Output written to: %s\n", outputStateDiffFilePath)

	fmt.Println("Start apply changes to pre smt DB")
	preEriDb := db2.NewEriDb(txPreSmt, txPreChain)
	smtPre := smt.NewSMT(preEriDb, false)
	_, _, err = smtPre.SetStorage(ctx, "", accChanges, codeChanges, storageChanges)
	if err != nil {
		fmt.Println("SetStorage error ", err)
		panic("SetStorage: " + err.Error())
	}
	postEriDb := db2.NewEriDb(txPostSmt, nil)
	smtPost := smt.NewSMT(postEriDb, false)
	if smtPost.LastRoot() == smtPre.LastRoot() {
		fmt.Println("Verify success")
	} else {
		fmt.Printf("Verify fail, pre smt after apply change root is: %v, post smt root is: %v \n", smtPre.LastRoot(), smtPost.LastRoot())
	}

	return nil
}

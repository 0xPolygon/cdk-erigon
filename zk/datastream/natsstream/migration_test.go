//go:build integration

package natsstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDualSyncValidation(t *testing.T) {
	seqRPCURL := os.Getenv("SEQ_RPC_URL")
	if seqRPCURL == "" {
		t.Fatal("SEQ_RPC_URL environment variable not set")
	}

	rpcNodeURL := os.Getenv("RPC_NODE_URL")
	if rpcNodeURL == "" {
		t.Fatal("RPC_NODE_URL environment variable not set")
	}

	t.Logf("Sequencer RPC: %s", seqRPCURL)
	t.Logf("RPC Node: %s", rpcNodeURL)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	t.Log("=== VERIFICATION: RPC Node Sync ===")

	seqBlockNum := getBlockNumber(t, ctx, seqRPCURL)
	t.Logf("Sequencer block number: %d", seqBlockNum)

	require.Greater(t, seqBlockNum, uint64(0), "Sequencer has no blocks")

	syncTimeout := time.After(90 * time.Second)
	syncTicker := time.NewTicker(3 * time.Second)
	defer syncTicker.Stop()

	var rpcBlockNum uint64
	synced := false

syncLoop:
	for {
		select {
		case <-syncTicker.C:
			rpcBlockNum = getBlockNumber(t, ctx, rpcNodeURL)
			t.Logf("RPC node block: %d, Sequencer block: %d, lag: %d",
				rpcBlockNum, seqBlockNum, seqBlockNum-rpcBlockNum)

			if rpcBlockNum >= seqBlockNum-2 {
				synced = true
				break syncLoop
			}

			seqBlockNum = getBlockNumber(t, ctx, seqRPCURL)

		case <-syncTimeout:
			t.Fatalf("RPC node failed to sync within 90s (at block %d, need %d)",
				rpcBlockNum, seqBlockNum)

		case <-ctx.Done():
			t.Fatal("Context cancelled during sync wait")
		}
	}

	require.True(t, synced, "RPC node did not sync")
	t.Logf("✓ RPC node synced to within 2 blocks (RPC: %d, Seq: %d)", rpcBlockNum, seqBlockNum)

	t.Log("=== VERIFICATION: Block Data Comparison ===")

	compareBlocks := 10
	if seqBlockNum < uint64(compareBlocks) {
		compareBlocks = int(seqBlockNum)
	}

	endBlock := rpcBlockNum
	if seqBlockNum < endBlock {
		endBlock = seqBlockNum
	}

	startBlock := endBlock - uint64(compareBlocks) + 1
	for blockNum := startBlock; blockNum <= endBlock; blockNum++ {
		seqBlock := getBlockByNumber(t, ctx, seqRPCURL, blockNum)
		rpcBlock := getBlockByNumber(t, ctx, rpcNodeURL, blockNum)

		assert.Equal(t, seqBlock.Hash, rpcBlock.Hash,
			"Block %d hash mismatch", blockNum)
		assert.Equal(t, seqBlock.TxCount, rpcBlock.TxCount,
			"Block %d transaction count mismatch", blockNum)

		t.Logf("✓ Block %d verified (hash: %s, txs: %d)",
			blockNum, seqBlock.Hash, seqBlock.TxCount)
	}

	t.Log("=== DUAL-SYNC VALIDATION SUMMARY ===")
	t.Logf("✓ RPC node synced to block %d", rpcBlockNum)
	t.Logf("✓ Last %d blocks verified for consistency", compareBlocks)
	t.Log("✓ DUAL-SYNC VERIFIED")
}

type rpcBlockInfo struct {
	Hash    string
	TxCount int
}

func getBlockNumber(t *testing.T, ctx context.Context, rpcURL string) uint64 {
	t.Helper()

	client := &http.Client{Timeout: 10 * time.Second}
	reqBody := `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`

	req, err := http.NewRequestWithContext(ctx, "POST", rpcURL, strings.NewReader(reqBody))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Logf("RPC request failed (may be starting up): %v", err)
		return 0
	}
	defer resp.Body.Close()

	var result struct {
		Result string `json:"result"`
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Logf("Failed to decode response: %v", err)
		return 0
	}

	if result.Result == "" {
		return 0
	}

	blockNum, err := strconv.ParseUint(result.Result[2:], 16, 64)
	if err != nil {
		t.Logf("Failed to parse block number %s: %v", result.Result, err)
		return 0
	}

	return blockNum
}

func getBlockByNumber(t *testing.T, ctx context.Context, rpcURL string, blockNum uint64) *rpcBlockInfo {
	t.Helper()

	client := &http.Client{Timeout: 10 * time.Second}
	blockHex := fmt.Sprintf("0x%x", blockNum)
	reqBody := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["%s",false],"id":1}`, blockHex)

	req, err := http.NewRequestWithContext(ctx, "POST", rpcURL, strings.NewReader(reqBody))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	var result struct {
		Result struct {
			Hash         string   `json:"hash"`
			Transactions []string `json:"transactions"`
		} `json:"result"`
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	return &rpcBlockInfo{
		Hash:    result.Result.Hash,
		TxCount: len(result.Result.Transactions),
	}
}

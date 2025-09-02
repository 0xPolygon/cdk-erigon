package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/ethclient"
	"github.com/schollz/progressbar/v3"
	"golang.org/x/sync/errgroup"
)

var (
	dumpStateFile   = flag.String("dump-state-file", "", "dump state JSON file")
	ignoreListFile  = flag.String("ignore-list-file", "", "ignore accounts or contract addresses in the JSON file")
	rpcURL          = flag.String("rpc-url", "", "rpc url")
	progressBar     = flag.Bool("progress-bar", true, "show progress bar")
	connectionCount = flag.Int("connection-count", 10, "number of RPC connections in the pool")
)

// AccountState represents the structure of account data in the state dump
type AccountState struct {
	Balance string            `json:"balance"`
	Nonce   string            `json:"nonce"`
	Code    string            `json:"code"`
	Storage map[string]string `json:"storage"`
}

// ClientPool manages a pool of ethclient connections
type ClientPool struct {
	clients chan *ethclient.Client
}

// NewClientPool creates a new client pool with the specified size
func NewClientPool(rpcURL string, poolSize int) (*ClientPool, error) {
	pool := &ClientPool{
		clients: make(chan *ethclient.Client, poolSize),
	}

	// Create initial clients
	for i := 0; i < poolSize; i++ {
		client, err := ethclient.Dial(rpcURL)
		if err != nil {
			// Close any clients that were successfully created
			pool.Close()
			return nil, fmt.Errorf("failed to create client %d: %v", i, err)
		}
		pool.clients <- client
	}

	return pool, nil
}

// GetClient gets a client from the pool (blocks if none available)
func (p *ClientPool) GetClient() *ethclient.Client {
	return <-p.clients
}

// ReturnClient returns a client to the pool
func (p *ClientPool) ReturnClient(client *ethclient.Client) {
	p.clients <- client
}

// Close closes all clients in the pool
func (p *ClientPool) Close() {
	close(p.clients)
	for client := range p.clients {
		client.Close()
	}
}

// verifyAccountState verifies a single account's state against the Ethereum node
func verifyAccountState(address string, accountData AccountState, clientPool *ClientPool, bar *progressbar.ProgressBar, mu *sync.Mutex) error {
	// Create context with no timeout
	ctx := context.Background()

	// Parse the address
	addr := common.HexToAddress(address)

	// 1. Verify balance
	client := clientPool.GetClient()
	balanceRPC, err := client.BalanceAt(ctx, addr, nil)
	clientPool.ReturnClient(client)
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
	client = clientPool.GetClient()
	nonceRPC, err := client.NonceAt(ctx, addr, nil)
	clientPool.ReturnClient(client)
	if err != nil {
		return fmt.Errorf("address: %s nonce is invalid: %v", address, err)
	}

	if accountData.Nonce != "" {
		nonceStr := strings.TrimPrefix(accountData.Nonce, "0x")
		nonceDump, ok := new(big.Int).SetString(nonceStr, 16)
		if !ok {
			return fmt.Errorf("address: %s invalid nonce format in dump: %s", address, accountData.Nonce)
		}

		if new(big.Int).SetUint64(nonceRPC).Cmp(nonceDump) != 0 {
			return fmt.Errorf("address: %s nonce not match: %v (RPC) != %s (dump)", address, nonceRPC, nonceDump.String())
		}
	}

	// 3. Verify code (if not empty)
	if accountData.Code != "" && accountData.Code != "0x" {
		client = clientPool.GetClient()
		code, err := client.CodeAt(ctx, addr, nil)
		clientPool.ReturnClient(client)
		if err != nil {
			return fmt.Errorf("address: %s code is invalid: %v", address, err)
		}

		codeHex := "0x" + hex.EncodeToString(code)
		if codeHex != accountData.Code {
			return fmt.Errorf("address: %s code does not match", address)
		}
	}

	// 4. Verify storage slots
	// record current time
	//now := time.Now()

	// Split storage verification into chunks of 100,000 and use errgroup
	if len(accountData.Storage) > 0 {
		err := verifyStorageInChunks(ctx, addr, address, accountData.Storage, clientPool, bar, mu)
		if err != nil {
			return err
		}
	}

	// if code is empty but has storage, return error
	//if (accountData.Code == "" || accountData.Code == "0x") && len(accountData.Storage) > 0 {
	//	fmt.Printf("address: %s code is empty but has storage slots: %d\n", address, len(accountData.Storage))
	//}

	// if len(accountData.Storage) > 10000 {
	// log time cost
	//fmt.Printf("address: %s has %d storage slots, cost: %s\n", address, len(accountData.Storage), time.Since(now))
	//}

	// Update progress for account completion
	if bar != nil {
		mu.Lock()
		bar.Add(1)
		mu.Unlock()
	}
	return nil
}

// storageItem represents a single storage slot
type storageItem struct {
	key   string
	value string
}

// verifyStorageInChunks verifies storage slots in chunks of 10,000 using errgroup
func verifyStorageInChunks(ctx context.Context, addr common.Address, address string, storage map[string]string, clientPool *ClientPool, bar *progressbar.ProgressBar, mu *sync.Mutex) error {
	const chunkSize = 3000

	// If storage is small enough, process directly without converting to slice
	if len(storage) <= chunkSize {
		return verifyStorageMap(ctx, addr, address, storage, clientPool, bar, mu)
	}

	// Convert storage map to slice for chunking
	var storageItems []storageItem
	for key, value := range storage {
		storageItems = append(storageItems, storageItem{key: key, value: value})
	}

	// Split into chunks and process with errgroup
	g, _ := errgroup.WithContext(ctx)

	for i := 0; i < len(storageItems); i += chunkSize {
		end := i + chunkSize
		if end > len(storageItems) {
			end = len(storageItems)
		}

		// Use slice directly without creating new array
		chunk := storageItems[i:end]
		g.Go(func() error {
			return verifyStorageChunk(ctx, addr, address, chunk, clientPool, bar, mu)
		})
	}

	return g.Wait()
}

// verifyStorageSlot verifies a single storage slot
func verifyStorageSlot(ctx context.Context, addr common.Address, address, storageKey, valueDump string, clientPool *ClientPool) error {
	// Parse storage key
	key := common.HexToHash(storageKey)

	// Get storage value
	client := clientPool.GetClient()
	storageValue, err := client.StorageAt(ctx, addr, key, nil)
	clientPool.ReturnClient(client)
	if err != nil {
		return fmt.Errorf("address: %s storage is invalid for key %s: %v", address, storageKey, err)
	}

	valueRPC := "0x" + hex.EncodeToString(storageValue)

	if valueRPC != valueDump {
		return fmt.Errorf("address: %s storage not match for key %s: %s (RPC) != %s (dump)", address, storageKey, valueRPC, valueDump)
	}

	return nil
}

// verifyStorageMap verifies storage slots directly from map
func verifyStorageMap(ctx context.Context, addr common.Address, address string, storage map[string]string, clientPool *ClientPool, bar *progressbar.ProgressBar, mu *sync.Mutex) error {
	count := 0
	for storageKey, valueDump := range storage {
		if err := verifyStorageSlot(ctx, addr, address, storageKey, valueDump, clientPool); err != nil {
			return err
		}
		count++
		// Update progress for each storage slot
		if bar != nil {
			if count%50 == 0 {
				mu.Lock()
				bar.Add(count)
				mu.Unlock()
				count = 0
			}
		}
	}

	if bar != nil {
		mu.Lock()
		bar.Add(count)
		mu.Unlock()
		count = 0
	}

	return nil
}

// verifyStorageChunk verifies a chunk of storage slots
func verifyStorageChunk(ctx context.Context, addr common.Address, address string, storageItems []storageItem, clientPool *ClientPool, bar *progressbar.ProgressBar, mu *sync.Mutex) error {
	count := 0
	for _, item := range storageItems {
		if err := verifyStorageSlot(ctx, addr, address, item.key, item.value, clientPool); err != nil {
			return err
		}
		count++
		// Update progress for each storage slot
		if bar != nil {
			if count%50 == 0 {
				mu.Lock()
				bar.Add(count)
				mu.Unlock()
				count = 0
			}
		}
	}

	if bar != nil {
		mu.Lock()
		bar.Add(count)
		mu.Unlock()
		count = 0
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

	// Define a structure that matches the JSON format
	var stateFile struct {
		Alloc map[string]AccountState `json:"alloc"`
	}

	if err := json.Unmarshal(fileContent, &stateFile); err != nil {
		return fmt.Errorf("failed to parse JSON: %v", err)
	}

	if stateFile.Alloc == nil {
		fmt.Println("No alloc field found in state dump!")
		os.Exit(1)
	}

	stateDump := stateFile.Alloc

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

	// Calculate total progress units: accounts + storage slots
	totalProgressUnits := len(stateDump)
	totalStorageSlots := 0
	for address, accountData := range stateDump {
		if slices.Contains(ignoreList, address) {
			continue
		}
		totalStorageSlots += len(accountData.Storage)
		totalProgressUnits += len(accountData.Storage)
	}

	totalAccounts := len(stateDump)
	fmt.Printf("Total accounts to verify: %d\n", totalAccounts)
	fmt.Printf("Total storage slots to verify: %d\n", totalStorageSlots)

	// Log CPU information
	cpus := runtime.NumCPU()
	fmt.Printf("CPUs available: %d\n", cpus)

	// Create client pool
	clientPool, err := NewClientPool(rpcURL, *connectionCount)
	if err != nil {
		return fmt.Errorf("failed to create client pool: %v", err)
	}
	defer clientPool.Close()

	// Verify each account concurrently
	ok := true
	var bar *progressbar.ProgressBar
	if *progressBar {
		bar = progressbar.NewOptions(totalProgressUnits,
			progressbar.OptionSetPredictTime(true),
			progressbar.OptionShowCount(),
			progressbar.OptionSetDescription("Verifying state"),
		)
	}

	// Split: Launch goroutines for each account using errgroup
	g, _ := errgroup.WithContext(context.Background())
	var mu sync.Mutex // Protect ok variable and progress bar

	for address, accountData := range stateDump {
		if slices.Contains(ignoreList, address) {
			fmt.Printf("\nIgnoring address: %s\n", address)
			continue
		}

		addr, data := address, accountData // Capture loop variables
		g.Go(func() error {
			// Verify account state
			err := verifyAccountState(addr, data, clientPool, bar, &mu)

			// Print error directly and update status
			if err != nil {
				mu.Lock()
				fmt.Printf("\nverification failed: %v\n", err)
				ok = false
				mu.Unlock()
			}

			return nil // no need to return error, because we already print error in the goroutine
		})
	}

	// Join: Wait for all goroutines to complete
	if err := g.Wait(); err != nil {
		// errgroup will return the first error encountered
		// but we've already printed all errors, so just log this
		fmt.Printf("\nSome verifications failed (first error: %v)\n", err)
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

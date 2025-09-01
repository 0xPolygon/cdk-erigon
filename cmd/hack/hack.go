package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"net/http"
	_ "net/http/pprof" //nolint:gosec
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ledgerwatch/erigon-lib/common/hexutil"
	"github.com/ledgerwatch/erigon/core/types/accounts"
	"github.com/ledgerwatch/erigon/smt/pkg/smt"
	"github.com/ledgerwatch/erigon/smt/pkg/utils"
	"github.com/schollz/progressbar/v3"

	"github.com/ledgerwatch/erigon-lib/kv/dbutils"

	"github.com/RoaringBitmap/roaring/roaring64"
	"github.com/holiman/uint256"
	"github.com/ledgerwatch/log/v3"

	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/hexutility"
	"github.com/ledgerwatch/erigon-lib/common/length"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/kvcfg"
	"github.com/ledgerwatch/erigon-lib/kv/mdbx"
	"github.com/ledgerwatch/erigon-lib/kv/temporal/historyv2"
	"github.com/ledgerwatch/erigon-lib/recsplit"
	"github.com/ledgerwatch/erigon-lib/recsplit/eliasfano32"
	"github.com/ledgerwatch/erigon-lib/seg"
	"golang.org/x/exp/slices"

	"path"

	db2 "github.com/ledgerwatch/erigon/smt/pkg/db"

	hackdb "github.com/ledgerwatch/erigon/cmd/hack/db"
	"github.com/ledgerwatch/erigon/cmd/hack/flow"
	"github.com/ledgerwatch/erigon/cmd/hack/tool"
	"github.com/ledgerwatch/erigon/common"
	"github.com/ledgerwatch/erigon/common/paths"
	"github.com/ledgerwatch/erigon/core"
	"github.com/ledgerwatch/erigon/core/rawdb"
	"github.com/ledgerwatch/erigon/core/rawdb/blockio"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/crypto"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/eth/stagedsync/stages"
	"github.com/ledgerwatch/erigon/ethdb"
	"github.com/ledgerwatch/erigon/ethdb/cbor"
	"github.com/ledgerwatch/erigon/params"
	"github.com/ledgerwatch/erigon/rlp"
	"github.com/ledgerwatch/erigon/turbo/debug"
	"github.com/ledgerwatch/erigon/turbo/services"
	"github.com/ledgerwatch/erigon/turbo/snapshotsync/freezeblocks"
)

var (
	logLevel = flag.String("log-level", "info", "log level (debug, info, warn, error)")

	action     = flag.String("action", "", "action to execute")
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile `file`")
	block      = flag.Int("block", 1, "specifies a block number for operation")
	blockTotal = flag.Int("blocktotal", 1, "specifies a total amount of blocks to process (will offset from head block if <= 0)")
	account    = flag.String("account", "0x", "specifies account to investigate")
	name       = flag.String("name", "", "name to add to the file names")
	chaindata  = flag.String("chaindata", "chaindata", "path to the chaindata database file")
	bucket     = flag.String("bucket", "", "bucket in the database")
	hash       = flag.String("hash", "0x00", "image for preimage or state root for testBlockHashes action")
	output     = flag.String("output", "", "output path")
	input      = flag.String("input", "", "input path")

	// For X Layer, split db
	pathSmtDb       = flag.String("smt-db-path", "smt", "path to the standalone SMT database file")
	standaloneSmtDb = flag.Bool("standalone-smt-db", false, "specifies if the SMT DB is separate from the ChainDB")
	incremental     = flag.Bool("incremental", false, "use incremental  mode")
	ignoreScalable  = flag.Bool("ignore-scalable", false, "ignore scalable account")
	deleteScalable  = flag.Bool("delete-scalable", false, "delete scalable account")
	debugPrint      = flag.Bool("debugPrint", false, "print debug info")

	// For differential smt verification
	preSmtData                = flag.String("pre-smt-data", "", "path to pre smt data")
	preChainData              = flag.String("pre-chain-data", "", "path to pre chain data")
	postSmtData               = flag.String("post-smt-data", "", "path to post smt data")
	preStateSnapshotFilePath  = flag.String("pre-state-snapshot", "", "path to pre-state snapshot file")
	postStateSnapshotFilePath = flag.String("post-state-snapshot", "", "path to post-state file")
	outputStateDiffFilePath   = flag.String("state-diff-output", "", "path to output state diff file")
)

const ZERO_CODE_HASH = "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"

func dbSlice(chaindata string, bucket string, prefix []byte) {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	if err := db.View(context.Background(), func(tx kv.Tx) error {
		c, err := tx.Cursor(bucket)
		if err != nil {
			return err
		}
		for k, v, err := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, v, err = c.Next() {
			if err != nil {
				return err
			}
			fmt.Printf("db.Put([]byte(\"%s\"), common.FromHex(\"%x\"), common.FromHex(\"%x\"))\n", bucket, k, v)
		}
		return nil
	}); err != nil {
		panic(err)
	}
}

// Searches 1000 blocks from the given one to try to find the one with the given state root hash
func testBlockHashes(chaindata string, block int, stateRoot libcommon.Hash) {
	ethDb := mdbx.MustOpen(chaindata)
	defer ethDb.Close()
	br, _ := blocksIO(ethDb)
	tool.Check(ethDb.View(context.Background(), func(tx kv.Tx) error {
		blocksToSearch := 10000000
		for i := uint64(block); i < uint64(block+blocksToSearch); i++ {
			header, err := br.HeaderByNumber(context.Background(), tx, i)
			if err != nil {
				panic(err)
			}
			if header.Root == stateRoot || stateRoot == (libcommon.Hash{}) {
				fmt.Printf("\n===============\nCanonical hash for %d: %x\n", i, hash)
				fmt.Printf("Header.Root: %x\n", header.Root)
				fmt.Printf("Header.TxHash: %x\n", header.TxHash)
				fmt.Printf("Header.UncleHash: %x\n", header.UncleHash)
			}
		}
		return nil
	}))
}

func getCurrentBlockNumber(tx kv.Tx) *uint64 {
	return rawdb.ReadCurrentBlockNumber(tx)
}

func printCurrentBlockNumber(chaindata string) {
	ethDb := mdbx.MustOpen(chaindata)
	defer ethDb.Close()
	ethDb.View(context.Background(), func(tx kv.Tx) error {
		if number := getCurrentBlockNumber(tx); number != nil {
			fmt.Printf("Block number: %d\n", *number)
		} else {
			fmt.Println("Block number: <nil>")
		}
		return nil
	})
}

func blocksIO(db kv.RoDB) (services.FullBlockReader, *blockio.BlockWriter) {
	var histV3 bool
	if err := db.View(context.Background(), func(tx kv.Tx) error {
		histV3, _ = kvcfg.HistoryV3.Enabled(tx)
		return nil
	}); err != nil {
		panic(err)
	}
	br := freezeblocks.NewBlockReader(freezeblocks.NewRoSnapshots(ethconfig.BlocksFreezing{Enabled: false}, "", 0, log.New()), nil /* BorSnapshots */)
	bw := blockio.NewBlockWriter(histV3)
	return br, bw
}

func printTxHashes(chaindata string, block uint64) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	br, _ := blocksIO(db)
	if err := db.View(context.Background(), func(tx kv.Tx) error {
		for b := block; b < block+1; b++ {
			block, _ := br.BlockByNumber(context.Background(), tx, b)
			if block == nil {
				break
			}
			for i, tx := range block.Transactions() {
				fmt.Printf("%d: %x\n", i, tx.Hash())
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func readAccount(chaindata string, account libcommon.Address) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()

	tx, txErr := db.BeginRo(context.Background())
	if txErr != nil {
		return txErr
	}
	defer tx.Rollback()

	a, err := state.NewPlainStateReader(tx).ReadAccountData(account)
	if err != nil {
		return err
	} else if a == nil {
		return fmt.Errorf("acc not found")
	}
	fmt.Printf("CodeHash:%x\nIncarnation:%d\n", a.CodeHash, a.Incarnation)

	c, err := tx.Cursor(kv.PlainState)
	if err != nil {
		return err
	}
	defer c.Close()
	for k, v, e := c.Seek(account.Bytes()); k != nil; k, v, e = c.Next() {
		if e != nil {
			return e
		}
		if !bytes.HasPrefix(k, account.Bytes()) {
			break
		}
		fmt.Printf("%x => %x\n", k, v)
	}
	cc, err := tx.Cursor(kv.PlainContractCode)
	if err != nil {
		return err
	}
	defer cc.Close()
	fmt.Printf("code hashes\n")
	for k, v, e := cc.Seek(account.Bytes()); k != nil; k, v, e = c.Next() {
		if e != nil {
			return e
		}
		if !bytes.HasPrefix(k, account.Bytes()) {
			break
		}
		fmt.Printf("%x => %x\n", k, v)
	}
	return nil
}

func readAccountAtVersion(chaindata string, account string, block uint64) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()

	tx, txErr := db.BeginRo(context.Background())
	if txErr != nil {
		return txErr
	}
	defer tx.Rollback()

	ps := state.NewPlainState(tx, block, nil)
	defer ps.Close()

	addr := libcommon.HexToAddress(account)
	acc, err := ps.ReadAccountData(addr)
	if err != nil {
		return err
	}

	asJson, err := json.Marshal(acc)
	if err != nil {
		return err
	}

	fmt.Printf("account: %s", asJson)

	return nil
}

func nextIncarnation(chaindata string, addrHash libcommon.Hash) {
	ethDb := mdbx.MustOpen(chaindata)
	defer ethDb.Close()
	var found bool
	var incarnationBytes [length.Incarnation]byte
	startkey := make([]byte, length.Hash+length.Incarnation+length.Hash)
	var fixedbits = 8 * length.Hash
	copy(startkey, addrHash[:])
	tool.Check(ethDb.View(context.Background(), func(tx kv.Tx) error {
		c, err := tx.Cursor(kv.HashedStorage)
		if err != nil {
			return err
		}
		defer c.Close()
		return ethdb.Walk(c, startkey, fixedbits, func(k, v []byte) (bool, error) {
			fmt.Printf("Incarnation(z): %d\n", 0)
			copy(incarnationBytes[:], k[length.Hash:])
			found = true
			return false, nil
		})
	}))
	if found {
		fmt.Printf("Incarnation: %d\n", (binary.BigEndian.Uint64(incarnationBytes[:]))+1)
		return
	}
	fmt.Printf("Incarnation(f): %d\n", state.FirstContractIncarnation)
}

func repairCurrent() {
	historyDb := mdbx.MustOpen("/Volumes/tb4/erigon/ropsten/geth/chaindata")
	defer historyDb.Close()
	currentDb := mdbx.MustOpen("statedb")
	defer currentDb.Close()
	tool.Check(historyDb.Update(context.Background(), func(tx kv.RwTx) error {
		return tx.ClearBucket(kv.HashedStorage)
	}))
	tool.Check(historyDb.Update(context.Background(), func(tx kv.RwTx) error {
		newB, err := tx.RwCursor(kv.HashedStorage)
		if err != nil {
			return err
		}
		count := 0
		if err := currentDb.View(context.Background(), func(ctx kv.Tx) error {
			c, err := ctx.Cursor(kv.HashedStorage)
			if err != nil {
				return err
			}
			for k, v, err := c.First(); k != nil; k, v, err = c.Next() {
				if err != nil {
					return err
				}
				tool.Check(newB.Put(k, v))
				count++
				if count == 10000 {
					fmt.Printf("Copied %d storage items\n", count)
				}
			}
			return nil
		}); err != nil {
			return err
		}
		return nil
	}))
}

func dumpStorage() {
	db := mdbx.MustOpen(paths.DefaultDataDir() + "/geth/chaindata")
	defer db.Close()
	if err := db.View(context.Background(), func(tx kv.Tx) error {
		return tx.ForEach(kv.E2StorageHistory, nil, func(k, v []byte) error {
			fmt.Printf("%x %x\n", k, v)
			return nil
		})
	}); err != nil {
		panic(err)
	}
}

func dumpAll(chaindata, output string) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()

	if output == "" {
		// use the chaindata path as a relative path for the datadir dump
		path := filepath.Dir(chaindata)
		path += "-dump"
		output = path
	}

	// check if the dumps folder exists or not
	if _, err := os.Stat(output); os.IsNotExist(err) {
		err := os.Mkdir(output, 0755)
		if err != nil {
			return err
		}
	}

	// For X Layer, split db
	fdumper := func(tx kv.Tx) error {
		buckets, err := tx.ListBuckets()
		if err != nil {
			return err
		}

		for _, buc := range buckets {
			if buc == "HermezSmtLastRoot" { // this is old and deleted table
				continue
			}

			// create a file to dump the contents to
			fileName := buc + ".txt"
			file, err := os.Create(path.Join(output, fileName))
			if err != nil {
				return err
			}
			err = tx.ForEach(buc, nil, func(k, v []byte) error {
				if _, err = file.WriteString(fmt.Sprintf("%x,%x\n", k, v)); err != nil {
					return err
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	}

	// For X Layer, split db
	if *standaloneSmtDb {
		err := db.View(context.Background(), fdumper)
		if err != nil {
			return err
		}
		dbsmt := mdbx.MustOpen(*pathSmtDb)
		defer dbsmt.Close()
		err = dbsmt.View(context.Background(), fdumper)
		if err != nil {
			return err
		}
		return nil
	}
	return db.View(context.Background(), fdumper)
}

func BytesToPaddedHex(data []byte, length int) string {
	hexStr := hex.EncodeToString(data)

	currentLen := len(hexStr)
	zeroCount := length - currentLen
	zeros := ""
	if zeroCount > 0 {
		zeros = fmt.Sprintf("0x%0*s", zeroCount, "")
	} else {
		zeros = "0x"
	}
	return zeros + hexStr
}

func migrateGenesis(chaindata, input, output string) error {
	start := time.Now()
	db := mdbx.MustOpen(chaindata)
	defer db.Close()

	var genesisData map[string]interface{}
	var allocData map[string]interface{}

	if input == "" {
		input = "genesis.json"
	}
	logger.Info("input", "filename: ", input)
	fileData, err := os.ReadFile(input)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Println("Error reading file:", err)
			return err
		}
	} else {
		if err := json.Unmarshal(fileData, &genesisData); err != nil {
			fmt.Println("Error decoding JSON:", err)
			return err
		}
	}

	if _, ok := genesisData["alloc"]; !ok {
		logger.Warn("No alloc field found in genesis stub.")
		allocData = make(map[string]interface{})
	} else {
		allocData = genesisData["alloc"].(map[string]interface{})
	}

	var currentAcct map[string]interface{}
	var currentStorage map[string]interface{}
	var acctCount uint64

	startScanKeys := time.Now()
	if err := db.View(context.Background(), func(tx kv.Tx) error {
		plainStateReader := state.NewPlainStateReader(tx)

		return tx.ForEach(kv.PlainState, nil, func(k, v []byte) error {
			// account
			if len(k) == 20 {
				acctCount++
				acctHex := common.Bytes2Hex(k)
				acctAddress := libcommon.HexToAddress(acctHex)

				// Fixme: if xlayer account balance or nonce conflict with target node(such as op-geth), currently we use op-geth
				if _, exists := allocData[acctHex]; exists {
					logger.Warn("account state conflict found", "account: ", acctHex)
					return nil
				}

				if allocData[acctHex] == nil {
					allocData[acctHex] = make(map[string]interface{})
				}

				switch node := allocData[acctHex].(type) {
				case map[string]interface{}:
					currentAcct = node
				default:
					panic("unhandled json type")
				}
				acctData, err := plainStateReader.ReadAccountData(acctAddress)
				if err != nil {
					return err
				} else if acctData == nil {
					return fmt.Errorf("acc not found")
				}

				currentAcct["nonce"] = "0x" + strconv.FormatUint(acctData.Nonce, 16)
				currentAcct["balance"] = acctData.Balance.Hex()

				//if acctAddress == state.ADDRESS_SCALABLE_L2 {
				//	logger.Debug("SCALABLE contract", "incarnation:", acctData.Incarnation)
				//}

				code, err := tx.GetOne(kv.Code, acctData.CodeHash[:])
				if err != nil {
					return err
				}
				currentAcct["code"] = hexutil.Encode(code)
			}

			// storage
			if len(k) > 28 {

				acctBytes := k[:20]
				acctHex := common.Bytes2Hex(acctBytes)
				//acctAddress := libcommon.HexToAddress(acctHex)
				if allocData[acctHex] == nil {
					allocData[acctHex] = make(map[string]interface{})
				}

				switch node := allocData[acctHex].(type) {
				case map[string]interface{}:
					currentAcct = node
				default:
					panic("unhandled json type")
				}

				if _, exists := currentAcct["storage"]; !exists {
					currentAcct["storage"] = make(map[string]interface{})
				}

				//incarnation := binary.BigEndian.Uint64(k[20:28])

				switch node := currentAcct["storage"].(type) {
				case map[string]interface{}:
					currentStorage = node
				default:
					panic("unhandled json type")
				}

				currentStorage[hexutil.Encode(k[28:])] = BytesToPaddedHex(v, 64)
			}
			return nil
		})
	}); err != nil {
		return err
	}
	elapsedScanKeys := time.Since(startScanKeys).Seconds()
	logger.Info("complete scan keys", "total acct count: ", acctCount, "elapsed in seconds: ", elapsedScanKeys)

	genesisData["alloc"] = allocData

	startJsonWrite := time.Now()
	updatedData, err := json.MarshalIndent(genesisData, "", "  ")
	if err != nil {
		fmt.Println("Error encoding JSON:", err)
		return err
	}

	if output == "" {
		output = "state_dump.json"
	}
	logger.Info("output", "written to", output)

	if err := os.WriteFile(output, updatedData, 0644); err != nil {
		fmt.Println("Error writing to file:", err)
		return err
	}
	elapsedJsonWrite := time.Since(startJsonWrite).Seconds()
	logger.Info("complete json write", "elapsed in seconds: ", elapsedJsonWrite)

	elapsed := time.Since(start).Seconds()
	logger.Info("completed", "total time elapsed", elapsed)
	return nil
}

func printBucket(chaindata, bucket string) {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	f, err := os.Create(fmt.Sprintf("bucket-%s.txt", bucket))
	tool.Check(err)
	defer f.Close()
	fb := bufio.NewWriter(f)
	defer fb.Flush()
	if err := db.View(context.Background(), func(tx kv.Tx) error {
		c, err := tx.Cursor(bucket)
		if err != nil {
			return err
		}
		for k, v, err := c.First(); k != nil; k, v, err = c.Next() {
			if err != nil {
				return err
			}
			fmt.Println(formatBucketKVPair(k, v, bucket))
			fmt.Fprintf(fb, "%s\n", formatBucketKVPair(k, v, bucket))
		}
		return nil
	}); err != nil {
		panic(err)
	}
}

func searchChangeSet(chaindata string, key []byte, block uint64) error {
	fmt.Printf("Searching changesets\n")
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	tx, err1 := db.BeginRw(context.Background())
	if err1 != nil {
		return err1
	}
	defer tx.Rollback()

	if err := historyv2.ForEach(tx, kv.AccountChangeSet, hexutility.EncodeTs(block), func(blockN uint64, k, v []byte) error {
		if bytes.Equal(k, key) {
			fmt.Printf("Found in block %d with value %x\n", blockN, v)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func searchStorageChangeSet(chaindata string, key []byte, block uint64) error {
	fmt.Printf("Searching storage changesets\n")
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	tx, err1 := db.BeginRw(context.Background())
	if err1 != nil {
		return err1
	}
	defer tx.Rollback()
	if err := historyv2.ForEach(tx, kv.StorageChangeSet, hexutility.EncodeTs(block), func(blockN uint64, k, v []byte) error {
		if bytes.Equal(k, key) {
			fmt.Printf("Found in block %d with value %x\n", blockN, v)
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func extractCode(chaindata string) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	var contractCount int
	if err1 := db.View(context.Background(), func(tx kv.Tx) error {
		c, err := tx.Cursor(kv.Code)
		if err != nil {
			return err
		}
		// This is a mapping of CodeHash => Byte code
		for k, v, err := c.First(); k != nil; k, v, err = c.Next() {
			if err != nil {
				return err
			}
			fmt.Printf("%x,%x", k, v)
			contractCount++
		}
		return nil
	}); err1 != nil {
		return err1
	}
	fmt.Fprintf(os.Stderr, "contractCount: %d\n", contractCount)
	return nil
}

func iterateOverCode(chaindata string) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	hashes := make(map[libcommon.Hash][]byte)
	if err1 := db.View(context.Background(), func(tx kv.Tx) error {
		// This is a mapping of CodeHash => Byte code
		if err := tx.ForEach(kv.Code, nil, func(k, v []byte) error {
			if len(v) > 0 && v[0] == 0xef {
				fmt.Printf("Found code with hash %x: %x\n", k, v)
				hashes[libcommon.BytesToHash(k)] = libcommon.CopyBytes(v)
			}
			return nil
		}); err != nil {
			return err
		}
		// This is a mapping of contractAddress + incarnation => CodeHash
		if err := tx.ForEach(kv.PlainContractCode, nil, func(k, v []byte) error {
			hash := libcommon.BytesToHash(v)
			if code, ok := hashes[hash]; ok {
				fmt.Printf("address: %x: %x\n", k[:20], code)
			}
			return nil
		}); err != nil {
			return err
		}
		return nil
	}); err1 != nil {
		return err1
	}
	return nil
}

func getBlockTotal(tx kv.Tx, blockFrom uint64, blockTotalOrOffset int64) uint64 {
	if blockTotalOrOffset > 0 {
		return uint64(blockTotalOrOffset)
	}
	if head := getCurrentBlockNumber(tx); head != nil {
		if blockSub := uint64(-blockTotalOrOffset); blockSub <= *head {
			if blockEnd := *head - blockSub; blockEnd > blockFrom {
				return blockEnd - blockFrom + 1
			}
		}
	}
	return 1
}

func extractHashes(chaindata string, blockStep uint64, blockTotalOrOffset int64, name string) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	br, _ := blocksIO(db)

	f, err := os.Create(fmt.Sprintf("preverified_hashes_%s.go", name))
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	defer w.Flush()

	fmt.Fprintf(w, "package headerdownload\n\n")
	fmt.Fprintf(w, "var %sPreverifiedHashes = []string{\n", name)

	b := uint64(0)
	tool.Check(db.View(context.Background(), func(tx kv.Tx) error {
		blockTotal := getBlockTotal(tx, b, blockTotalOrOffset)
		// Note: blockTotal used here as block number rather than block count
		for b <= blockTotal {
			hash, err := br.CanonicalHash(context.Background(), tx, b)
			if err != nil {
				return err
			}

			if hash == (libcommon.Hash{}) {
				break
			}

			fmt.Fprintf(w, "	\"%x\",\n", hash)
			b += blockStep
		}
		return nil
	}))

	b -= blockStep
	fmt.Fprintf(w, "}\n\n")
	fmt.Fprintf(w, "const %sPreverifiedHeight uint64 = %d\n", name, b)
	fmt.Printf("Last block is %d\n", b)
	return nil
}

func extractHeaders(chaindata string, block uint64, blockTotalOrOffset int64) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	tx, err := db.BeginRo(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback()
	c, err := tx.Cursor(kv.Headers)
	if err != nil {
		return err
	}
	defer c.Close()
	blockEncoded := hexutility.EncodeTs(block)
	blockTotal := getBlockTotal(tx, block, blockTotalOrOffset)
	for k, v, err := c.Seek(blockEncoded); k != nil && blockTotal > 0; k, v, err = c.Next() {
		if err != nil {
			return err
		}
		blockNumber := binary.BigEndian.Uint64(k[:8])
		blockHash := libcommon.BytesToHash(k[8:])
		var header types.Header
		if err = rlp.DecodeBytes(v, &header); err != nil {
			return fmt.Errorf("decoding header from %x: %w", v, err)
		}
		fmt.Printf("Header %d %x: stateRoot %x, parentHash %x, diff %d\n", blockNumber, blockHash, header.Root, header.ParentHash, header.Difficulty)
		blockTotal--
	}
	return nil
}

func extractBodies(datadir string) error {
	snaps := freezeblocks.NewRoSnapshots(ethconfig.BlocksFreezing{
		Enabled:    true,
		KeepBlocks: true,
		Produce:    false,
	}, filepath.Join(datadir, "snapshots"), 0, log.New())
	snaps.ReopenFolder()

	/* method Iterate was removed, need re-implement
	snaps.Bodies.View(func(sns []*snapshotsync.BodySegment) error {
		for _, sn := range sns {
			var firstBlockNum, firstBaseTxNum, firstAmount uint64
			var lastBlockNum, lastBaseTxNum, lastAmount uint64
			var prevBlockNum, prevBaseTxNum, prevAmount uint64
			first := true
			sn.Iterate(func(blockNum uint64, baseTxNum uint64, txAmount uint64) error {
				if first {
					firstBlockNum = blockNum
					firstBaseTxNum = baseTxNum
					firstAmount = txAmount
					first = false
				} else {
					if blockNum != prevBlockNum+1 {
						fmt.Printf("Discount block Num: %d => %d\n", prevBlockNum, blockNum)
					}
					if baseTxNum != prevBaseTxNum+prevAmount {
						fmt.Printf("Wrong baseTxNum: %d+%d => %d\n", prevBaseTxNum, prevAmount, baseTxNum)
					}
				}
				prevBlockNum = blockNum
				lastBlockNum = blockNum
				prevBaseTxNum = baseTxNum
				lastBaseTxNum = baseTxNum
				prevAmount = txAmount
				lastAmount = txAmount
				return nil
			})
			fmt.Printf("Seg: [%d, %d, %d] => [%d, %d, %d]\n", firstBlockNum, firstBaseTxNum, firstAmount, lastBlockNum, lastBaseTxNum, lastAmount)
		}
		return nil
	})
	*/
	db := mdbx.MustOpen(filepath.Join(datadir, "chaindata"))
	defer db.Close()
	br, _ := blocksIO(db)

	tx, err := db.BeginRo(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback()
	c, err := tx.Cursor(kv.BlockBody)
	if err != nil {
		return err
	}
	defer c.Close()
	i := 0
	var txId uint64
	for k, _, err := c.First(); k != nil; k, _, err = c.Next() {
		if err != nil {
			return err
		}
		blockNumber := binary.BigEndian.Uint64(k[:8])
		blockHash := libcommon.BytesToHash(k[8:])
		var hash libcommon.Hash
		if hash, err = br.CanonicalHash(context.Background(), tx, blockNumber); err != nil {
			return err
		}
		_, baseTxId, txAmount := rawdb.ReadBody(tx, blockHash, blockNumber)
		fmt.Printf("Body %d %x: baseTxId %d, txAmount %d\n", blockNumber, blockHash, baseTxId, txAmount)
		if hash != blockHash {
			fmt.Printf("Non-canonical\n")
			continue
		}
		i++
		if txId > 0 {
			if txId != baseTxId {
				fmt.Printf("Mismatch txId for block %d, txId = %d, baseTxId = %d\n", blockNumber, txId, baseTxId)
			}
		}
		txId = baseTxId + uint64(txAmount) + 2
		if i == 50 {
			break
		}
	}
	return nil
}

func snapSizes(chaindata string) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()

	tx, err := db.BeginRo(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback()

	c, _ := tx.Cursor(kv.CliqueSeparate)
	defer c.Close()

	sizes := make(map[int]int)
	differentValues := make(map[string]struct{})

	var (
		total uint64
		k, v  []byte
	)

	for k, v, err = c.First(); k != nil; k, v, err = c.Next() {
		if err != nil {
			return err
		}
		sizes[len(v)]++
		differentValues[string(v)] = struct{}{}
		total += uint64(len(v) + len(k))
	}

	var lens = make([]int, len(sizes))

	i := 0
	for l := range sizes {
		lens[i] = l
		i++
	}
	slices.Sort(lens)

	for _, l := range lens {
		fmt.Printf("%6d - %d\n", l, sizes[l])
	}

	fmt.Printf("Different keys %d\n", len(differentValues))
	fmt.Printf("Total size: %d bytes\n", total)

	return nil
}

func readCallTraces(chaindata string, block uint64) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	tx, err := db.BeginRw(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback()
	traceCursor, err1 := tx.RwCursorDupSort(kv.CallTraceSet)
	if err1 != nil {
		return err1
	}
	defer traceCursor.Close()
	var k []byte
	var v []byte
	count := 0
	for k, v, err = traceCursor.First(); k != nil; k, v, err = traceCursor.Next() {
		if err != nil {
			return err
		}
		blockNum := binary.BigEndian.Uint64(k)
		if blockNum == block {
			fmt.Printf("%x\n", v)
		}
		count++
	}
	fmt.Printf("Found %d records\n", count)
	idxCursor, err2 := tx.Cursor(kv.CallToIndex)
	if err2 != nil {
		return err2
	}
	var acc = libcommon.HexToAddress("0x511bc4556d823ae99630ae8de28b9b80df90ea2e")
	for k, v, err = idxCursor.Seek(acc[:]); k != nil && err == nil && bytes.HasPrefix(k, acc[:]); k, v, err = idxCursor.Next() {
		bm := roaring64.New()
		_, err = bm.ReadFrom(bytes.NewReader(v))
		if err != nil {
			return err
		}
		//fmt.Printf("%x: %d\n", k, bm.ToArray())
	}
	if err != nil {
		return err
	}
	return nil
}

func fixTd(chaindata string) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	tx, err := db.BeginRw(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback()
	c, err1 := tx.RwCursor(kv.Headers)
	if err1 != nil {
		return err1
	}
	defer c.Close()
	var k, v []byte
	for k, v, err = c.First(); err == nil && k != nil; k, v, err = c.Next() {
		hv, herr := tx.GetOne(kv.HeaderTD, k)
		if herr != nil {
			return herr
		}
		if hv == nil {
			fmt.Printf("Missing TD record for %x, fixing\n", k)
			var header types.Header
			if err = rlp.DecodeBytes(v, &header); err != nil {
				return fmt.Errorf("decoding header from %x: %w", v, err)
			}
			if header.Number.Uint64() == 0 {
				continue
			}
			var parentK [40]byte
			binary.BigEndian.PutUint64(parentK[:], header.Number.Uint64()-1)
			copy(parentK[8:], header.ParentHash[:])
			var parentTdRec []byte
			if parentTdRec, err = tx.GetOne(kv.HeaderTD, parentK[:]); err != nil {
				return fmt.Errorf("reading parentTd Rec for %d: %w", header.Number.Uint64(), err)
			}
			var parentTd big.Int
			if err = rlp.DecodeBytes(parentTdRec, &parentTd); err != nil {
				return fmt.Errorf("decoding parent Td record for block %d, from %x: %w", header.Number.Uint64(), parentTdRec, err)
			}
			var td big.Int
			td.Add(&parentTd, header.Difficulty)
			var newHv []byte
			if newHv, err = rlp.EncodeToBytes(&td); err != nil {
				return fmt.Errorf("encoding td record for block %d: %w", header.Number.Uint64(), err)
			}
			if err = tx.Put(kv.HeaderTD, k, newHv); err != nil {
				return err
			}
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func advanceExec(chaindata string) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	tx, err := db.BeginRw(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stageExec, err := stages.GetStageProgress(tx, stages.Execution)
	if err != nil {
		return err
	}
	log.Info("ID exec", "progress", stageExec)
	if err = stages.SaveStageProgress(tx, stages.Execution, stageExec+1); err != nil {
		return err
	}
	stageExec, err = stages.GetStageProgress(tx, stages.Execution)
	if err != nil {
		return err
	}
	log.Info("ID exec", "changed to", stageExec)
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func backExec(chaindata string) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	tx, err := db.BeginRw(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stageExec, err := stages.GetStageProgress(tx, stages.Execution)
	if err != nil {
		return err
	}
	log.Info("ID exec", "progress", stageExec)
	if err = stages.SaveStageProgress(tx, stages.Execution, stageExec-1); err != nil {
		return err
	}
	stageExec, err = stages.GetStageProgress(tx, stages.Execution)
	if err != nil {
		return err
	}
	log.Info("ID exec", "changed to", stageExec)
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func fixState(chaindata string) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	tx, err := db.BeginRw(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback()
	c, err1 := tx.RwCursor(kv.HeaderCanonical)
	if err1 != nil {
		return err1
	}
	defer c.Close()
	var prevHeaderKey [40]byte
	var k, v []byte
	for k, v, err = c.First(); err == nil && k != nil; k, v, err = c.Next() {
		var headerKey [40]byte
		copy(headerKey[:], k)
		copy(headerKey[8:], v)
		hv, herr := tx.GetOne(kv.Headers, headerKey[:])
		if herr != nil {
			return herr
		}
		if hv == nil {
			return fmt.Errorf("missing header record for %x", headerKey)
		}
		var header types.Header
		if err = rlp.DecodeBytes(hv, &header); err != nil {
			return fmt.Errorf("decoding header from %x: %w", v, err)
		}
		if header.Number.Uint64() > 1 {
			var parentK [40]byte
			binary.BigEndian.PutUint64(parentK[:], header.Number.Uint64()-1)
			copy(parentK[8:], header.ParentHash[:])
			if !bytes.Equal(parentK[:], prevHeaderKey[:]) {
				fmt.Printf("broken ancestry from %d %x (parent hash %x): prevKey %x\n", header.Number.Uint64(), v, header.ParentHash, prevHeaderKey)
			}
		}
		copy(prevHeaderKey[:], headerKey[:])
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func trimTxs(chaindata string) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	tx, err := db.BeginRw(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback()
	lastTxId, err := tx.ReadSequence(kv.EthTx)
	if err != nil {
		return err
	}
	txs, err1 := tx.RwCursor(kv.EthTx)
	if err1 != nil {
		return err1
	}
	defer txs.Close()
	bodies, err2 := tx.Cursor(kv.BlockBody)
	if err2 != nil {
		return err
	}
	defer bodies.Close()
	toDelete := roaring64.New()
	toDelete.AddRange(0, lastTxId)
	// Exclude transaction that are used, from the range
	for k, v, err := bodies.First(); k != nil; k, v, err = bodies.Next() {
		if err != nil {
			return err
		}
		var body types.BodyForStorage
		if err = rlp.DecodeBytes(v, &body); err != nil {
			return err
		}
		// Remove from the map
		toDelete.RemoveRange(body.BaseTxId, body.BaseTxId+uint64(body.TxAmount))
	}
	fmt.Printf("Number of tx records to delete: %d\n", toDelete.GetCardinality())
	// Takes 20min to iterate 1.4b
	toDelete2 := roaring64.New()
	var iterated int
	for k, _, err := txs.First(); k != nil; k, _, err = txs.Next() {
		if err != nil {
			return err
		}
		toDelete2.Add(binary.BigEndian.Uint64(k))
		iterated++
		if iterated%100_000_000 == 0 {
			fmt.Printf("Iterated %d\n", iterated)
		}
	}
	fmt.Printf("Number of tx records: %d\n", toDelete2.GetCardinality())
	toDelete.And(toDelete2)
	fmt.Printf("Number of tx records to delete: %d\n", toDelete.GetCardinality())
	fmt.Printf("Roaring size: %d\n", toDelete.GetSizeInBytes())

	iter := toDelete.Iterator()
	for {
		var deleted int
		for iter.HasNext() {
			txId := iter.Next()
			var key [8]byte
			binary.BigEndian.PutUint64(key[:], txId)
			if err = txs.Delete(key[:]); err != nil {
				return err
			}
			deleted++
			if deleted >= 10_000_000 {
				break
			}
		}
		if deleted == 0 {
			fmt.Printf("Nothing more to delete\n")
			break
		}
		fmt.Printf("Committing after deleting %d records\n", deleted)
		if err = tx.Commit(); err != nil {
			return err
		}
		txs.Close()
		tx, err = db.BeginRw(context.Background())
		if err != nil {
			return err
		}
		defer tx.Rollback()
		txs, err = tx.RwCursor(kv.EthTx)
		if err != nil {
			return err
		}
		defer txs.Close()
	}
	return nil
}

func scanTxs(chaindata string) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	tx, err := db.BeginRo(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback()
	c, err := tx.Cursor(kv.EthTx)
	if err != nil {
		return err
	}
	defer c.Close()
	trTypes := make(map[byte]int)
	trTypesAl := make(map[byte]int)
	for k, v, err := c.First(); k != nil; k, v, err = c.Next() {
		if err != nil {
			return err
		}
		var tr types.Transaction
		if tr, err = types.DecodeTransaction(v); err != nil {
			return err
		}
		if _, ok := trTypes[tr.Type()]; !ok {
			fmt.Printf("Example for type %d:\n%x\n", tr.Type(), v)
		}
		trTypes[tr.Type()]++
		if tr.GetAccessList().StorageKeys() > 0 {
			if _, ok := trTypesAl[tr.Type()]; !ok {
				fmt.Printf("Example for type %d with AL:\n%x\n", tr.Type(), v)
			}
			trTypesAl[tr.Type()]++
		}
	}
	fmt.Printf("Transaction types: %v\n", trTypes)
	return nil
}

func scanReceipts3(chaindata string, block uint64) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	tx, err := db.BeginRw(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var key [8]byte
	var v []byte
	binary.BigEndian.PutUint64(key[:], block)
	if v, err = tx.GetOne(kv.Receipts, key[:]); err != nil {
		return err
	}
	fmt.Printf("%x\n", v)
	return nil
}

func scanReceipts2(chaindata string) error {
	f, err := os.Create("receipts.txt")
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	dbdb := mdbx.MustOpen(chaindata)
	defer dbdb.Close()
	tx, err := dbdb.BeginRw(context.Background())
	if err != nil {
		return err
	}
	br, _ := blocksIO(dbdb)

	defer tx.Rollback()
	blockNum, err := historyv2.AvailableFrom(tx)
	if err != nil {
		return err
	}
	fixedCount := 0
	logEvery := time.NewTicker(20 * time.Second)
	defer logEvery.Stop()
	var key [8]byte
	var v []byte
	for ; true; blockNum++ {
		select {
		default:
		case <-logEvery.C:
			log.Info("Scanned", "block", blockNum, "fixed", fixedCount)
		}
		var hash libcommon.Hash
		if hash, err = br.CanonicalHash(context.Background(), tx, blockNum); err != nil {
			return err
		}
		if hash == (libcommon.Hash{}) {
			break
		}
		binary.BigEndian.PutUint64(key[:], blockNum)
		if v, err = tx.GetOne(kv.Receipts, key[:]); err != nil {
			return err
		}
		var receipts types.Receipts
		if err = cbor.Unmarshal(&receipts, bytes.NewReader(v)); err == nil {
			broken := false
			for _, receipt := range receipts {
				if receipt.CumulativeGasUsed < 10000 {
					broken = true
					break
				}
			}
			if !broken {
				continue
			}
		}
		fmt.Fprintf(w, "%d %x\n", blockNum, v)
		fixedCount++
		if fixedCount > 100 {
			break
		}

	}
	tx.Rollback()
	return nil
}

func devTx(chaindata string) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	tx, err := db.BeginRo(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cc := tool.ChainConfig(tx)
	txn := types.NewTransaction(2, libcommon.Address{}, uint256.NewInt(100), 100_000, uint256.NewInt(1), []byte{1})
	signedTx, err := types.SignTx(txn, *types.LatestSigner(cc), core.DevnetSignPrivateKey)
	tool.Check(err)
	buf := bytes.NewBuffer(nil)
	err = signedTx.MarshalBinary(buf)
	tool.Check(err)
	fmt.Printf("%x\n", buf.Bytes())
	return nil
}

func chainConfig(name string) error {
	chainConfig := params.ChainConfigByChainName(name)
	if chainConfig == nil {
		return fmt.Errorf("unknown name: %s", name)
	}
	f, err := os.Create(filepath.Join("params", "chainspecs", fmt.Sprintf("%s.json", name)))
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(chainConfig); err != nil {
		return err
	}
	if err = w.Flush(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return nil
}

func keybytesToHex(str []byte) []byte {
	l := len(str)*2 + 1
	var nibbles = make([]byte, l)
	for i, b := range str {
		nibbles[i*2] = b / 16
		nibbles[i*2+1] = b % 16
	}
	nibbles[l-1] = 16
	return nibbles
}

func findPrefix(chaindata string) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()

	tx, txErr := db.BeginRo(context.Background())
	if txErr != nil {
		return txErr
	}
	defer tx.Rollback()

	c, err := tx.Cursor(kv.PlainState)
	if err != nil {
		return err
	}
	defer c.Close()
	var k []byte
	var e error
	prefix := common.FromHex("0x0901050b0c03")
	count := 0
	for k, _, e = c.First(); k != nil && e == nil; k, _, e = c.Next() {
		if len(k) != 20 {
			continue
		}
		hash := crypto.Keccak256(k)
		nibbles := keybytesToHex(hash)
		if bytes.HasPrefix(nibbles, prefix) {
			fmt.Printf("addr = [%x], hash = [%x]\n", k, hash)
			break
		}
		count++
		if count%1_000_000 == 0 {
			fmt.Printf("Searched %d records\n", count)
		}
	}
	if e != nil {
		return e
	}
	return nil
}

func rmSnKey(chaindata string) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	return db.Update(context.Background(), func(tx kv.RwTx) error {
		_ = tx.Delete(kv.DatabaseInfo, rawdb.SnapshotsKey)
		_ = tx.Delete(kv.DatabaseInfo, rawdb.SnapshotsHistoryKey)
		return nil
	})
}

func findLogs(chaindata string, block uint64, blockTotal uint64) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()

	tx, txErr := db.BeginRo(context.Background())
	if txErr != nil {
		return txErr
	}
	defer tx.Rollback()
	logs, err := tx.Cursor(kv.Log)
	if err != nil {
		return err
	}
	defer logs.Close()

	reader := bytes.NewReader(nil)
	addrs := map[libcommon.Address]int{}
	topics := map[string]int{}

	for k, v, err := logs.Seek(dbutils.LogKey(block, 0)); k != nil; k, v, err = logs.Next() {
		if err != nil {
			return err
		}

		blockNum := binary.BigEndian.Uint64(k[:8])
		if blockNum >= block+blockTotal {
			break
		}

		var ll types.Logs
		reader.Reset(v)
		if err := cbor.Unmarshal(&ll, reader); err != nil {
			return fmt.Errorf("receipt unmarshal failed: %w, blocl=%d", err, blockNum)
		}

		for _, l := range ll {
			addrs[l.Address]++
			for _, topic := range l.Topics {
				topics[fmt.Sprintf("%x | %x", l.Address, topic)]++
			}
		}
	}
	addrsInv := map[int][]libcommon.Address{}
	topicsInv := map[int][]string{}
	for a, c := range addrs {
		addrsInv[c] = append(addrsInv[c], a)
	}
	counts := make([]int, 0, len(addrsInv))
	for c := range addrsInv {
		counts = append(counts, -c)
	}
	sort.Ints(counts)
	for i := 0; i < 10 && i < len(counts); i++ {
		as := addrsInv[-counts[i]]
		fmt.Printf("%d=%x\n", -counts[i], as)
	}
	for t, c := range topics {
		topicsInv[c] = append(topicsInv[c], t)
	}
	counts = make([]int, 0, len(topicsInv))
	for c := range topicsInv {
		counts = append(counts, -c)
	}
	sort.Ints(counts)
	for i := 0; i < 10 && i < len(counts); i++ {
		as := topicsInv[-counts[i]]
		fmt.Printf("%d=%s\n", -counts[i], as)
	}
	return nil
}

func iterate(filename string, prefix string) error {
	pBytes := common.FromHex(prefix)
	efFilename := filename + ".ef"
	viFilename := filename + ".vi"
	vFilename := filename + ".v"
	efDecomp, err := seg.NewDecompressor(efFilename)
	if err != nil {
		return err
	}
	defer efDecomp.Close()
	viIndex, err := recsplit.OpenIndex(viFilename)
	if err != nil {
		return err
	}
	defer viIndex.Close()
	r := recsplit.NewIndexReader(viIndex)
	vDecomp, err := seg.NewDecompressor(vFilename)
	if err != nil {
		return err
	}
	defer vDecomp.Close()
	gv := vDecomp.MakeGetter()
	g := efDecomp.MakeGetter()
	for g.HasNext() {
		key, _ := g.NextUncompressed()
		if bytes.HasPrefix(key, pBytes) {
			val, _ := g.NextUncompressed()
			ef, _ := eliasfano32.ReadEliasFano(val)
			efIt := ef.Iterator()
			fmt.Printf("[%x] =>", key)
			cnt := 0
			for efIt.HasNext() {
				txNum, _ := efIt.Next()
				var txKey [8]byte
				binary.BigEndian.PutUint64(txKey[:], txNum)
				offset, ok := r.Lookup2(txKey[:], key)
				if !ok {
					continue
				}
				gv.Reset(offset)
				v, _ := gv.Next(nil)
				fmt.Printf(" %d", txNum)
				if len(v) == 0 {
					fmt.Printf("*")
				}
				cnt++
				if cnt == 16 {
					fmt.Printf("\n")
					cnt = 0
				}
			}
			fmt.Printf("\n")
		} else {
			g.SkipUncompressed()
		}
	}
	return nil
}

func readSeg(chaindata string) error {
	vDecomp, err := seg.NewDecompressor(chaindata)
	if err != nil {
		return err
	}
	defer vDecomp.Close()
	g := vDecomp.MakeGetter()
	var buf []byte
	var count int
	var offset, nextPos uint64
	for g.HasNext() {
		buf, nextPos = g.Next(buf[:0])
		fmt.Printf("offset: %d, val: %x\n", offset, buf)
		offset = nextPos
		count++
	}
	return nil
}

func dumpState(chaindata string) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()

	if err := db.View(context.Background(), func(tx kv.Tx) error {
		return tx.ForEach(kv.PlainState, nil, func(k, v []byte) error {
			fmt.Printf("%x %x\n", k, v)
			return nil
		})
	}); err != nil {
		return err
	}

	return nil
}

type AccInfo struct {
	Balance string            `json:"balance"`
	Nonce   string            `json:"nonce"`
	Code    string            `json:"code"`
	Storage map[string]string `json:"storage"`
}

// const TableSmt = "HermezSmt"
// const TableStats = "HermezSmtStats"
// const TableAccountValues = "HermezSmtAccountValues"
// const TableMetadata = "HermezSmtMetadata"
// const TableHashKey = "HermezSmtHashKey"
func createSMTTables(db kv.RwDB, tx kv.RwTx) error {
	// List of SMT-related buckets that need to be created
	buckets := []string{
		db2.TableSmt,      // Main SMT table
		db2.TableMetadata, // SMT metadata
		db2.TableStats,    // SMT statistics
		//"HermezSmtNodes",    // SMT nodes
		//"HermezSmtRoots",    // SMT roots
		//"HermezSmtCode",     // Contract code
		//"HermezSmtStorage",  // Contract storage
		//"HermezSmtAccounts", // Account information
		//"HermezSmtNonces",   // Account nonces
		//"HermezSmtBalances", // Account balances
		db2.TableHashKey,
		// Add other buckets as needed
	}

	for _, bucketName := range buckets {
		if err := tx.CreateBucket(bucketName); err != nil {
			return fmt.Errorf("failed to create bucket %s: %w", bucketName, err)
		}
		fmt.Printf("Created bucket: %s\n", bucketName)
	}

	return nil
}

func checkStateRoot(chaindata, smtdata, input string, incremental, debug bool) error {
	if *deleteScalable && *ignoreScalable {
		return fmt.Errorf("you cannot use --delete-scalable=true and --ignore-scalable=true flags together")
	}

	var jsonData map[string]map[string]AccInfo
	if input == "" {
		input = "genesis.json"
	}
	fmt.Printf("input: %s\n", input)
	fileData, err := os.ReadFile(input)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Println("Error reading file:", err)
			return err
		}
	} else {
		if err := json.Unmarshal(fileData, &jsonData); err != nil {
			fmt.Println("Error decoding JSON:", err)
			return err
		}
	}

	ctx := context.Background()
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	tx, err := db.BeginRw(ctx)
	if err != nil {
		panic(err)
	}
	var txsmt kv.RwTx = nil
	if smtdata != "" {
		fmt.Printf("Using split DB: %s\n", smtdata)
		dbsmt := mdbx.MustOpen(*pathSmtDb)
		defer dbsmt.Close()
		txsmt, err = dbsmt.BeginRw(ctx)
		if err != nil {
			panic(err)
		}
	}
	eridb := db2.NewEriDb(txsmt, tx)
	smtOrigin := smt.NewSMT(eridb, false)

	accChanges := make(map[libcommon.Address]*accounts.Account)
	codeChanges := make(map[libcommon.Address]string)
	storageChanges := make(map[libcommon.Address]map[string]string)
	fmt.Println("Begin json decode")
	for acc, value := range jsonData["alloc"] {
		accBytes := common.FromHex(acc)
		if err != nil {
			panic("acc decoding error")
		}
		address := libcommon.BytesToAddress(accBytes)
		acc := accounts.NewAccount()
		balance, err := uint256.FromHex(value.Balance)
		if err != nil {
			panic(fmt.Sprintf("acc decoding error for acct: %s, err: %v", address, err))
		}
		acc.Balance = *balance
		nonce, err := hexutil.DecodeUint64(value.Nonce)
		if err != nil {
			panic("nonce decoding error")
		}
		acc.Nonce = nonce
		accChanges[address] = &acc

		if value.Code != "0x" {
			codeChanges[address] = value.Code
		}
		if *ignoreScalable && address == state.ADDRESS_SCALABLE_L2 {
			fmt.Printf("Ignoring scalable address: %s\n", address.String())

			if value.Storage != nil {
				storageChanges[address] = make(map[string]string)
				fmt.Printf("number of Storage items for account %s: %d\n", address.Hex(), len(value.Storage))
				for k, _ := range value.Storage {
					keyHash := libcommon.HexToHash(k)
					valInSmt, err := smtOrigin.ReadAccountStorage(address, 0, &keyHash)
					if err != nil {
						fmt.Printf("Error reading scalable account storage: %s\n", err)
						return err
					}
					valInSmtHex := hexutility.Encode(common.LeftPadBytes(valInSmt, 32))
					storageChanges[address][k] = valInSmtHex
					//fmt.Printf("key: %s, valInSmt: %s, valInGenesise: %s \n", k, valInSmtHex, v)
				}
			}
			fmt.Printf("Finish override scalable storages with original storage\n")
			continue
		}
		if value.Storage != nil {
			storageChanges[address] = make(map[string]string)
			/// Fixme: use maps.Clone, which is more efficient
			for k, v := range value.Storage {
				storageChanges[address][k] = v
			}
		}
	}
	fmt.Println("End json decode")

	if debug {
		for acc, acc_info := range accChanges {
			fmt.Printf("addr: %s, balance: %s, nonce: %d \n", acc.String(), acc_info.Balance.String(), acc_info.Nonce)
		}

		for acc, code := range codeChanges {
			fmt.Printf("addr: %s, code %s \n", acc.String(), code)
		}

		for acc, st := range storageChanges {
			for k, v := range st {
				fmt.Printf("addr: %s, key : %s, val: %s \n", acc.String(), k, v)
			}
		}
	}

	fmt.Printf("Number of accounts: %d\n", len(accChanges))
	fmt.Printf("Number of code: %d\n", len(codeChanges))
	fmt.Printf("Number of storage: %d\n", len(storageChanges))
	fmt.Printf("Total number of keys: %d\n", len(accChanges)+len(codeChanges)+len(storageChanges))

	if *deleteScalable {
		fmt.Println("Deleting scalable address storage ...")
		smtBatchRootHashOrigin, _ := smtOrigin.Db.GetLastRoot()
		fmt.Printf("*** (before delete) smtBatchRootHashOrigin: %x\n", smtBatchRootHashOrigin)
		ethAddr := libcommon.HexToAddress("0x000000000000000000000000000000005ca1ab1e")
		ethAddrBigInt := utils.ConvertHexToBigInt(ethAddr.String())
		ethAddrBigIngArray := utils.ScalarToArrayBig(ethAddrBigInt)
		for k := range storageChanges[ethAddr] {
			fmt.Printf("Deleting scalable address storage key: %s\n", k)
			keyStoragePosition := utils.KeyContractStorage(ethAddrBigIngArray, k)
			if err = smtOrigin.DeleteKeySource(&keyStoragePosition); err != nil {
				panic("DeleteKeySource: " + err.Error())
			}
		}
		//_, _, err := smtOrigin.SetStorage(ctx, "", accChanges, codeChanges, storageChanges)
		//if err != nil {
		//	panic("SetStorage: " + err.Error())
		//}
		fmt.Println("Done deleting scalable address.")
	}
	smtBatchRootHashOrigin := smtOrigin.LastRoot()
	fmt.Printf("*** smtBatchRootHashOrigin: %x\n", smtBatchRootHashOrigin)

	if incremental {
		fmt.Println("Begin incremental SMT buidling...")

		smtIncremental := smt.NewSMT(nil, false)

		/*
			mdb, err := newMDBX("tmp", ctx)
			if err != nil {
				panic(fmt.Sprintf("Failed to open MDBX: %v", err))
			}
			defer mdb.Close()
			txn, err := mdb.BeginRw(ctx)
			if err != nil {
				panic(err)
			}
			defer txn.Rollback()
			smtIncremental := smt.NewSMT(db2.NewEriDb(txn, tx), false)
		*/

		fmt.Println("Begin SetAccountStorage")
		bar := progressbar.NewOptions(len(accChanges), progressbar.OptionSetPredictTime(true))
		for addr, acc := range accChanges {
			if err := smtIncremental.SetAccountStorage(addr, acc); err != nil {
				panic("SetAccountStorage")
			}
			bar.Add(1)
		}
		bar.Finish()
		fmt.Println()

		fmt.Println("Begin SetContractBytecode")
		bar = progressbar.NewOptions(len(codeChanges), progressbar.OptionSetPredictTime(true))
		for addr, code := range codeChanges {
			if err := smtIncremental.SetContractBytecode(addr.String(), code); err != nil {
				panic("SetContractBytecode")
			}
			bar.Add(1)
		}
		bar.Finish()
		fmt.Println()

		fmt.Println("Begin SetContractStorage")
		totalStorage := 0
		for _, storage := range storageChanges {
			totalStorage += len(storage)
		}
		bar = progressbar.NewOptions(totalStorage, progressbar.OptionSetPredictTime(true))
		for addr, storage := range storageChanges {
			if _, err := smtIncremental.SetContractStorage(addr.String(), storage, nil); err != nil {
				panic("SetContractStorage")
			}
			bar.Add(len(storage))
		}
		bar.Finish()
		fmt.Println()

		smtIncrementalRootHash, _ := smtIncremental.Db.GetLastRoot()
		fmt.Printf("*** smtIncrementalRootHash: %x\n", smtIncrementalRootHash)
		if smtIncrementalRootHash.Text(16) == smtBatchRootHashOrigin.Text(16) {
			fmt.Println("Incremental check: Pass")
		} else {
			fmt.Println("Incremental check: Failed")
		}

		fmt.Println("Done incremental SMT buidling.")
	} else {
		start := time.Now() // record start time
		dbRebuild := mdbx.MustOpen("./chaindata_rebuild")
		defer dbRebuild.Close()
		txRebuild, err := dbRebuild.BeginRw(ctx)
		if err != nil {
			panic(err)
		}

		dbsmtRebuild := mdbx.MustOpenInMem(4)
		defer dbsmtRebuild.Close()
		var txsmtRebuild kv.RwTx = nil
		txsmtRebuild, err = dbsmtRebuild.BeginRw(ctx)
		if err != nil {
			panic(err)
		}

		//kv.InitStandaloneSMT(true)
		// Create the SMT buckets in the new database
		if err := createSMTTables(dbsmtRebuild, txsmtRebuild); err != nil {
			panic("Failed to create SMT tables: " + err.Error())
		}

		eridbRebuild := db2.NewEriDb(txsmtRebuild, txRebuild)
		smtBatchRebuild := smt.NewSMT(eridbRebuild, false)
		fmt.Println("Begin set storage of rebuilt smt")
		_, _, err = smtBatchRebuild.SetStorage(ctx, "", accChanges, codeChanges, storageChanges)
		if err != nil {
			fmt.Println("SetStorage error ", err)
			panic("SetStorage: " + err.Error())
		}
		fmt.Println("before check root")
		smtBatchRebuildRootHash, _ := smtBatchRebuild.Db.GetLastRoot()
		fmt.Printf("*** smtBatchRebuildRootHash: %x\n", smtBatchRebuildRootHash)
		if smtBatchRebuildRootHash.Text(16) == smtBatchRootHashOrigin.Text(16) {
			fmt.Println("batch check: Pass")
		} else {
			fmt.Println("batch check: Failed")
		}

		fmt.Println("Done batch SMT buidling.")
		elapsed := time.Since(start).Minutes() // compute elapsed duration
		fmt.Printf("Elapsed time: %.3f minutes\n", elapsed)

	}

	tx.Rollback()
	if txsmt != nil {
		txsmt.Rollback()
	}

	return nil
}

func checkStateRootFast(chaindata, smtdata, input string) error {

	var smtBatchRootHashOrigin *big.Int
	if chaindata != "" {
		ctx := context.Background()
		db := mdbx.MustOpen(chaindata)
		defer db.Close()
		tx, err := db.BeginRw(ctx)
		if err != nil {
			panic(err)
		}
		defer tx.Rollback()
		var txsmt kv.RwTx = nil
		if smtdata != "" {
			fmt.Printf("Using split DB: %s\n", smtdata)
			dbsmt := mdbx.MustOpen(*pathSmtDb)
			defer dbsmt.Close()
			txsmt, err = dbsmt.BeginRw(ctx)
			if err != nil {
				panic(err)
			}
			defer txsmt.Rollback()
		}
		eridb := db2.NewEriDb(txsmt, tx)
		smtOrigin := smt.NewSMT(eridb, false)
		smtBatchRootHashOrigin = smtOrigin.LastRoot()
		fmt.Printf("*** smtBatchRootHashOrigin: %x\n", smtBatchRootHashOrigin)
	}

	smtBatchRootHashRebuild, err := calcSmtRoot(input)
	if err != nil {
		return err
	}
	fmt.Printf("*** smtBatchRootHashRebuild: %x\n", smtBatchRootHashRebuild)

	if smtBatchRootHashOrigin != nil {
		if smtBatchRootHashOrigin.Text(16) == smtBatchRootHashRebuild.Text(16) {
			fmt.Println("Batch check: Pass")
		} else {
			fmt.Println("Batch check: Failed")
		}
	}
	return nil
}

func TinyScalarToArrayUint64(scalar uint64) [8]uint64 {
	var result [8]uint64

	result[0] = scalar & 0xFFFFFFFF
	result[1] = (scalar >> 32) & 0xFFFFFFFF

	return result
}

type NodeKV struct {
	Key       utils.NodeKey
	Value     [8]uint64
	leafHash  [4]uint64
	level     int // [0, 255]
	path      []int
	shortPath [4]uint64 // Store 256 bits using 4 uint64s
}

func calculateLevels(nodeKvs []*NodeKV, startLevel int) {
	if len(nodeKvs) <= 1 || startLevel > 255 {
		return
	}

	// Find the position that split the tree into two subtrees.
	splitIndex := sort.Search(len(nodeKvs), func(i int) bool {
		return nodeKvs[i].Key.GetPath()[startLevel] == 1
	})

	if splitIndex == 0 {
		// All nodes belong to the right subtree
		calculateLevels(nodeKvs, startLevel+1)
	} else if splitIndex == len(nodeKvs) {
		// All nodes belong to the left subtree
		calculateLevels(nodeKvs, startLevel+1)
	} else {
		// Both left subtree and right subtree have node(s)
		for i := range nodeKvs {
			nodeKvs[i].level = startLevel + 1
		}

		// Enable concurrent processing only when data size is large enough
		if len(nodeKvs) > 10000 {
			var wg sync.WaitGroup
			wg.Add(2)

			// Process left subtree concurrently
			go func() {
				defer wg.Done()
				calculateLevels(nodeKvs[:splitIndex], startLevel+1)
			}()

			// Process right subtree concurrently
			go func() {
				defer wg.Done()
				calculateLevels(nodeKvs[splitIndex:], startLevel+1)
			}()

			wg.Wait()
		} else {
			// Process sequentially when data size is small
			calculateLevels(nodeKvs[:splitIndex], startLevel+1)
			calculateLevels(nodeKvs[splitIndex:], startLevel+1)
		}
	}
}

func KeyContractStorageHack(ethaddr [8]uint64, storageKey string) utils.NodeKey {
	storageKeyBig := utils.ConvertHexToBigInt(storageKey)
	storageKeyArr := utils.ScalarToArrayUint64(storageKeyBig)
	hk0 := utils.HashByPointers(&storageKeyArr, &utils.BranchCapacity)
	var key1 = [8]uint64{ethaddr[0], ethaddr[1], ethaddr[2], ethaddr[3], ethaddr[4], ethaddr[5], uint64(utils.SC_STORAGE), uint64(0)}
	return *utils.HashByPointers(&key1, hk0)
}

func calcSmtRoot(input string) (*big.Int, error) {
	var jsonData map[string]map[string]AccInfo
	if input == "" {
		input = "genesis.json"
	}
	fmt.Printf("input: %s\n", input)

	start0 := time.Now()
	fileData, err := os.ReadFile(input)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Println("Error reading file:", err)
			return nil, err
		}
	} else {
		if err := json.Unmarshal(fileData, &jsonData); err != nil {
			fmt.Println("Error decoding JSON:", err)
			return nil, err
		}
	}
	fmt.Println("read json elapsed:", time.Since(start0))

	alloc := jsonData["alloc"]

	nodeKvs := make([]*NodeKV, 0)
	start1 := time.Now()

	// Get all addresses and process them in chunks
	addrs := make([]string, 0, len(alloc))
	for addr := range alloc {
		addrs = append(addrs, addr)
	}

	numWorkers := runtime.NumCPU()
	fmt.Println("numWorkers:", numWorkers)
	chunkSize := (len(addrs) + numWorkers - 1) / numWorkers
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < len(addrs); i += chunkSize {
		end := i + chunkSize
		if end > len(addrs) {
			end = len(addrs)
		}

		wg.Add(1)
		go func(addrSlice []string) {
			defer wg.Done()
			localKvs := make([]*NodeKV, 0, len(addrSlice)*2+2)

			for _, addrStr := range addrSlice {
				addr := libcommon.HexToAddress(addrStr).String()
				acc := alloc[addrStr]

				if !isEmpty(acc.Balance) {
					balanceKey := utils.KeyEthAddrBalance(addr)
					balanceBig := utils.ConvertHexToBigInt(acc.Balance)
					balanceValue := utils.ScalarToArrayUint64(balanceBig)
					localKvs = append(localKvs, &NodeKV{
						Key:   balanceKey,
						Value: balanceValue,
					})
				}

				if !isEmpty(acc.Nonce) {
					nonceKey := utils.KeyEthAddrNonce(addr)
					nonceBig := utils.ConvertHexToBigInt(acc.Nonce)
					nonceValue := utils.ScalarToArrayUint64(nonceBig)
					localKvs = append(localKvs, &NodeKV{
						Key:   nonceKey,
						Value: nonceValue,
					})
				}

				if !isEmptyCode(acc.Code) {
					keyContractCode := utils.KeyContractCode(addr)
					keyContractLength := utils.KeyContractLength(addr)
					bi, bytecodeLength, _ := smt.HackWrapConvertBytecodeToBigInt(acc.Code)
					localKvs = append(localKvs, &NodeKV{
						Key:   keyContractCode,
						Value: utils.ScalarToArrayUint64(bi),
					})
					localKvs = append(localKvs, &NodeKV{
						Key:   keyContractLength,
						Value: TinyScalarToArrayUint64(uint64(bytecodeLength)),
					})
				}

				if len(acc.Storage) > 50000 {
					// Convert storage to slice for parallel processing
					storageKeys := make([]string, 0, len(acc.Storage))
					for k := range acc.Storage {
						storageKeys = append(storageKeys, k)
					}

					// Calculate chunk size for each worker
					numStorageWorkers := runtime.NumCPU()
					storageChunkSize := (len(storageKeys) + numStorageWorkers - 1) / numStorageWorkers

					var storageWg sync.WaitGroup
					var storageMu sync.Mutex
					addrBig := utils.ConvertHexToBigInt(addr)
					addrArr := utils.ScalarToArrayUint64(addrBig)

					// Process storage concurrently
					for i := 0; i < len(storageKeys); i += storageChunkSize {
						end := i + storageChunkSize
						if end > len(storageKeys) {
							end = len(storageKeys)
						}

						storageWg.Add(1)
						go func(keys []string) {
							defer storageWg.Done()
							localStorageKvs := make([]*NodeKV, 0, len(keys))

							for _, k := range keys {
								v := acc.Storage[k]
								storageBig := utils.ConvertHexToBigInt(v)
								storageValue := utils.ScalarToArrayUint64(storageBig)
								storageKey := KeyContractStorageHack(addrArr, k)
								localStorageKvs = append(localStorageKvs, &NodeKV{
									Key:   storageKey,
									Value: storageValue,
								})
							}

							storageMu.Lock()
							localKvs = append(localKvs, localStorageKvs...)
							storageMu.Unlock()
						}(storageKeys[i:end])
					}

					storageWg.Wait()
				} else {
					// Process storage sequentially when size is small
					addrBig := utils.ConvertHexToBigInt(addr)
					addrArr := utils.ScalarToArrayUint64(addrBig)
					for k, v := range acc.Storage {
						storageBig := utils.ConvertHexToBigInt(v)
						storageValue := utils.ScalarToArrayUint64(storageBig)
						storageKey := KeyContractStorageHack(addrArr, k)
						localKvs = append(localKvs, &NodeKV{
							Key:   storageKey,
							Value: storageValue,
						})
					}
				}
			}

			// Calculate path and shortPath
			for i := range localKvs {
				localKvs[i].path = localKvs[i].Key.GetPath()
				// Convert path to shortPath
				for j := 0; j < 256; j++ {
					if localKvs[i].path[j] == 1 {
						// Set corresponding bit to 1
						// j=0 should map to highest bit, so use 63-(j%64)
						blockIdx := j / 64      // Determine which uint64
						bitPos := 63 - (j % 64) // Position in this uint64, starting from highest bit
						localKvs[i].shortPath[blockIdx] |= uint64(1) << uint64(bitPos)
					}
				}
			}

			// Merge results
			mu.Lock()
			nodeKvs = append(nodeKvs, localKvs...)
			mu.Unlock()
		}(addrs[i:end])
	}

	wg.Wait()
	fmt.Println("prepare elapsed:", time.Since(start1))

	start11 := time.Now()
	slices.SortFunc(nodeKvs, func(a, b *NodeKV) int {
		// Directly compare uint64 arrays
		for i := 0; i < 4; i++ {
			if a.shortPath[i] < b.shortPath[i] {
				return -1
			}
			if a.shortPath[i] > b.shortPath[i] {
				return 1
			}
		}
		return 0
	})
	fmt.Println("sorting nodes elapsed:", time.Since(start11))

	start2 := time.Now()
	calculateLevels(nodeKvs, 0)
	fmt.Println("calculate level elapsed:", time.Since(start2))

	start3 := time.Now()
	// 2. Calculate leaf node hashes concurrently
	chunkSize = (len(nodeKvs) + numWorkers - 1) / numWorkers

	for i := 0; i < len(nodeKvs); i += chunkSize {
		end := i + chunkSize
		if end > len(nodeKvs) {
			end = len(nodeKvs)
		}

		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()
			for i := start; i < end; i++ {
				// value hash
				valueHash := utils.HashByPointers(
					&nodeKvs[i].Value,
					&utils.BranchCapacity,
				)

				// leaf hash
				remainingKey := utils.RemoveKeyBits(nodeKvs[i].Key, nodeKvs[i].level)
				nodeKvs[i].leafHash = *utils.HashByPointers(
					&[8]uint64{
						remainingKey[0], remainingKey[1], remainingKey[2], remainingKey[3],
						valueHash[0], valueHash[1], valueHash[2], valueHash[3],
					},
					&utils.LeafCapacity,
				)
			}
		}(i, end)
	}
	wg.Wait()
	fmt.Println("calculate leaf hash elapsed:", time.Since(start3))

	start4 := time.Now()
	root := utils.NodeKey(calculateRoot(nodeKvs, 0, len(nodeKvs), 0))
	fmt.Println("calculate root hash elapsed:", time.Since(start4))

	return root.ToBigInt(), nil
}

func calculateRoot(nodeKVs []*NodeKV, start, end, level int) [4]uint64 {
	if start >= end {
		return [4]uint64{0, 0, 0, 0} // Return zero hash for empty node
	}

	if start+1 == end {
		return nodeKVs[start].leafHash // Return leaf hash for single node
	}

	// Find the split point
	splitIndex := sort.Search(end-start, func(i int) bool {
		return nodeKVs[start+i].path[level] == 1
	}) + start

	// Calculate hashes for left and right subtrees
	var leftHash, rightHash [4]uint64
	if end-start > 10000 {
		var wg sync.WaitGroup

		if splitIndex > start {
			wg.Add(1)
			go func() {
				defer wg.Done()
				leftHash = calculateRoot(nodeKVs, start, splitIndex, level+1)
			}()
		}

		if splitIndex < end {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rightHash = calculateRoot(nodeKVs, splitIndex, end, level+1)
			}()
		}

		wg.Wait()
	} else {
		if splitIndex > start {
			leftHash = calculateRoot(nodeKVs, start, splitIndex, level+1)
		}
		if splitIndex < end {
			rightHash = calculateRoot(nodeKVs, splitIndex, end, level+1)
		}
	}

	// Calculate hash for current node
	return *utils.HashByPointers(
		&[8]uint64{
			leftHash[0], leftHash[1], leftHash[2], leftHash[3],
			rightHash[0], rightHash[1], rightHash[2], rightHash[3],
		},
		&utils.BranchCapacity,
	)
}

func isEmpty(s string) bool {
	return s == "" || s == "0x0"
}

func isEmptyCode(s string) bool {
	return s == "" || s == "0x"
}

func getSmtroot(chaindata string) error {
	db := mdbx.MustOpen(chaindata)
	defer db.Close()
	tx, _ := db.BeginRw(context.Background())
	eridb := db2.NewEriDb(tx, nil)
	s := smt.NewSMT(eridb, false)
	root, err := s.Db.GetLastRoot()
	if err != nil {
		panic(err)
	}
	fmt.Printf("smt root:%x\n", root)
	if *debugPrint {
		s.RoSMT.PrintDb()
	}
	tx.Rollback()
	return nil
}

var logger log.Logger

func main() {

	debug.RaiseFdLimit()
	flag.Parse()

	// Configure log level based on flag
	var level = log.LvlInfo
	switch strings.ToLower(*logLevel) {
	case "debug":
		level = log.LvlDebug
	case "info":
		level = log.LvlInfo
	case "warn", "warning":
		level = log.LvlWarn
	case "error":
		level = log.LvlError
	default:
		fmt.Printf("Invalid log level: %s, using 'info'\n", *logLevel)
		level = log.LvlInfo
	}

	logger = log.New()
	consoleHandler := log.StreamHandler(os.Stdout, log.TerminalFormat())
	logger.SetHandler(log.LvlFilterHandler(level, consoleHandler))

	// For X Layer, split db
	kv.InitStandaloneSMT(*standaloneSmtDb)

	if *cpuprofile != "" {
		logger.Info("starting cpu profile")
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Error("could not create CPU profile", "err", err)
			return
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Error("could not start CPU profile", "err", err)
			return
		}
		defer pprof.StopCPUProfile()
	}
	go func() {
		if err := http.ListenAndServe("localhost:6961", nil); err != nil {
			log.Error("Failure in running pprof server", "err", err)
		}
	}()

	start := time.Now()

	var err error
	switch *action {
	case "cfg":
		flow.TestGenCfg()

	case "testBlockHashes":
		testBlockHashes(*chaindata, *block, libcommon.HexToHash(*hash))

	case "readAccount":
		if err := readAccount(*chaindata, libcommon.HexToAddress(*account)); err != nil {
			fmt.Printf("Error: %v\n", err)
		}

	case "nextIncarnation":
		nextIncarnation(*chaindata, libcommon.HexToHash(*account))

	case "dumpStorage":
		dumpStorage()

	case "countAccounts":
		countAccounts(*chaindata)

	case "current":
		printCurrentBlockNumber(*chaindata)

	case "bucket":
		printBucket(*chaindata, kv.PlainState)

	case "buckets":
		printBuckets(*chaindata, *bucket)

	case "slice":
		dbSlice(*chaindata, *bucket, common.FromHex(*hash))

	case "searchChangeSet":
		err = searchChangeSet(*chaindata, common.FromHex(*hash), uint64(*block))

	case "searchStorageChangeSet":
		err = searchStorageChangeSet(*chaindata, common.FromHex(*hash), uint64(*block))

	case "extractCode":
		err = extractCode(*chaindata)

	case "iterateOverCode":
		err = iterateOverCode(*chaindata)

	case "extractHeaders":
		err = extractHeaders(*chaindata, uint64(*block), int64(*blockTotal))

	case "extractHashes":
		err = extractHashes(*chaindata, uint64(*block), int64(*blockTotal), *name)

	case "defrag":
		err = hackdb.Defrag()

	case "textInfo":
		err = hackdb.TextInfo(*chaindata, &strings.Builder{})

	case "extractBodies":
		err = extractBodies(*chaindata)

	case "repairCurrent":
		repairCurrent()

	case "printTxHashes":
		printTxHashes(*chaindata, uint64(*block))

	case "snapSizes":
		err = snapSizes(*chaindata)

	case "readCallTraces":
		err = readCallTraces(*chaindata, uint64(*block))

	case "fixTd":
		err = fixTd(*chaindata)

	case "advanceExec":
		err = advanceExec(*chaindata)

	case "backExec":
		err = backExec(*chaindata)

	case "fixState":
		err = fixState(*chaindata)

	case "trimTxs":
		err = trimTxs(*chaindata)

	case "scanTxs":
		err = scanTxs(*chaindata)

	case "scanReceipts2":
		err = scanReceipts2(*chaindata)

	case "scanReceipts3":
		err = scanReceipts3(*chaindata, uint64(*block))

	case "devTx":
		err = devTx(*chaindata)
	case "chainConfig":
		err = chainConfig(*name)
	case "findPrefix":
		err = findPrefix(*chaindata)
	case "findLogs":
		err = findLogs(*chaindata, uint64(*block), uint64(*blockTotal))
	case "iterate":
		err = iterate(*chaindata, *account)
	case "rmSnKey":
		err = rmSnKey(*chaindata)
	case "readSeg":
		err = readSeg(*chaindata)
	case "dumpState":
		err = dumpState(*chaindata)
	case "readAccountAtVersion":
		err = readAccountAtVersion(*chaindata, *account, uint64(*block))
	case "getOldAccInputHash":
		err = getOldAccInputHash(uint64(*block))
	case "dumpAll":
		err = dumpAll(*chaindata, *output)
	case "migrateGenesis":
		err = migrateGenesis(*chaindata, *input, *output)
	case "verifySmtWithStateDiff":
		err = VerifySmtWithStateDiff(
			*preSmtData, *preChainData,
			*preStateSnapshotFilePath, *postSmtData, *postStateSnapshotFilePath, *outputStateDiffFilePath)
	case "checkStateRoot":
		if *standaloneSmtDb {
			err = checkStateRoot(*chaindata, *pathSmtDb, *input, *incremental, *debugPrint)
		} else {
			err = checkStateRoot(*chaindata, "", *input, *incremental, *debugPrint)
		}
	case "checkStateRootFast":
		if *standaloneSmtDb {
			err = checkStateRootFast(*chaindata, *pathSmtDb, *input)
		} else {
			err = checkStateRootFast(*chaindata, "", *input)
		}
	case "getSmtroot":
		err = getSmtroot(*chaindata)
	default:
		fmt.Printf("Unknown action: %s\n", *action)
		return
	}

	fmt.Println("total time elapsed:", time.Since(start))

	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		os.Exit(0)
	}
}

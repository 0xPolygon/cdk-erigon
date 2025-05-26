package txpool

import (
	"container/heap"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/common/cmp"
	"github.com/ledgerwatch/erigon-lib/common/fixedgas"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/types"
	ecommon "github.com/ledgerwatch/erigon/common"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/zkevm/hex"
)

// free gas tx type
const (
	notFree = iota
	claim
	freeByNonce
	specificProject
)

var (
	removeWG sync.WaitGroup
)

const (
	erc20TransferMethod = "0xa9059cbb"
)

// XLayerConfig contains the X Layer configs for the txpool
type XLayerConfig struct {
	// BlockedList is the blocked address list
	BlockedList common.OrderedList[common.Address]
	// EnableWhitelist is a flag to enable/disable the whitelist
	EnableWhitelist bool
	// WhiteList is the white address list
	WhiteList common.OrderedList[common.Address]
	// FreeClaimGasAddrs is the address list for free claimTx
	FreeClaimGasAddrs common.OrderedList[common.Address]
	// GasPriceMultiple is the factor claim tx gas price should mul
	GasPriceMultiple uint64
	// EnableFreeGasByNonce enable free gas
	EnableFreeGasByNonce bool
	// FreeGasExAddrs is the ex address which can be free gas for the transfer receiver
	FreeGasExAddrs common.OrderedList[common.Address]
	// FreeGasCountPerAddr is the count limit of free gas tx per address
	FreeGasCountPerAddr uint64
	// FreeGasLimit is the max gas allowed use to do a free gas tx
	FreeGasLimit uint64
	// EnableFreeGasList enable the specific free gas project
	EnableFreeGasList  bool
	FreeGasFromNameMap map[common.Address]string         // map[from]projectName
	FreeGasList        map[string]*ethconfig.FreeGasInfo // map[projectName]FreeGasInfo
	// EnableTimsort is the switch to use timsort on the best slice of txpool
	EnableTimsort bool
	EnableNotify  bool
}

type GPCache interface {
	GetLatest() (common.Hash, *big.Int)
	GetLatestPriceReadOnly() *big.Int
	SetLatest(hash common.Hash, price *big.Int)
	GetLatestRawGP() *big.Int
	SetLatestRawGP(rgp *big.Int)
}

func (p *TxPool) bestForXLayer(n uint16, txs *types.TxsRlp, tx kv.Tx, onTopOf, availableGas, availableBlobGas uint64, toSkip mapset.Set[[32]byte]) (bool, int, error) {
	removeWG.Wait()
	ok, count, toRemove, err := p.bestRead(n, txs, tx, onTopOf, availableGas, availableBlobGas, toSkip)
	if err != nil {
		return ok, count, err
	}
	if !ok {
		return false, count, nil
	}
	txs.Resize(uint(count))
	if len(toRemove) > 0 {
		removeWG.Add(1)
		go func() {
			p.lock.Lock()
			defer p.lock.Unlock()
			removeWG.Done()
			for _, mt := range toRemove {
				p.pending.Remove(mt)
				p.discardLocked(mt, UnsupportedTx)
				//log.Debug("Removed transaction from pending pool", "txID", mt.Tx.IDHash)
			}
		}()
		time.Sleep(1 * time.Nanosecond)
	}

	return true, count, nil
}

func (p *TxPool) bestRead(n uint16, txs *types.TxsRlp, tx kv.Tx, onTopOf, availableGas, availableBlobGas uint64, toSkip mapset.Set[[32]byte]) (bool, int, []*metaTx, error) {
	if p.isDeniedYieldingTransactions() {
		//log.Trace("Denied yielding transactions, cannot proceed")
		return false, 0, nil, nil
	}

	// First wait for the corresponding block to arrive
	if p.lastSeenBlock.Load() < onTopOf {
		//log.Trace("Block not yet arrived, too early to process", "lastSeenBlock", p.lastSeenBlock.Load(), "requiredBlock", onTopOf)
		return false, 0, nil, nil
	}

	isShanghai := p.isShanghai()
	isLondon := p.isLondon()

	p.lock.RLock()
	defer p.lock.RUnlock()

	best := p.pending.best

	txs.Resize(uint(cmp.Min(int(n), len(best.ms))))
	var toRemove []*metaTx
	count := 0

	p.pending.EnforceBestInvariants()

	for i := 0; count < int(n) && i < len(best.ms); i++ {
		// if we wouldn't have enough gas for a standard transaction then quit out early
		if availableGas < fixedgas.TxGas {
			break
		}

		mt := best.ms[i]
		//log.Trace("Processing transaction", "txID", mt.Tx.IDHash)

		if toSkip.Contains(mt.Tx.IDHash) {
			//log.Trace("Skipping transaction, already in toSkip", "txID", mt.Tx.IDHash)
			continue
		}

		if !isLondon && mt.Tx.Type == 0x2 {
			// remove ldn txs when not in london
			toRemove = append(toRemove, mt)
			toSkip.Add(mt.Tx.IDHash)
			//log.Info("Removing London transaction in non-London environment", "txID", mt.Tx.IDHash)
			continue
		}

		if mt.Tx.Gas > transactionGasLimit {
			// Skip transactions with very large gas limit, these shouldn't enter the pool at all
			//log.Debug("found a transaction in the pending pool with too high gas for tx - clear the tx pool")
			//log.Trace("Skipping transaction with too high gas", "txID", mt.Tx.IDHash, "gas", mt.Tx.Gas)
			continue
		}
		rlpTx, sender, isLocal, err := p.getRlpLocked(tx, mt.Tx.IDHash[:])
		if err != nil {
			//log.Trace("Error getting RLP of transaction", "txID", mt.Tx.IDHash, "error", err)
			return false, count, toRemove, err
		}
		if len(rlpTx) == 0 {
			toRemove = append(toRemove, mt)
			//log.Info("Removing transaction with empty RLP", "txID", common.BytesToHash(mt.Tx.IDHash[:]))
			continue
		}

		// Skip transactions that require more blob gas than is available
		blobCount := uint64(len(mt.Tx.BlobHashes))
		if blobCount*fixedgas.BlobGasPerBlob > availableBlobGas {
			//log.Trace("Skipping transaction due to insufficient blob gas", "txID", mt.Tx.IDHash, "requiredBlobGas", blobCount*fixedgas.BlobGasPerBlob, "availableBlobGas", availableBlobGas)
			continue
		}
		availableBlobGas -= blobCount * fixedgas.BlobGasPerBlob

		// make sure we have enough gas in the caller to add this transaction.
		// not an exact science using intrinsic gas but as close as we could hope for at
		// this stage
		intrinsicGas, _ := CalcIntrinsicGas(uint64(mt.Tx.DataLen), uint64(mt.Tx.DataNonZeroLen), nil, mt.Tx.Creation, true, true, isShanghai)
		if intrinsicGas > availableGas {
			// we might find another TX with a low enough intrinsic gas to include so carry on
			//log.Trace("Skipping transaction due to insufficient gas", "txID", mt.Tx.IDHash, "intrinsicGas", intrinsicGas, "availableGas", availableGas)
			continue
		}

		if intrinsicGas <= availableGas { // check for potential underflow
			availableGas -= intrinsicGas
		}

		//log.Trace("Including transaction", "txID", mt.Tx.IDHash)
		txs.Txs[count] = rlpTx
		txs.TxIds[count] = mt.Tx.IDHash
		copy(txs.Senders.At(count), sender.Bytes())
		txs.IsLocal[count] = isLocal
		toSkip.Add(mt.Tx.IDHash)
		count++
	}

	return true, count, toRemove, nil
}

func contains(addresses []common.Address, addr common.Address) bool {
	for _, item := range addresses {
		if item == addr {
			return true
		}
	}
	return false
}

func containsMethod(data string, methods []string) bool {
	for _, m := range methods {
		if strings.Contains(data, m) {
			return true
		}
	}
	return false
}

// ApolloConfig is the interface for the singleton apollo config instance.
// This design is necessary to prevent circular dependencies on the txpool
// with the apollo package
type ApolloConfig interface {
	CheckBlockedAddr(localBlockedList common.OrderedList[common.Address], addr common.Address) bool
	GetEnableWhitelist(localEnableWhitelist bool) bool
	CheckWhitelistAddr(localWhitelist common.OrderedList[common.Address], addr common.Address) bool
	CheckFreeClaimAddr(localFreeClaimGasAddrs common.OrderedList[common.Address], addr common.Address) bool
	CheckFreeGasExAddr(localFreeGasExAddrs common.OrderedList[common.Address], addr common.Address) bool
	GetEnableFreeGasList(localEnableFreeGasList bool) bool
}

// SetApolloConfig sets the apollo config with the node's apollo config
// singleton instance
func (p *TxPool) SetApolloConfig(cfg ApolloConfig) {
	p.apolloCfg = cfg
}

func (p *TxPool) SetGpCacheForXLayer(gpCache GPCache) {
	p.gpCache = gpCache
}

func (p *TxPool) checkFreeGasExAddrXLayer(senderID uint64) bool {
	addr, ok := p.senders.senderID2Addr[senderID]
	if !ok {
		return false
	}
	return p.apolloCfg.CheckFreeGasExAddr(p.xlayerCfg.FreeGasExAddrs, addr)
}

func (p *TxPool) checkFreeGasAddrXLayer(senderID uint64, tx *types.TxSlot) (freeType int, gpMul uint64) {
	addr := common.Address{}
	freeType, gpMul = p.checkFreeGasSenderXLayer(senderID, &addr)
	if addr == [20]byte{} {
		return
	}
	if freeType != notFree {
		return
	}

	return p.checkFreeGasTxXLayer(addr, tx)
}

func (p *TxPool) checkFreeGasSenderXLayer(senderID uint64, address *common.Address) (freeType int, gpMul uint64) {
	addr, ok := p.senders.senderID2Addr[senderID]
	if !ok {
		return
	}
	*address = addr
	// is claim tx
	if p.apolloCfg != nil && p.apolloCfg.CheckFreeClaimAddr(p.xlayerCfg.FreeClaimGasAddrs, addr) {
		return claim, p.xlayerCfg.GasPriceMultiple
	}

	// 	new bridge address
	free := p.freeGasAddrs[addr.String()]
	if free {
		return freeByNonce, 1
	}

	return notFree, 0
}

func (p *TxPool) checkFreeGasTxXLayer(addr common.Address, tx *types.TxSlot) (freeType int, gpMul uint64) {
	// specific project

	if p.apolloCfg != nil && p.apolloCfg.GetEnableFreeGasList(p.xlayerCfg.EnableFreeGasList) {
		fromToName, freeGpList := p.xlayerCfg.FreeGasFromNameMap, p.xlayerCfg.FreeGasList
		info := freeGpList[fromToName[addr]]
		if info != nil &&
			contains(info.ToList, tx.To) &&
			containsMethod(ecommon.Bytes2Hex(tx.Rlp), info.MethodSigs) {

			return specificProject, info.GasPriceMultiple
		}
	}

	return notFree, 0
}

func (p *TxPool) setFreeGasByNonceCache(senderID uint64, mt *metaTx, isClaim bool) {
	if p.xlayerCfg.EnableFreeGasByNonce {
		if p.checkFreeGasExAddrXLayer(senderID) {
			inputHex := hex.EncodeToHex(mt.Tx.Rlp)
			if strings.HasPrefix(inputHex, erc20TransferMethod) && len(inputHex) > 74 {
				addrHex := "0x" + inputHex[10:74]
				p.freeGasAddrs[addrHex] = true
			} else {
				p.freeGasAddrs[mt.Tx.To.Hex()] = true
			}
		} else if isClaim && mt.Tx.Nonce < p.xlayerCfg.FreeGasCountPerAddr {
			inputHex := hex.EncodeToHex(mt.Tx.Rlp)
			if len(inputHex) > 4554 {
				addrHex := "0x" + inputHex[4490:4554]
				p.freeGasAddrs[addrHex] = true
			} else {
				p.freeGasAddrs[mt.Tx.To.Hex()] = true
			}
		}
	}
}

func (p *TxPool) isFreeGasXLayer(senderID uint64, tx *types.TxSlot) bool {
	freeType, _ := p.checkFreeGasAddrXLayer(senderID, tx)
	return freeType > notFree
}

func (p *TxPool) setFreeGasList(freeGasList []ethconfig.FreeGasInfo) {
	p.xlayerCfg.FreeGasFromNameMap = make(map[common.Address]string)
	p.xlayerCfg.FreeGasList = make(map[string]*ethconfig.FreeGasInfo, len(freeGasList))
	for _, info := range freeGasList {
		for _, from := range info.FromList {
			p.xlayerCfg.FreeGasFromNameMap[from] = info.Name
		}
		infoCopy := info
		p.xlayerCfg.FreeGasList[info.Name] = &infoCopy
	}
}

var requireTxPoolLock atomic.Bool

func ArquireTxPoolLock(acquire bool) {
	requireTxPoolLock.Swap(acquire)
}

func IsAcquireTxPoolLock() bool {
	return requireTxPoolLock.Load()
}

func (p *PendingPool) RemoveNoLock(i *metaTx) {
	if i.worstIndex >= 0 {
		heap.Remove(p.worst, i.worstIndex)
	}
	if i.bestIndex >= 0 {
		p.best.UnsafeRemove(i)
	}
	if i.bestIndex != p.best.Len()-1 {
		p.sorted.Swap(false)
	}
	i.currentSubPool = 0
}

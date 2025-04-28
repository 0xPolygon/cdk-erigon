package txpool

import (
	"context"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/types"
	"github.com/ledgerwatch/erigon/cmd/utils"
	ecommon "github.com/ledgerwatch/erigon/common"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/zk/apollo"
	"github.com/ledgerwatch/erigon/zkevm/hex"
	"github.com/ledgerwatch/erigon/zkevm/log"
)

// free gas tx type
const (
	notFree = iota
	claim
	freeByNonce
	specificProject
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
		log.Info("checkFreeGasTxXLayer info=", info == nil)
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
		log.Info(fmt.Sprintf("setFreeGasList fromlist=%v, tolist=%v, name=%s", info.FromList, info.ToList, info.Name))
		for _, from := range info.FromList {
			p.xlayerCfg.FreeGasFromNameMap[from] = info.Name
		}
		infoCopy := info
		p.xlayerCfg.FreeGasList[info.Name] = &infoCopy
	}
}

func (p *TxPool) listenApollo(ctx context.Context) {
	stream := apollo.GetEthConfigStream()
	ch, remove := stream.Sub()
	defer remove()

	for {
		select {
		case ethCfg := <-ch:
			if ethCfg == nil {
				continue
			}
			if slices.Contains(ethCfg.XLayer.ApolloChanged, utils.TxPoolEnableTimsort.Name) {
				p.pending.mtx.Lock()
				p.pending.enbaleTimsort = ethCfg.DeprecatedTxPool.EnableTimsort
				p.pending.mtx.Unlock()
			}
			if slices.Contains(ethCfg.XLayer.ApolloChanged, utils.TxPoolFreeGasList.Name) {
				p.lock.Lock()
				p.setFreeGasList(ethCfg.DeprecatedTxPool.FreeGasList)
				p.lock.Unlock()
			}
		case <-ctx.Done():
			return
		}
	}
}

var requireTxPoolLock atomic.Bool

func ArquireTxPoolLock(acquire bool) {
	requireTxPoolLock.Swap(acquire)
}

func IsAcquireTxPoolLock() bool {
	return requireTxPoolLock.Load()
}

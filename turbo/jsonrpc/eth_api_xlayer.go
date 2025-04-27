package jsonrpc

import (
	"context"
	"fmt"
	"math/big"
	"sync"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/common/math"
	"github.com/ledgerwatch/erigon/eth/gasprice/gaspricecfg"
	"github.com/ledgerwatch/erigon/zk/apollo"
)

func (apii *APIImpl) GetGPCache() *GasPriceCache {
	return apii.gasCache
}

func (apii *APIImpl) runL2GasPricerForXLayer() {
	// set default gas price
	apii.gasCache.SetLatest(common.Hash{}, apii.L2GasPricer.GetConfig().Default)
	apii.gasCache.SetLatestRawGP(apii.L2GasPricer.GetConfig().Default)
	go apii.runL2GasPriceSuggester()
}

func (apii *APIImpl) listenApollo(ctx context.Context) {
	stream := apollo.GetEthConfigStream()
	ch, remove := stream.Sub()
	defer remove()

	for {
		select {
		case ethCfg := <-ch:
			if ethCfg == nil {
				continue
			}
			if apii.BulkAddTxs != ethCfg.XLayer.BulkAddTxs {
				apii.BulkAddTxs = ethCfg.XLayer.BulkAddTxs
			}
			if apii.BulkAddTxsSize != ethCfg.XLayer.BulkAddTxsSize {
				apii.BulkAddTxsSize = ethCfg.XLayer.BulkAddTxsSize
			}
			if apii.BulkAddTxsWaitTime != ethCfg.XLayer.BulkAddTxsWaitTime {
				apii.BulkAddTxsWaitTime = ethCfg.XLayer.BulkAddTxsWaitTime
			}
			if apii.EnableNotify != ethCfg.XLayer.EnableAddTxNotify {
				apii.EnableNotify = ethCfg.XLayer.EnableAddTxNotify
			}
		case <-ctx.Done():
			return
		}
	}
}

const (
	// maxCacheSize = 300sec (TTL) / 10sec (UpdatePeriod) = 30
	maxCacheSize = 30

	// minGPWindowSize defines the window size to be used when calculating the
	// MinGP from the cache
	minGPWindowSize = 27
)

type RawGPCache struct {
	values [maxCacheSize]*big.Int
	head   int // Points to the current head of the buffer
}

// NewRawGPCache initializes a RawGPCache with a fixed size cache
func NewRawGPCache() *RawGPCache {
	return &RawGPCache{
		head: 0,
	}
}

// Add adds an RGP to the cache and manages the head position
func (c *RawGPCache) Add(rgp *big.Int) {
	c.values[c.head] = new(big.Int).Set(rgp)
	c.head = (c.head + 1) % maxCacheSize
}

// GetMin returns the minimum RGP in the cache
func (c *RawGPCache) GetMin() (*big.Int, error) {
	isEmpty := true
	minRGP := big.NewInt(0).SetInt64(math.MaxInt64) // Initialize to maximum big.Int
	for _, value := range c.values {
		if value == nil {
			continue
		}
		isEmpty = false
		if value.Cmp(minRGP) < 0 {
			minRGP = value
		}
	}

	if isEmpty {
		return nil, fmt.Errorf("no values in cache")
	}

	return new(big.Int).Set(minRGP), nil
}

// GetMinGPMoreRecent returns the minimum RGP in the cache for the last minGPWindowSize elements
func (c *RawGPCache) GetMinGPMoreRecent() (*big.Int, error) {
	isEmpty := true
	minRGP := big.NewInt(0).SetInt64(math.MaxInt64) // Initialize to maximum big.Int

	for i := 1; i <= minGPWindowSize; i++ {
		index := (c.head - i + maxCacheSize) % maxCacheSize
		value := c.values[index]
		if value == nil {
			break
		}

		isEmpty = false
		if value.Cmp(minRGP) < 0 {
			minRGP = value
		}
	}

	if isEmpty {
		return nil, fmt.Errorf("no values in cache")
	}

	return new(big.Int).Set(minRGP), nil
}

func (c *GasPriceCache) GetLatestRawGP() *big.Int {
	rgp, err := c.rawGPCache.GetMin()
	if err != nil {
		return gaspricecfg.DefaultXLayerPrice
	}
	return rgp
}

func (c *GasPriceCache) GetMinRawGPMoreRecent() *big.Int {
	rgp, err := c.rawGPCache.GetMinGPMoreRecent()
	if err != nil {
		return gaspricecfg.DefaultXLayerPrice
	}
	return rgp
}

func (c *GasPriceCache) SetLatestRawGP(rgp *big.Int) {
	c.rawGPCache.Add(rgp)
}

var GasPricerOnce sync.Once

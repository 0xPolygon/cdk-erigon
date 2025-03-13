package vm

import (
	"fmt"
	"time"

	"github.com/ledgerwatch/erigon/cl/phase1/core/state/lru"
	"github.com/ledgerwatch/log/v3"
)

var PrecompiledCache *lru.CacheWithTTL[string, []byte]

func InitPrecompiledCache(cacheSize int, ttl time.Duration) {
	log.Info(fmt.Sprintf("prerun cache config: cacheSize = %d, ttl = %s", cacheSize, ttl))
	PrecompiledCache = lru.NewWithTTL[string, []byte]("evm_precompiled_cache", cacheSize, ttl)
}

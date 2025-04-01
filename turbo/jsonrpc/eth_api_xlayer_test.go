package jsonrpc

import (
	"github.com/ledgerwatch/erigon-lib/common"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCache_AddAndGetMin(t *testing.T) {
	cache := NewRawGPCache()

	// Add values to the cache
	cache.Add(big.NewInt(5))
	cache.Add(big.NewInt(3))
	cache.Add(big.NewInt(7))

	// Check the minimum value
	minRGP, err := cache.GetMin()
	require.NoError(t, err)

	expectedMin := big.NewInt(3)
	require.Equal(t, expectedMin.Int64(), minRGP.Int64())
}

func TestCache_EmptyCache(t *testing.T) {
	cache := NewRawGPCache()

	// Try to get the minimum value from an empty cache
	_, err := cache.GetMin()
	require.Error(t, err)
}

func TestCache_CircularBufferOverwrite(t *testing.T) {
	cache := NewRawGPCache()

	// Fill the cache to its limit
	for i := 1; i <= maxCacheSize; i++ {
		cache.Add(big.NewInt(int64(i * 10)))
	}

	// Now add one more item, which should overwrite the oldest item
	cache.Add(big.NewInt(5))

	// Check that the minimum value is now 5, which is the smallest in the current buffer
	minRGP, err := cache.GetMin()
	require.NoError(t, err)

	expectedMin := big.NewInt(5)
	require.Equal(t, expectedMin.Int64(), minRGP.Int64())
}

func TestCache_MultipleValues(t *testing.T) {
	cache := NewRawGPCache()

	// Add multiple values
	cache.Add(big.NewInt(8))
	cache.Add(big.NewInt(6))
	cache.Add(big.NewInt(4))
	cache.Add(big.NewInt(9))

	// Check the minimum value
	minRGP, err := cache.GetMin()
	require.NoError(t, err)

	expectedMin := big.NewInt(4)
	require.Equal(t, expectedMin.Int64(), minRGP.Int64())
}

func TestCache_ValuesExactlyAtLimit(t *testing.T) {
	cache := NewRawGPCache()

	// Fill the cache to its limit with specific values
	cache.Add(big.NewInt(8))
	cache.Add(big.NewInt(4))
	cache.Add(big.NewInt(12))
	cache.Add(big.NewInt(2))
	cache.Add(big.NewInt(6))

	// Now check the minimum value
	minRGP, err := cache.GetMin()
	require.NoError(t, err)

	expectedMin := big.NewInt(2)
	require.Equal(t, expectedMin.Int64(), minRGP.Int64())
}

func TestCache_OverwriteOldValues(t *testing.T) {
	cache := NewRawGPCache()

	// Add values to fill the buffer
	for i := 1; i <= maxCacheSize; i++ {
		cache.Add(big.NewInt(int64(i * 10)))
	}

	// Add additional values to overwrite the oldest ones
	cache.Add(big.NewInt(1))
	cache.Add(big.NewInt(2))
	cache.Add(big.NewInt(3))

	// Check that the minimum value is now 1, which is the smallest in the buffer
	minRGP, err := cache.GetMin()
	require.NoError(t, err)

	expectedMin := big.NewInt(1)
	require.Equal(t, expectedMin.Int64(), minRGP.Int64())
}

func TestRawGPCache_GetMinGPMoreRecent(t *testing.T) {
	cache := NewRawGPCache()

	// Fill the cache with initial values
	for i := 0; i < maxCacheSize; i++ {
		cache.Add(big.NewInt(int64(i + 1)))
	}

	// Ensure we get the minimum from the last minGPWindowSize elements
	minRGP, err := cache.GetMinGPMoreRecent()
	require.NoError(t, err)

	expectedMin := big.NewInt(4)
	require.Equal(t, expectedMin.Int64(), minRGP.Int64())
}

func TestRawGPCache_GetMinGPMoreRecent_OverwriteOldValues(t *testing.T) {
	cache := NewRawGPCache()

	// Fill the cache with initial values
	for i := 0; i < maxCacheSize; i++ {
		cache.Add(big.NewInt(int64(i + 1)))
	}

	// Now add more values that overwrite the older ones
	cache.Add(big.NewInt(30))
	cache.Add(big.NewInt(30)) // This is the new minimum
	cache.Add(big.NewInt(30))

	// Ensure we get the minimum from the last minGPWindowSize elements
	minRGP, err := cache.GetMinGPMoreRecent()
	require.NoError(t, err)

	expectedMin := big.NewInt(7)
	require.Equal(t, expectedMin.Int64(), minRGP.Int64())
}

func TestGasPriceCache(t *testing.T) {
	gp := NewGasPriceCache()
	gp.SetLatest(common.Hash{1, 2, 3}, big.NewInt(10))
	p := gp.GetLatestPriceReadOnly()
	if p.Cmp(big.NewInt(10)) != 0 {
		t.Fatal("invalid price")
	}
	head, p2 := gp.GetLatest()
	if p2.Cmp(p) != 0 || head != [32]byte{1, 2, 3} {
		t.Fatal("invalid")
	}
}

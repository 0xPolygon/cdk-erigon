//go:build !skip_smoke_realtime
// +build !skip_smoke_realtime

package test

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/holiman/uint256"
	"github.com/ledgerwatch/erigon-lib/common"
	ethTypes "github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/crypto"
	"github.com/ledgerwatch/erigon/ethclient"
	"github.com/ledgerwatch/erigon/rpc"
	"github.com/ledgerwatch/erigon/zk/realtime/realtimeapi"
	"github.com/ledgerwatch/erigon/zk/realtime/rtclient"
	"github.com/ledgerwatch/erigon/zkevm/encoding"
	logger "github.com/ledgerwatch/log/v3"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

var (
	Iterations = 11
)

func TestRealtimeBenchmarkNativeTransfer(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	ctx := context.Background()
	ec, err := ethclient.Dial(DefaultL2NetworkRealtimeURL)
	require.NoError(t, err)
	client := rtclient.NewRealtimeClient(ec, DefaultL2NetworkRealtimeURL)

	// Default test address for tests that require an address
	testAddress := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Benchmark transfer tx to test address
	var totalRealtimeBalanceDuration, totalEthBalanceDuration time.Duration
	for i := 0; i < Iterations; i++ {
		balance, err := client.BalanceAt(ctx, testAddress, nil)
		require.NoError(t, err)
		realtimeBalance, err := client.RealtimeGetBalance(testAddress)
		require.NoError(t, err)
		require.Equal(t, balance.String(), realtimeBalance.String())

		// Send tx
		signedTx := nativeTransferTx(t, context.Background(), client, uint256.NewInt(encoding.Gwei), testAddress.String())

		// Run state benchmark
		g, ctx := errgroup.WithContext(ctx)
		var realtimeBalanceDuration, ethBalanceDuration time.Duration
		g.Go(func() error {
			duration, err := WaitCallback(ctx, client, signedTx, common.Address{}, *signedTx.GetTo(), balance.ToBig(), DefaultTimeoutTxToBeMined, WaitAccountBalanceRealtime)
			if err != nil {
				return err
			}
			realtimeBalanceDuration = duration
			return nil
		})

		g.Go(func() error {
			duration, err := WaitCallback(ctx, client, signedTx, common.Address{}, *signedTx.GetTo(), balance.ToBig(), DefaultTimeoutTxToBeMined, WaitAccountBalanceEth)
			if err != nil {
				return err
			}
			ethBalanceDuration = duration
			return nil
		})

		// Wait for all goroutines to complete
		err = g.Wait()
		require.NoError(t, err)

		if i == 0 {
			continue
		}
		totalRealtimeBalanceDuration += realtimeBalanceDuration
		totalEthBalanceDuration += ethBalanceDuration

		fmt.Printf("Iteration %v:\n", i)
		fmt.Printf("RT state update for native tx transfer confirmation took: %s\n", realtimeBalanceDuration)
		fmt.Printf("ETH state update for native tx transfer confirmation took: %s\n", ethBalanceDuration)
	}

	avgRealtimeBalanceDuration := time.Duration(int64(totalRealtimeBalanceDuration) / int64(Iterations-1))
	avgEthBalanceDuration := time.Duration(int64(totalEthBalanceDuration) / int64(Iterations-1))

	// Log out metrics
	fmt.Printf("Avg RT state update for native tx transfer confirmation took: %s\n", avgRealtimeBalanceDuration)
	fmt.Printf("Avg ETH state update for native tx transfer confirmation took: %s\n", avgEthBalanceDuration)
}

func TestRealtimeBenchmarkERC20Transfer(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	ctx := context.Background()
	ec, err := ethclient.Dial(DefaultL2NetworkRealtimeURL)
	require.NoError(t, err)
	client := rtclient.NewRealtimeClient(ec, DefaultL2NetworkRealtimeURL)

	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(DefaultL2AdminPrivateKey, "0x"))
	require.NoError(t, err)

	// Default test address for tests that require an address
	fromAddress := common.HexToAddress(DefaultL2AdminAddress)
	testAddress := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Deploy the contract
	erc20Address := deployERC20Contract(t, ctx, privateKey, client)
	transferAmount := new(big.Int).Mul(big.NewInt(1), big.NewInt(1e18)) // Adjust for token decimals (18 in this case)

	startNonce, err := client.RealtimeGetTransactionCount(fromAddress)
	require.NoError(t, err)

	// Benchmark erc20 transfer tx
	var totalRealtimeBalanceDuration, totalEthBalanceDuration time.Duration
	for i := 0; i < Iterations; i++ {
		balance, err := client.EthGetTokenBalance(ctx, testAddress, erc20Address, nil)
		require.NoError(t, err)
		realtimeBalance, err := client.RealtimeGetTokenBalance(fromAddress, testAddress, erc20Address)
		require.NoError(t, err)
		require.Equal(t, balance.String(), realtimeBalance.String())

		signedTx := erc20TransferTx(t, ctx, privateKey, client, transferAmount, testAddress, erc20Address, startNonce+uint64(i))

		// Run state benchmark
		g, ctx := errgroup.WithContext(ctx)
		var realtimeBalanceDuration, ethBalanceDuration time.Duration
		g.Go(func() error {
			duration, err := WaitCallback(ctx, client, signedTx, fromAddress, testAddress, balance, DefaultTimeoutTxToBeMined, WaitTokenBalanceRealtime)
			if err != nil {
				return err
			}
			realtimeBalanceDuration = duration
			return nil
		})

		g.Go(func() error {
			duration, err := WaitCallback(ctx, client, signedTx, fromAddress, testAddress, balance, DefaultTimeoutTxToBeMined, WaitTokenBalanceEth)
			if err != nil {
				return err
			}
			ethBalanceDuration = duration
			return nil
		})

		// Wait for all goroutines to complete
		err = g.Wait()
		require.NoError(t, err)

		if i == 0 {
			continue
		}
		totalRealtimeBalanceDuration += realtimeBalanceDuration
		totalEthBalanceDuration += ethBalanceDuration

		fmt.Printf("Iteration %v:\n", i)
		fmt.Printf("RT state update for erc20 tx transfer confirmation took: %s\n", realtimeBalanceDuration)
		fmt.Printf("ETH state update for erc20 tx transfer confirmation took: %s\n", ethBalanceDuration)
	}

	avgRealtimeBalanceDuration := time.Duration(int64(totalRealtimeBalanceDuration) / int64(Iterations-1))
	avgEthBalanceDuration := time.Duration(int64(totalEthBalanceDuration) / int64(Iterations-1))

	// Log out metrics
	fmt.Printf("Avg RT state update for erc20 tx transfer confirmation took: %s\n", avgRealtimeBalanceDuration)
	fmt.Printf("Avg ETH state update for erc20 tx transfer confirmation took: %s\n", avgEthBalanceDuration)
}

func TestRealtimeBenchmarNewHeadsSubscription(t *testing.T) {
	ctx := context.Background()
	logger := logger.New()
	wsClient, err := rpc.Dial(DefaultL2NetworkWSURL, logger)
	require.NoError(t, err)

	// Benchmark variables
	var totalSubTimeDiff time.Duration

	realtimeMsgCh := make(chan realtimeapi.RealtimeSubResult)
	realtimeSub, err := wsClient.Subscribe(ctx, "eth", realtimeMsgCh, "realtime", map[string]bool{"NewHeads": true, "TransactionExtraInfo": false, "TransactionReceipt": false, "TransactionInnerTxs": false})
	require.NoError(t, err)
	defer realtimeSub.Unsubscribe()

	ethMsgCh := make(chan ethTypes.Header)
	ethSub, err := wsClient.Subscribe(ctx, "eth", ethMsgCh, "newHeads")
	require.NoError(t, err)
	defer ethSub.Unsubscribe()

	// Benchmark realtime vs eth subscibe new block headers
	heights := make(map[int64]time.Time)
	count := 0
	for count < Iterations {
		select {
		case msg := <-realtimeMsgCh:
			if msg.Header != nil {
				height := msg.Header.Number.Int64()
				heights[height] = time.Now()
			}
		case msg := <-ethMsgCh:
			height := msg.Number.Int64()
			_, ok := heights[height]
			if ok {
				timeDiff := time.Since(heights[height])
				count++
				if count == 1 {
					continue
				}
				totalSubTimeDiff += timeDiff
				fmt.Printf("RT newHeads sub is faster than ETH newHeads sub by: %s\n", timeDiff)
			}
		case err := <-realtimeSub.Err():
			t.Fatal(err)
		}
	}

	avgTimeDiff := time.Duration(int64(totalSubTimeDiff) / int64(Iterations-1))
	fmt.Printf("Avg RT newHeads sub is faster than ETH newHeads sub by: %s\n", avgTimeDiff)
}

func TestRealtimeBenchmarNewTransactionSubscription(t *testing.T) {
	ctx := context.Background()
	ec, err := ethclient.Dial(DefaultL2NetworkRealtimeURL)
	require.NoError(t, err)
	client := rtclient.NewRealtimeClient(ec, DefaultL2NetworkRealtimeURL)

	logger := logger.New()
	wsClient, err := rpc.Dial(DefaultL2NetworkWSURL, logger)
	require.NoError(t, err)

	// Default test address for tests that require an address
	testAddress := common.HexToAddress("0x1234567890123456789012345678901234567890")

	// Benchmark variables
	var totalRealtimeDuration time.Duration

	realtimeMsgCh := make(chan realtimeapi.RealtimeSubResult)
	realtimeSub, err := wsClient.Subscribe(ctx, "eth", realtimeMsgCh, "realtime", map[string]bool{"NewHeads": false, "TransactionExtraInfo": false, "TransactionReceipt": false, "TransactionInnerTxs": false})
	require.NoError(t, err)
	defer realtimeSub.Unsubscribe()

	for i := 0; i < Iterations; i++ {
		// Send tx
		signedTx := nativeTransferTx(t, ctx, client, uint256.NewInt(encoding.Gwei), testAddress.String())

		g, _ := errgroup.WithContext(ctx)
		var subDuration time.Duration

		// realtime subscription
		g.Go(func() error {
			startTime := time.Now()

			for {
				select {
				case msg := <-realtimeMsgCh:
					if msg.TxHash == signedTx.Hash().String() {
						subDuration = time.Since(startTime)
						return nil
					}
				case err := <-realtimeSub.Err():
					return err
				case <-time.After(DefaultTimeoutTxToBeMined):
					return fmt.Errorf("realtime subscription timeout")
				}
			}
		})

		// Wait for all goroutines to complete
		err = g.Wait()
		require.NoError(t, err)

		if i == 0 {
			continue
		}
		totalRealtimeDuration += subDuration

		fmt.Printf("Iteration %v:\n", i)
		fmt.Printf("RT newTx sub duration: %s\n", subDuration)
	}

	avgDuration := time.Duration(int64(totalRealtimeDuration) / int64(Iterations-1))

	// Log out metrics
	fmt.Printf("Avg RT newTx sub duration: %s\n", avgDuration)
}

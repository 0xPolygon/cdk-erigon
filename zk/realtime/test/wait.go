package test

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/holiman/uint256"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/test/operations"
	"github.com/ledgerwatch/erigon/zk/realtime/rtclient"
)

// WaitTxToBeMined waits until a tx has been mined or the given timeout expires.
func WaitTxToBeMined(parentCtx context.Context, client *rtclient.RealtimeClient, tx types.Transaction, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()
	receipt, err := WaitMined(ctx, client, tx.Hash())
	if errors.Is(err, context.DeadlineExceeded) {
		return err
	} else if err != nil {
		fmt.Printf("error waiting tx %s to be mined: %v\n", tx.Hash(), err)
		return err
	}
	if receipt.Status == types.ReceiptStatusFailed {
		// Get revert reason
		reason, reasonErr := RevertReasonRealtime(ctx, client, tx)
		if reasonErr != nil {
			reason = reasonErr.Error()
		}
		return fmt.Errorf("transaction has failed, reason: %s, receipt: %+v. tx: %+v, gas: %v", reason, receipt, tx, tx.GetGas())
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		currNum, err := client.BlockNumber(ctx)
		if err != nil {
			return err
		}

		if currNum >= receipt.BlockNumber.Uint64() {
			break
		}
	}

	fmt.Printf("Transaction successfully mined: %v\n", tx.Hash())
	return nil
}

// WaitRealtimeTxToBeConfirmed waits until a tx has been confirmed or the given timeout expires.
func WaitRealtimeTxToBeConfirmed(parentCtx context.Context, client *rtclient.RealtimeClient, tx types.Transaction, timeout time.Duration, toAddress common.Address, initialBalance *big.Int) error {
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()
	receipt, err := WaitMined(ctx, client, tx.Hash())
	if errors.Is(err, context.DeadlineExceeded) {
		return err
	} else if err != nil {
		fmt.Printf("error waiting tx %s to be confirmed: %v\n", tx.Hash(), err)
		return err
	}
	if receipt.Status == types.ReceiptStatusFailed {
		// Get revert reason
		reason, reasonErr := RevertReasonRealtime(ctx, client, tx)
		if reasonErr != nil {
			reason = reasonErr.Error()
		}
		return fmt.Errorf("transaction has failed, reason: %s, receipt: %+v. tx: %+v, gas: %v", reason, receipt, tx, tx.GetGas())
	}
	fmt.Printf("Realtime transaction successfully confirmed: %v\n", tx.Hash())

	// Ensure native balance is updated
	for {
		balance, err := client.RealtimeGetBalance(toAddress)
		if err != nil {
			return err
		}
		if balance.Cmp(initialBalance) != 0 {
			return nil
		}
		// Wait for the next round.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// With the default block time set at 1s, 5ms of sleep is a good enough threshold
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// WaitEthTxToBeConfirmed waits until a tx has been confirmed or the given timeout expires.
func WaitEthTxToBeConfirmed(parentCtx context.Context, client ethClienter, tx types.Transaction, timeout time.Duration, toAddress common.Address, initialBalance *uint256.Int) error {
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()
	receipt, err := WaitMined(ctx, client, tx.Hash())
	if errors.Is(err, context.DeadlineExceeded) {
		return err
	} else if err != nil {
		fmt.Printf("error waiting tx %s to be mined: %v\n", tx.Hash(), err)
		return err
	}
	if receipt.Status == types.ReceiptStatusFailed {
		// Get revert reason
		reason, reasonErr := operations.RevertReason(ctx, client, tx, receipt.BlockNumber)
		if reasonErr != nil {
			reason = reasonErr.Error()
		}
		return fmt.Errorf("transaction has failed, reason: %s, receipt: %+v. tx: %+v, gas: %v", reason, receipt, tx, tx.GetGas())
	}
	fmt.Printf("Eth transaction successfully confirmed: %v\n", tx.Hash())

	// Ensure native balance is updated
	for {
		balance, err := client.BalanceAt(ctx, toAddress, nil)
		if err != nil {
			return err
		}
		if balance.Cmp(initialBalance) != 0 {
			return nil
		}
		// Wait for the next round.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// With the default block time set at 1s, 5ms of sleep is a good enough threshold
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// WaitRealtimeErc20TxToBeConfirmed waits until an erc20 tx has been confirmed or the given timeout expires.
func WaitRealtimeErc20TxToBeConfirmed(parentCtx context.Context, client *rtclient.RealtimeClient, tx types.Transaction, timeout time.Duration, fromAddress, toAddress common.Address, initialBalance *big.Int) error {
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()
	receipt, err := WaitMined(ctx, client, tx.Hash())
	if errors.Is(err, context.DeadlineExceeded) {
		return err
	} else if err != nil {
		fmt.Printf("error waiting tx %s to be confirmed: %v\n", tx.Hash(), err)
		return err
	}
	if receipt.Status == types.ReceiptStatusFailed {
		// Get revert reason
		reason, reasonErr := RevertReasonRealtime(ctx, client, tx)
		if reasonErr != nil {
			reason = reasonErr.Error()
		}
		return fmt.Errorf("transaction has failed, reason: %s, receipt: %+v. tx: %+v, gas: %v", reason, receipt, tx, tx.GetGas())
	}
	fmt.Printf("Realtime transaction successfully confirmed: %v\n", tx.Hash())

	// Ensure erc20 balance is updated
	for {
		// Get the receiver address from the transaction
		erc20Address := *tx.GetTo()
		if erc20Address == (common.Address{}) {
			return fmt.Errorf("invalid contract address")
		}

		balance, err := client.RealtimeGetTokenBalance(fromAddress, toAddress, erc20Address)
		if err != nil {
			return err
		}

		// Check if balance matches expected value
		if balance.Cmp(initialBalance) != 0 {
			return nil
		}
		// Wait for the next round.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

// WaitEthErc20TxToBeConfirmed waits until an erc20 tx has been confirmed or the given timeout expires.
func WaitEthErc20TxToBeConfirmed(parentCtx context.Context, client ethClienter, tx types.Transaction, timeout time.Duration, toAddress common.Address, initialBalance *big.Int) error {
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()
	receipt, err := WaitMined(ctx, client, tx.Hash())
	if errors.Is(err, context.DeadlineExceeded) {
		return err
	} else if err != nil {
		fmt.Printf("error waiting tx %s to be mined: %v\n", tx.Hash(), err)
		return err
	}
	if receipt.Status == types.ReceiptStatusFailed {
		// Get revert reason
		reason, reasonErr := operations.RevertReason(ctx, client, tx, receipt.BlockNumber)
		if reasonErr != nil {
			reason = reasonErr.Error()
		}
		return fmt.Errorf("transaction has failed, reason: %s, receipt: %+v. tx: %+v, gas: %v", reason, receipt, tx, tx.GetGas())
	}
	fmt.Printf("Eth transaction successfully confirmed: %v\n", tx.Hash())

	// Ensure erc20 balance is updated
	for {
		// Get the receiver address from the transaction
		erc20Address := *tx.GetTo()
		if erc20Address == (common.Address{}) {
			return fmt.Errorf("invalid contract address")
		}

		balance, err := GetErc20Balance(ctx, client, toAddress, erc20Address, nil)
		if err != nil {
			return err
		}

		// Check if balance matches expected value
		if balance.Cmp(initialBalance) != 0 {
			return nil
		}
		// Wait for the next round.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

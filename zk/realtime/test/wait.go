package test

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	ethereum "github.com/ledgerwatch/erigon"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/accounts/abi/bind"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/zkevm/log"
)

type ethClienter interface {
	ethereum.TransactionReader
	ethereum.ContractCaller
	bind.DeployBackend
}

func WaitCallback(
	parentCtx context.Context,
	client ethClienter,
	tx types.Transaction,
	fromAddress common.Address,
	toAddress common.Address,
	balance *big.Int,
	timeout time.Duration,
	callback func(context.Context, ethClienter, types.Transaction, common.Address, common.Address, *big.Int) error,
) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	timeStart := time.Now()
	err := callback(ctx, client, tx, fromAddress, toAddress, balance)
	if err != nil {
		return time.Since(timeStart), err
	}

	return time.Since(timeStart), nil
}

func WaitMinedRealtime(ctx context.Context, client ethClienter, tx types.Transaction, _, _ common.Address, _ *big.Int) error {
	for {
		receipt, err := RealtimeGetTransactionReceipt(tx.Hash())
		if err == nil && receipt != nil {
			if receipt.Status == types.ReceiptStatusFailed {
				// Get revert reason
				reason, reasonErr := RevertReason(ctx, client, tx, receipt.BlockNumber)
				if reasonErr != nil {
					reason = reasonErr.Error()
				}
				return fmt.Errorf("transaction has failed, reason: %s, receipt: %+v. tx: %+v, gas: %v", reason, receipt, tx, tx.GetGas())
			}
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

func WaitMinedEth(ctx context.Context, client ethClienter, tx types.Transaction, _, _ common.Address, _ *big.Int) error {
	for {
		receipt, err := client.TransactionReceipt(ctx, tx.Hash())
		if err == nil && receipt != nil {
			if receipt.Status == types.ReceiptStatusFailed {
				// Get revert reason
				reason, reasonErr := RevertReason(ctx, client, tx, receipt.BlockNumber)
				if reasonErr != nil {
					reason = reasonErr.Error()
				}
				return fmt.Errorf("transaction has failed, reason: %s, receipt: %+v. tx: %+v, gas: %v", reason, receipt, tx, tx.GetGas())
			}
			return nil
		}
		// Wait for the next round.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func WaitAccountBalanceRealtime(ctx context.Context, client ethClienter, tx types.Transaction, _, toAddress common.Address, balance *big.Int) error {
	for {
		realtimeBalance, err := RealtimeGetBalance(toAddress)
		if err != nil {
			return err
		}
		if realtimeBalance.Cmp(balance) != 0 {
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

func WaitAccountBalanceEth(ctx context.Context, client ethClienter, tx types.Transaction, _, toAddress common.Address, balance *big.Int) error {
	for {
		ethBalance, err := EthGetBalance(toAddress, "latest")
		if err != nil {
			return err
		}
		if ethBalance.Cmp(balance) != 0 {
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

func WaitTokenBalanceRealtime(ctx context.Context, client ethClienter, tx types.Transaction, fromAddress, toAddress common.Address, tokenBalance *big.Int) error {
	for {
		// Get the receiver address from the transaction
		erc20Address := *tx.GetTo()
		if erc20Address == (common.Address{}) {
			return fmt.Errorf("invalid contract address")
		}

		rpcBalance, err := RealtimeGetTokenBalance(ctx, client, fromAddress, toAddress, erc20Address)
		if err != nil {
			return err
		}

		// Check if balance matches expected value
		if rpcBalance.Cmp(tokenBalance) != 0 {
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

func WaitTokenBalanceEth(ctx context.Context, client ethClienter, tx types.Transaction, _, toAddress common.Address, tokenBalance *big.Int) error {
	for {
		// Get the receiver address from the transaction
		erc20Address := *tx.GetTo()
		if erc20Address == (common.Address{}) {
			return fmt.Errorf("invalid contract address")
		}

		rpcBalance, err := EthGetTokenBalance(ctx, client, toAddress, erc20Address)
		if err != nil {
			return err
		}

		// Check if balance matches expected value
		if rpcBalance.Cmp(tokenBalance) != 0 {
			return nil
		}

		// Wait for the next round
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// WaitTxToBeMined waits until a tx has been mined or the given timeout expires.
func WaitTxToBeMined(parentCtx context.Context, client ethClienter, tx types.Transaction, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()
	receipt, err := bind.WaitMined(ctx, client, tx)
	if errors.Is(err, context.DeadlineExceeded) {
		return err
	} else if err != nil {
		log.Error(fmt.Sprintf("error waiting tx %s to be mined. error: %v", tx.Hash(), err))
		return err
	}
	if receipt.Status == types.ReceiptStatusFailed {
		// Get revert reason
		reason, reasonErr := RevertReason(ctx, client, tx, receipt.BlockNumber)
		if reasonErr != nil {
			reason = reasonErr.Error()
		}
		return fmt.Errorf("transaction has failed, reason: %s, receipt: %+v. tx: %+v, gas: %v", reason, receipt, tx, tx.GetGas())
	}
	log.Debug(fmt.Sprintf("Transaction successfully mined: %v", tx.Hash()))
	return nil
}

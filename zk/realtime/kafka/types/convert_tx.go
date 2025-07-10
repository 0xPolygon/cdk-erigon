package types

import (
	"fmt"
	"math/big"

	"github.com/holiman/uint256"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	types2 "github.com/ledgerwatch/erigon-lib/types"
	types1 "github.com/ledgerwatch/erigon/core/types"
)

func fromCommonTxMessage(tx types1.Transaction, blockNumber uint64) (TransactionMessage, error) {
	msg := TransactionMessage{
		BlockNumber: blockNumber,
		Type:        tx.Type(),
		Hash:        tx.Hash(),
		ChainID:     tx.GetChainID(),
		Nonce:       tx.GetNonce(),
		Gas:         tx.GetGas(),
		To:          tx.GetTo(),
		Value:       tx.GetValue(),
		Data:        tx.GetData(),
	}

	txSender, ok := tx.GetSender()
	if !ok {
		return TransactionMessage{}, fmt.Errorf("failed to recover sender from transaction")
	}
	msg.From = txSender

	v, r, s := tx.RawSignatureValues()
	msg.V = *v
	msg.R = *r
	msg.S = *s

	return msg, nil
}

func fromLegacyTxMessage(tx types1.Transaction, blockNumber uint64) (TransactionMessage, error) {
	msg, err := fromCommonTxMessage(tx, blockNumber)
	if err != nil {
		return TransactionMessage{}, err
	}

	msg.GasPrice = tx.GetPrice().String()

	return msg, nil
}

func fromAccessListTxMessage(tx types1.Transaction, blockNumber uint64) (TransactionMessage, error) {
	msg, err := fromLegacyTxMessage(tx, blockNumber)
	if err != nil {
		return TransactionMessage{}, err
	}

	accessList := tx.GetAccessList()
	msg.AccessList = make([]AccessTupleMessage, 0, len(accessList))
	for _, tuple := range accessList {
		msg.AccessList = append(msg.AccessList, fromAccessTuple(tuple))
	}

	return msg, nil
}

func fromDynamicFeeTxMessage(tx types1.Transaction, blockNumber uint64) (TransactionMessage, error) {
	msg, err := fromCommonTxMessage(tx, blockNumber)
	if err != nil {
		return TransactionMessage{}, err
	}

	accessList := tx.GetAccessList()
	msg.AccessList = make([]AccessTupleMessage, 0, len(accessList))
	for _, tuple := range accessList {
		msg.AccessList = append(msg.AccessList, fromAccessTuple(tuple))
	}

	msg.Tip = tx.GetTip().String()
	msg.FeeCap = tx.GetFeeCap().String()

	return msg, nil
}

func fromBlobTxMessage(tx types1.Transaction, blockNumber uint64) (TransactionMessage, error) {
	// Check if it's a BlobTx or BlobTxWrapper
	msg, err := fromDynamicFeeTxMessage(tx, blockNumber)
	if err != nil {
		return TransactionMessage{}, err
	}

	switch t := tx.(type) {
	case *types1.BlobTx:
		msg.MaxFeePerBlobGas = t.MaxFeePerBlobGas.String()
		msg.BlobVersionedHashes = make([]string, 0, len(t.BlobVersionedHashes))
		for _, hash := range t.BlobVersionedHashes {
			msg.BlobVersionedHashes = append(msg.BlobVersionedHashes, hash.String())
		}
	case *types1.BlobTxWrapper:
		msg.MaxFeePerBlobGas = t.Tx.MaxFeePerBlobGas.String()
		msg.BlobVersionedHashes = make([]string, 0, len(t.Tx.BlobVersionedHashes))
		for _, hash := range t.Tx.BlobVersionedHashes {
			msg.BlobVersionedHashes = append(msg.BlobVersionedHashes, hash.String())
		}
	default:
		return TransactionMessage{}, fmt.Errorf("unsupported transaction type: %d", tx.Type())
	}

	return msg, nil
}

func (msg TransactionMessage) toCommonTx() (types1.CommonTx, error) {
	tx := types1.CommonTx{
		TransactionMisc: types1.TransactionMisc{},
		ChainID:         msg.ChainID,
		Nonce:           msg.Nonce,
		Gas:             msg.Gas,
		To:              msg.To,
		Value:           msg.Value,
		Data:            msg.Data,
		V:               msg.V,
		R:               msg.R,
		S:               msg.S,
	}

	// Set sender address
	tx.SetSender(msg.From)

	return tx, nil
}

func (msg TransactionMessage) toLegacyTx() (types1.LegacyTx, error) {
	commonTx, err := msg.toCommonTx()
	if err != nil {
		err = fmt.Errorf("convert to legacy tx error: %w", err)
		return types1.LegacyTx{}, err
	}

	tx := types1.LegacyTx{
		CommonTx: commonTx,
	}

	// Set gas price
	gasPrice, ok := new(big.Int).SetString(msg.GasPrice, 10)
	if !ok {
		return types1.LegacyTx{}, fmt.Errorf("convert to legacy tx error, invalid gas price : %s", msg.GasPrice)
	}
	overflow := false
	tx.GasPrice, overflow = uint256.FromBig(gasPrice)
	if overflow {
		return types1.LegacyTx{}, fmt.Errorf("convert to legacy tx error, gasprice overflow: %s", msg.Value)
	}

	return tx, nil
}

func (msg TransactionMessage) toAccessListTx() (types1.AccessListTx, error) {
	legacyTx, err := msg.toLegacyTx()
	if err != nil {
		err = fmt.Errorf("convert to access list tx error: %w", err)
		return types1.AccessListTx{}, err
	}

	tx := types1.AccessListTx{
		LegacyTx: legacyTx,
		ChainID:  msg.ChainID,
	}

	// Set access list
	tx.AccessList = make([]types2.AccessTuple, 0, len(msg.AccessList))
	for _, tuple := range msg.AccessList {
		tx.AccessList = append(tx.AccessList, tuple.toAccessTuple())
	}

	return tx, nil
}

func (msg TransactionMessage) toDynamicFeeTx() (types1.DynamicFeeTransaction, error) {
	commonTx, err := msg.toCommonTx()
	if err != nil {
		err = fmt.Errorf("convert to dynamic fee tx error: %w", err)
		return types1.DynamicFeeTransaction{}, err
	}

	tx := types1.DynamicFeeTransaction{
		CommonTx: commonTx,
		ChainID:  msg.ChainID,
	}

	// Set access list
	tx.AccessList = make([]types2.AccessTuple, 0, len(msg.AccessList))
	for _, tuple := range msg.AccessList {
		tx.AccessList = append(tx.AccessList, tuple.toAccessTuple())
	}

	// Set tip
	tip, ok := new(big.Int).SetString(msg.Tip, 10)
	if !ok {
		return types1.DynamicFeeTransaction{}, fmt.Errorf("convert to dynamic fee tx error, invalid gas price : %s", msg.GasPrice)
	}
	overflow := false
	tx.Tip, overflow = uint256.FromBig(tip)
	if overflow {
		return types1.DynamicFeeTransaction{}, fmt.Errorf("convert to dynamic fee tx error, tip overflow: %s", msg.Tip)
	}

	// Set fee cap
	feeCap, ok := new(big.Int).SetString(msg.FeeCap, 10)
	if !ok {
		return types1.DynamicFeeTransaction{}, fmt.Errorf("convert to dynamic fee tx error, invalid gas price : %s", msg.GasPrice)
	}
	overflow = false
	tx.FeeCap, overflow = uint256.FromBig(feeCap)
	if overflow {
		return types1.DynamicFeeTransaction{}, fmt.Errorf("convert to dynamic fee tx error, fee cap overflow: %s", msg.FeeCap)
	}

	return tx, nil
}

func (msg TransactionMessage) toBlobTx() (types1.BlobTx, error) {
	dynamicFeeTx, err := msg.toDynamicFeeTx()
	if err != nil {
		err = fmt.Errorf("convert to blob tx error: %w", err)
		return types1.BlobTx{}, err
	}

	tx := types1.BlobTx{
		DynamicFeeTransaction: dynamicFeeTx,
	}

	// Set max fee per blob gas
	maxFeePerBlobGas, ok := new(big.Int).SetString(msg.MaxFeePerBlobGas, 10)
	if !ok {
		return types1.BlobTx{}, fmt.Errorf("convert to blob tx error, invalid max fee per blob gas : %s", msg.MaxFeePerBlobGas)
	}
	overflow := false
	tx.MaxFeePerBlobGas, overflow = uint256.FromBig(maxFeePerBlobGas)
	if overflow {
		return types1.BlobTx{}, fmt.Errorf("convert to blob tx error, max fee per blob gas overflow: %s", msg.MaxFeePerBlobGas)
	}

	// Set blob versioned hashes
	tx.BlobVersionedHashes = make([]libcommon.Hash, 0, len(msg.BlobVersionedHashes))
	for _, hash := range msg.BlobVersionedHashes {
		tx.BlobVersionedHashes = append(tx.BlobVersionedHashes, libcommon.HexToHash(hash))
	}

	return tx, nil
}

package types

import (
	"encoding/json"
	"fmt"

	"github.com/holiman/uint256"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	ethTypes "github.com/ledgerwatch/erigon/core/types"
	realtimeTypes "github.com/ledgerwatch/erigon/zk/realtime/types"
	zktypes "github.com/ledgerwatch/erigon/zk/types"
)

// TransactionMessage represents the structure of the transaction message to be sent to Kafka
type TransactionMessage struct {
	// Sequenced block number
	BlockNumber uint64 `json:"blockNumber"`
	BlockTime   uint64 `json:"blockTime"`

	// Common tx fields
	Type    uint8              `json:"type"`
	Hash    libcommon.Hash     `json:"hash"`
	From    libcommon.Address  `json:"from"`
	ChainID *uint256.Int       `json:"chainId"`
	Nonce   uint64             `json:"nonce"`
	Gas     uint64             `json:"gas"`
	To      *libcommon.Address `json:"to"`
	Value   *uint256.Int       `json:"value"`
	Data    []byte             `json:"data"`
	V       uint256.Int        `json:"v"`
	R       uint256.Int        `json:"r"`
	S       uint256.Int        `json:"s"`

	// For legacy txs
	GasPrice string `json:"gasPrice"`
	// For EIP-1559 and EIP-2930 txs
	AccessList []AccessTupleMessage `json:"accessList"`
	Tip        string               `json:"tip"`
	FeeCap     string               `json:"feeCap"`
	// For blob txs
	MaxFeePerBlobGas    string   `json:"maxFeePerBlobGas"`
	BlobVersionedHashes []string `json:"blobVersionedHashes"`

	// Receipt data
	Receipt *ethTypes.Receipt `json:"receipt"`

	// Inner transactions
	InnerTxs []*zktypes.InnerTx `json:"innerTxs"`

	// Changeset
	Changeset *realtimeTypes.Changeset `json:"changeset"`
}

func ToKafkaTransactionMessage(tx ethTypes.Transaction, receipt *ethTypes.Receipt, innerTxs []*zktypes.InnerTx, changeset *realtimeTypes.Changeset, blockNumber uint64, blockTime uint64) (txMsg TransactionMessage, err error) {
	// Parse tx
	if tx == nil || receipt == nil || innerTxs == nil || changeset == nil {
		return TransactionMessage{}, fmt.Errorf("nil tx data received")
	}

	switch tx.Type() {
	case ethTypes.LegacyTxType:
		if _, ok := tx.(*ethTypes.LegacyTx); !ok {
			return TransactionMessage{}, fmt.Errorf("incorrect type, failed to encode legacy tx")
		}

		txMsg, err = fromLegacyTxMessage(tx, blockNumber)
		if err != nil {
			return TransactionMessage{}, fmt.Errorf("parse legacy tx error: %w", err)
		}
	case ethTypes.AccessListTxType:
		if _, ok := tx.(*ethTypes.AccessListTx); !ok {
			return TransactionMessage{}, fmt.Errorf("incorrect type, failed to encode access list tx")
		}

		txMsg, err = fromAccessListTxMessage(tx, blockNumber)
		if err != nil {
			return TransactionMessage{}, fmt.Errorf("parse accesslist tx error: %w", err)
		}
	case ethTypes.DynamicFeeTxType:
		if _, ok := tx.(*ethTypes.DynamicFeeTransaction); !ok {
			return TransactionMessage{}, fmt.Errorf("incorrect type, failed to encode dynamic fee tx")
		}

		txMsg, err = fromDynamicFeeTxMessage(tx, blockNumber)
		if err != nil {
			return TransactionMessage{}, fmt.Errorf("parse dynamic fee tx error: %w", err)
		}
	case ethTypes.BlobTxType:
		switch tx.(type) {
		case *ethTypes.BlobTx:
			// continue
		case *ethTypes.BlobTxWrapper:
			// continue
		default:
			return TransactionMessage{}, fmt.Errorf("incorrect type, failed to encode blob tx")
		}

		txMsg, err = fromBlobTxMessage(tx, blockNumber)
		if err != nil {
			return TransactionMessage{}, fmt.Errorf("parse blob tx error: %w", err)
		}
	default:
		return TransactionMessage{}, fmt.Errorf("unsupported transaction type: %d", tx.Type())
	}

	// Parse receipt
	txMsg.Receipt = receipt
	txMsg.InnerTxs = innerTxs
	txMsg.Changeset = changeset
	txMsg.BlockTime = blockTime

	return txMsg, nil
}

func (msg TransactionMessage) GetTransaction() (ethTypes.Transaction, uint64, error) {
	blockNumber := msg.BlockNumber

	// Get tx
	switch msg.Type {
	case ethTypes.LegacyTxType:
		tx, err := msg.toLegacyTx()
		if err != nil {
			return nil, blockNumber, err
		}

		return &tx, blockNumber, nil
	case ethTypes.AccessListTxType:
		tx, err := msg.toAccessListTx()
		if err != nil {
			return nil, blockNumber, err
		}

		return &tx, blockNumber, nil
	case ethTypes.DynamicFeeTxType:
		tx, err := msg.toDynamicFeeTx()
		if err != nil {
			return nil, blockNumber, err
		}

		return &tx, blockNumber, nil
	case ethTypes.BlobTxType:
		tx, err := msg.toBlobTx()
		if err != nil {
			return nil, blockNumber, err
		}

		return &tx, blockNumber, nil
	default:
		return nil, blockNumber, fmt.Errorf("unsupported transaction type: %d", msg.Type)
	}
}

func (msg TransactionMessage) GetReceipt() (*ethTypes.Receipt, error) {
	if msg.Receipt == nil {
		return nil, fmt.Errorf("receipt is nil")
	}

	return msg.Receipt, nil
}

func (msg TransactionMessage) GetInnerTxs() ([]*zktypes.InnerTx, error) {
	if msg.InnerTxs == nil {
		return nil, fmt.Errorf("innerTxs is nil")
	}

	return msg.InnerTxs, nil
}

func (msg TransactionMessage) GetAllTxData() (uint64, ethTypes.Transaction, *ethTypes.Receipt, []*zktypes.InnerTx, error) {
	tx, blockNum, err := msg.GetTransaction()
	if err != nil {
		return 0, nil, nil, nil, err
	}
	receipt, err := msg.GetReceipt()
	if err != nil {
		return 0, nil, nil, nil, err
	}
	innerTxs, err := msg.GetInnerTxs()
	if err != nil {
		return 0, nil, nil, nil, err
	}

	return blockNum, tx, receipt, innerTxs, nil
}

func (msg TransactionMessage) GetChangeset() (*realtimeTypes.Changeset, error) {
	if msg.Changeset == nil {
		return nil, fmt.Errorf("changeset is nil")
	}

	return msg.Changeset, nil
}

func (msg TransactionMessage) Validate() error {
	if _, _, err := msg.GetTransaction(); err != nil {
		return err
	}
	if _, err := msg.GetReceipt(); err != nil {
		return err
	}
	if _, err := msg.GetInnerTxs(); err != nil {
		return err
	}
	if _, err := msg.GetChangeset(); err != nil {
		return err
	}

	return nil
}

func (msg TransactionMessage) MarshalJSON() ([]byte, error) {
	type TransactionMessage struct {
		BlockNumber         uint64                   `json:"blockNumber"`
		BlockTime           uint64                   `json:"blockTime"`
		Type                uint8                    `json:"type"`
		Hash                libcommon.Hash           `json:"hash"`
		From                libcommon.Address        `json:"from"`
		ChainID             *uint256.Int             `json:"chainId"`
		Nonce               uint64                   `json:"nonce"`
		Gas                 uint64                   `json:"gas"`
		To                  *libcommon.Address       `json:"to"`
		Value               *uint256.Int             `json:"value"`
		Data                []byte                   `json:"data"`
		V                   uint256.Int              `json:"v"`
		R                   uint256.Int              `json:"r"`
		S                   uint256.Int              `json:"s"`
		GasPrice            string                   `json:"gasPrice"`
		AccessList          []AccessTupleMessage     `json:"accessList"`
		Tip                 string                   `json:"tip"`
		FeeCap              string                   `json:"feeCap"`
		MaxFeePerBlobGas    string                   `json:"maxFeePerBlobGas"`
		BlobVersionedHashes []string                 `json:"blobVersionedHashes"`
		Receipt             *ethTypes.Receipt        `json:"receipt"`
		InnerTxs            []*zktypes.InnerTx       `json:"innerTxs"`
		Changeset           *realtimeTypes.Changeset `json:"changeset"`
	}

	var enc TransactionMessage
	enc.BlockNumber = msg.BlockNumber
	enc.BlockTime = msg.BlockTime
	enc.Type = msg.Type
	enc.Hash = msg.Hash
	enc.From = msg.From
	enc.ChainID = msg.ChainID
	enc.Nonce = msg.Nonce
	enc.Gas = msg.Gas
	enc.To = msg.To
	enc.Value = msg.Value
	enc.Data = msg.Data
	enc.R = msg.R
	enc.S = msg.S
	enc.V = msg.V
	enc.GasPrice = msg.GasPrice
	enc.AccessList = msg.AccessList
	enc.Tip = msg.Tip
	enc.FeeCap = msg.FeeCap
	enc.MaxFeePerBlobGas = msg.MaxFeePerBlobGas
	enc.BlobVersionedHashes = msg.BlobVersionedHashes
	enc.InnerTxs = msg.InnerTxs
	enc.Changeset = msg.Changeset

	if msg.Receipt != nil {
		// Handle nil logs
		receipt := *msg.Receipt
		if receipt.Logs == nil {
			receipt.Logs = []*ethTypes.Log{}
		}

		// Handle nil topics
		for _, log := range receipt.Logs {
			if log != nil {
				if log.Topics == nil {
					log.Topics = []libcommon.Hash{}
				}
			}
		}
		enc.Receipt = &receipt
	}

	return json.Marshal(&enc)
}

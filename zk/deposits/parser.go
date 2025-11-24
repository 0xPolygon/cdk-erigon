package deposits

import (
	"fmt"

	libcommon "github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/crypto"
	"github.com/erigontech/erigon/core/types"
	"github.com/holiman/uint256"
)

var (
	DepositEventABI     = "TransactionDeposited(address,address,uint256,bytes)"
	DepositEventABIHash = crypto.Keccak256Hash([]byte(DepositEventABI))
)

// UnmarshalDepositLog decodes a TransactionDeposited log into a Deposit payload.
func UnmarshalDepositLog(ev *types.Log) (*Deposit, error) {
	if len(ev.Topics) != 4 {
		return nil, fmt.Errorf("expected 4 topics, got %d", len(ev.Topics))
	}
	if libcommon.Hash(ev.Topics[0]) != libcommon.Hash(DepositEventABIHash) {
		return nil, fmt.Errorf("invalid selector")
	}
	if len(ev.Data) < 64 || len(ev.Data)%32 != 0 {
		return nil, fmt.Errorf("invalid data length %d", len(ev.Data))
	}

	from := libcommon.BytesToAddress(ev.Topics[1][12:])
	to := libcommon.BytesToAddress(ev.Topics[2][12:])
	_ = ev.Topics[3] // version; only version 0 supported

	// opaqueData offset/len
	var offset uint256.Int
	offset.SetBytes(ev.Data[0:32])
	if !offset.IsUint64() || offset.Uint64() != 32 {
		return nil, fmt.Errorf("invalid opaqueData offset")
	}
	var length uint256.Int
	length.SetBytes(ev.Data[32:64])
	if !length.IsUint64() || length.Uint64() > uint64(len(ev.Data)-64) {
		return nil, fmt.Errorf("invalid opaqueData length")
	}
	opaque := ev.Data[64 : 64+length.Uint64()]
	if len(opaque) < 32+32+8+1 {
		return nil, fmt.Errorf("opaqueData too short")
	}
	pos := 0
	mint := libcommon.BytesToHash(opaque[pos : pos+32]).Big()
	pos += 32
	value := libcommon.BytesToHash(opaque[pos : pos+32]).Big()
	pos += 32
	gas := new(uint256.Int).SetBytes(opaque[pos : pos+8])
	pos += 8
	isCreation := opaque[pos] == 1
	pos++
	data := opaque[pos:]

	var toPtr *libcommon.Address
	if !isCreation {
		tmp := to
		toPtr = &tmp
	}

	srcHash := computeSourceHash(ev.BlockHash, uint64(ev.Index))

	return &Deposit{
		From:       from,
		To:         toPtr,
		Mint:       mint,
		Value:      value,
		Gas:        gas.Uint64(),
		IsCreation: isCreation,
		Data:       data,
		SourceHash: srcHash,
		Log:        *ev,
	}, nil
}

func computeSourceHash(blockHash libcommon.Hash, logIndex uint64) libcommon.Hash {
	var idxBytes [8]byte
	for i := 0; i < 8; i++ {
		idxBytes[7-i] = byte(logIndex >> (8 * i))
	}
	return crypto.Keccak256Hash(blockHash.Bytes(), idxBytes[:])
}

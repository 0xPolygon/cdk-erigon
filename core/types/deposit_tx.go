package types

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/big"

	"github.com/holiman/uint256"

	"github.com/erigontech/erigon-lib/chain"
	libcommon "github.com/erigontech/erigon-lib/common"
	rlp2 "github.com/erigontech/erigon-lib/rlp"
	types2 "github.com/erigontech/erigon-lib/types"
)

// DepositTx represents a forced L2 transaction derived from an L1 TransactionDeposited log.
type DepositTx struct {
	TransactionMisc

	SourceHash libcommon.Hash
	From       libcommon.Address
	To         *libcommon.Address
	Mint       uint256.Int
	Value      uint256.Int
	Gas        uint64
	Data       []byte
	IsCreation bool

	gasPrice uint256.Int
	feeCap   uint256.Int
	tip      uint256.Int
}

func NewDepositTx(
	sourceHash libcommon.Hash,
	from libcommon.Address,
	to *libcommon.Address,
	mint *uint256.Int,
	value *uint256.Int,
	gas uint64,
	isCreation bool,
	data []byte,
) (*DepositTx, error) {
	if mint == nil || value == nil {
		return nil, errors.New("mint/value must not be nil")
	}
	tx := &DepositTx{
		SourceHash: sourceHash,
		From:       from,
		Gas:        gas,
		Data:       libcommon.CopyBytes(data),
		IsCreation: isCreation,
	}
	tx.Mint.Set(mint)
	tx.Value.Set(value)
	if to != nil && !isCreation {
		addr := *to
		tx.To = &addr
	}
	return tx, nil
}

func (tx *DepositTx) Type() byte { return DepositTxType }

func (tx *DepositTx) GetChainID() *uint256.Int { return nil }

func (tx *DepositTx) GetNonce() uint64 { return 0 }

func (tx *DepositTx) GetPrice() *uint256.Int { return &tx.gasPrice }

func (tx *DepositTx) GetTip() *uint256.Int { return &tx.tip }

func (tx *DepositTx) GetEffectiveGasTip(_ *uint256.Int) *uint256.Int { return &tx.tip }

func (tx *DepositTx) GetFeeCap() *uint256.Int { return &tx.feeCap }

func (tx *DepositTx) GetBlobHashes() []libcommon.Hash { return nil }

func (tx *DepositTx) GetGas() uint64 { return tx.Gas }

func (tx *DepositTx) GetBlobGas() uint64 { return 0 }

func (tx *DepositTx) GetValue() *uint256.Int { return &tx.Value }

func (tx *DepositTx) GetTo() *libcommon.Address { return tx.To }

func (tx *DepositTx) GetData() []byte { return tx.Data }

func (tx *DepositTx) GetAccessList() types2.AccessList { return nil }

func (tx *DepositTx) Protected() bool { return false }

func (tx *DepositTx) IsContractDeploy() bool { return tx.To == nil }

func (tx *DepositTx) Unwrap() Transaction { return tx }

func (tx *DepositTx) Sender(_ Signer) (libcommon.Address, error) {
	if from := tx.from.Load(); from != nil {
		return *from, nil
	}
	tx.from.Store(&tx.From)
	return tx.From, nil
}

func (tx *DepositTx) SetSender(addr libcommon.Address) {
	tx.from.Store(&addr)
}

func (tx *DepositTx) Hash() libcommon.Hash {
	if hash := tx.hash.Load(); hash != nil {
		return *hash
	}
	hash := tx.SourceHash
	tx.hash.Store(&hash)
	return hash
}

func (tx *DepositTx) SigningHash(_ *big.Int) libcommon.Hash {
	return tx.Hash()
}

func (tx *DepositTx) RawSignatureValues() (*uint256.Int, *uint256.Int, *uint256.Int) {
	return &tx.gasPrice, &tx.feeCap, &tx.tip
}

func (tx *DepositTx) AsMessage(_ Signer, _ *big.Int, _ *chain.Rules) (Message, error) {
	msg := Message{
		gasLimit:   tx.Gas,
		to:         tx.To,
		amount:     tx.Value,
		data:       tx.Data,
		checkNonce: false,
		isFree:     true,
	}
	msg.from = tx.From
	return msg, nil
}

func (tx *DepositTx) WithSignature(_ Signer, _ []byte) (Transaction, error) {
	return nil, ErrTxTypeNotSupported
}

func (tx *DepositTx) MarshalBinary(w io.Writer) error {
	if _, err := w.Write([]byte{DepositTxType}); err != nil {
		return err
	}
	return tx.encodePayload(w)
}

func (tx *DepositTx) EncodeRLP(w io.Writer) error {
	var buf bytes.Buffer
	if err := tx.MarshalBinary(&buf); err != nil {
		return err
	}
	var b [33]byte
	if err := rlp2.EncodeStringSizePrefix(buf.Len(), w, b[:]); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

func (tx *DepositTx) encodePayload(w io.Writer) error {
	return rlp2.Encode(w, []interface{}{
		tx.SourceHash,
		tx.From,
		tx.To,
		tx.Mint.ToBig(),
		tx.Value.ToBig(),
		tx.Gas,
		tx.IsCreation,
		tx.Data,
	})
}

func (tx *DepositTx) DecodeRLP(s *rlp2.Stream) error {
	payload, err := s.Bytes()
	if err != nil {
		return err
	}
	if len(payload) == 0 {
		return errors.New("empty deposit transaction payload")
	}
	if payload[0] != DepositTxType {
		return fmt.Errorf("unexpected tx type %d for deposit tx", payload[0])
	}
	stream := rlp2.NewStream(bytes.NewReader(payload[1:]), uint64(len(payload)-1))
	if err := tx.decodePayload(stream); err != nil {
		return err
	}
	return nil
}

func (tx *DepositTx) decodePayload(s *rlp2.Stream) error {
	if _, err := s.List(); err != nil {
		return err
	}
	if b, err := s.Bytes(); err != nil {
		return err
	} else if len(b) != len(tx.SourceHash) {
		return fmt.Errorf("invalid source hash length %d", len(b))
	} else {
		copy(tx.SourceHash[:], b)
	}
	if b, err := s.Bytes(); err != nil {
		return err
	} else if len(b) != len(tx.From) {
		return fmt.Errorf("invalid from address length %d", len(b))
	} else {
		copy(tx.From[:], b)
	}
	if b, err := s.Bytes(); err != nil {
		return err
	} else if len(b) == 0 {
		tx.To = nil
	} else if len(b) != len(libcommon.Address{}) {
		return fmt.Errorf("invalid to address length %d", len(b))
	} else {
		addr := libcommon.Address{}
		copy(addr[:], b)
		tx.To = &addr
	}
	if b, err := s.Uint256Bytes(); err != nil {
		return err
	} else {
		tx.Mint.SetBytes(b)
	}
	if b, err := s.Uint256Bytes(); err != nil {
		return err
	} else {
		tx.Value.SetBytes(b)
	}
	gas, err := s.Uint()
	if err != nil {
		return err
	}
	tx.Gas = gas
	if tx.IsCreation, err = s.Bool(); err != nil {
		return err
	}
	if tx.Data, err = s.Bytes(); err != nil {
		return err
	}
	return s.ListEnd()
}

func (tx *DepositTx) EncodingSize() int {
	// size of SourceHash
	payloadSize := 33
	// size of From
	payloadSize += 21
	// size of To
	payloadSize++
	if tx.To != nil {
		payloadSize += 20
	}
	// size of Mint
	payloadSize++
	payloadSize += rlp2.Uint256LenExcludingHead(&tx.Mint)
	// size of Value
	payloadSize++
	payloadSize += rlp2.Uint256LenExcludingHead(&tx.Value)
	// size of Gas
	payloadSize++
	payloadSize += rlp2.IntLenExcludingHead(tx.Gas)
	// size of IsCreation
	payloadSize++
	// size of Data
	payloadSize += rlp2.StringLen(tx.Data)

	// Add envelope size and type size
	return 1 + rlp2.ListPrefixLen(payloadSize) + payloadSize
}

func (tx *DepositTx) GetSender() (libcommon.Address, bool) {
	if sc := tx.from.Load(); sc != nil {
		return *sc, true
	}
	return libcommon.Address{}, false
}

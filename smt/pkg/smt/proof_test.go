package smt_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ledgerwatch/erigon-lib/chain"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv/memdb"
	"github.com/stretchr/testify/require"

	"github.com/holiman/uint256"
	"github.com/ledgerwatch/erigon-lib/common/hexutility"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/smt/pkg/db"
	"github.com/ledgerwatch/erigon/smt/pkg/smt"
	"github.com/ledgerwatch/erigon/smt/pkg/utils"
	"github.com/ledgerwatch/erigon/turbo/trie"
)

func prepareSMT(t *testing.T) (*smt.SMT, *trie.RetainList) {
	t.Helper()

	contract := libcommon.HexToAddress("0x71dd1027069078091B3ca48093B00E4735B20624")
	balance := uint256.NewInt(1000000000)
	sKey := libcommon.HexToHash("0x5")
	sVal := uint256.NewInt(0xdeadbeef)

	_, tx := memdb.NewTestTx(t)

	tds := state.NewTrieDbState(libcommon.Hash{}, tx, 0, state.NewPlainStateReader(tx))

	w := tds.NewTrieStateWriter()

	intraBlockState := state.New(tds)

	tds.StartNewBuffer()

	tds.SetResolveReads(false)

	intraBlockState.CreateAccount(contract, true)

	code := []byte{0x01, 0x02, 0x03, 0x04}
	intraBlockState.SetCode(contract, code)
	intraBlockState.AddBalance(contract, balance)
	intraBlockState.SetState(contract, &sKey, *sVal)

	err := intraBlockState.FinalizeTx(&chain.Rules{}, tds.NewTrieStateWriter())
	require.NoError(t, err, "error finalising 1st tx")

	err = intraBlockState.CommitBlock(&chain.Rules{}, w)
	require.NoError(t, err, "error committing block")

	inclusions := make(map[libcommon.Address][]libcommon.Hash)
	rl, err := tds.ResolveSMTRetainList(inclusions)
	require.NoError(t, err, "error resolving state trie")

	memdb := db.NewMemDb()

	smtTrie := smt.NewSMT(memdb, false)

	_, err = smtTrie.SetAccountState(contract.String(), balance.ToBig(), uint256.NewInt(1).ToBig())
	require.NoError(t, err)

	err = smtTrie.SetContractBytecode(contract.String(), hex.EncodeToString(code))
	require.NoError(t, err)

	err = memdb.AddCode(code)
	require.NoError(t, err, "error adding code to memdb")

	storage := make(map[string]string, 0)

	for i := 0; i < 100; i++ {
		k := libcommon.HexToHash(fmt.Sprintf("0x%d", i)).String()
		storage[k] = k
	}

	storage[sKey.String()] = sVal.String()

	_, err = smtTrie.SetContractStorage(contract.String(), storage, nil)
	require.NoError(t, err)

	return smtTrie, rl
}

func TestFilterProofs(t *testing.T) {
	tests := []struct {
		name     string
		proofs   []*smt.SMTProofElement
		key      utils.NodeKey
		expected []hexutility.Bytes
	}{
		{
			name: "Matching proofs",
			proofs: []*smt.SMTProofElement{
				{Path: []byte{0, 1}, Proof: []byte{1, 2, 3}},
				{Path: []byte{0, 1, 1}, Proof: []byte{4, 5, 6}},
				{Path: []byte{1, 1}, Proof: []byte{7, 8, 9}},
			},
			key:      utils.NodeKey{0, 1, 1, 1},
			expected: []hexutility.Bytes{{1, 2, 3}, {4, 5, 6}},
		},
		{
			name: "No matching proofs",
			proofs: []*smt.SMTProofElement{
				{Path: []byte{1, 1}, Proof: []byte{1, 2, 3}},
				{Path: []byte{1, 0}, Proof: []byte{4, 5, 6}},
			},
			key:      utils.NodeKey{0, 1, 1, 1},
			expected: []hexutility.Bytes{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := smt.FilterProofs(tt.proofs, tt.key)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("FilterProofs() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestVerifyAndGetVal(t *testing.T) {
	smtTrie, rl := prepareSMT(t)

	proofs, err := smt.BuildProofs(smtTrie.RoSMT, rl, context.Background())
	if err != nil {
		t.Fatalf("BuildProofs() error = %v", err)
	}

	contractAddress := libcommon.HexToAddress("0x71dd1027069078091B3ca48093B00E4735B20624")
	a := utils.ConvertHexToBigInt(contractAddress.String())
	address := utils.ScalarToArrayBig(a)

	smtRoot, _ := smtTrie.RoSMT.DbRo.GetLastRoot()
	if err != nil {
		t.Fatalf("GetLastRoot() error = %v", err)
	}
	root := utils.ScalarToRoot(smtRoot)

	t.Run("Value exists and proof is correct", func(t *testing.T) {
		storageKey := utils.KeyContractStorage(address, libcommon.HexToHash("0x5").String())
		storageProof := smt.FilterProofs(proofs, storageKey)

		val, err := smt.VerifyAndGetVal(root, storageProof, storageKey)

		if err != nil {
			t.Fatalf("VerifyAndGetVal() error = %v", err)
		}

		expected := uint256.NewInt(0xdeadbeef).Bytes()

		if !bytes.Equal(val, expected) {
			t.Errorf("VerifyAndGetVal() = %v, want %v", val, expected)
		}
	})

	t.Run("Value doesn't exist and non-existent proof is correct", func(t *testing.T) {
		nonExistentRl := trie.NewRetainList(0)
		nonExistentKeys := []utils.NodeKey{}

		// Fuzz with 1000 non-existent keys
		for i := 0; i < 1000; i++ {
			nonExistentKey := utils.KeyContractStorage(
				address,
				libcommon.HexToHash(fmt.Sprintf("0xdeadbeefabcd1234%d", i)).String(),
			)
			nonExistentKeys = append(nonExistentKeys, nonExistentKey)
			nonExistentKeyPath := nonExistentKey.GetPath()
			keyBytes := make([]byte, 0, len(nonExistentKeyPath))

			for _, v := range nonExistentKeyPath {
				keyBytes = append(keyBytes, byte(v))
			}

			nonExistentRl.AddHex(keyBytes)
		}

		nonExistentProofs, err := smt.BuildProofs(smtTrie.RoSMT, nonExistentRl, context.Background())
		if err != nil {
			t.Fatalf("BuildProofs() error = %v", err)
		}

		for _, key := range nonExistentKeys {
			nonExistentProof := smt.FilterProofs(nonExistentProofs, key)
			val, err := smt.VerifyAndGetVal(root, nonExistentProof, key)

			if err != nil {
				t.Fatalf("VerifyAndGetVal() error = %v", err)
			}

			if len(val) != 0 {
				t.Errorf("VerifyAndGetVal() = %v, want empty value", val)
			}
		}
	})

	t.Run("Value doesn't exist but non-existent proof is insufficient", func(t *testing.T) {
		nonExistentRl := trie.NewRetainList(0)
		nonExistentKey := utils.KeyContractStorage(address, libcommon.HexToHash("0x999").String())
		nonExistentKeyPath := nonExistentKey.GetPath()
		keyBytes := make([]byte, 0, len(nonExistentKeyPath))

		for _, v := range nonExistentKeyPath {
			keyBytes = append(keyBytes, byte(v))
		}

		nonExistentRl.AddHex(keyBytes)

		nonExistentProofs, err := smt.BuildProofs(smtTrie.RoSMT, nonExistentRl, context.Background())
		if err != nil {
			t.Fatalf("BuildProofs() error = %v", err)
		}

		nonExistentProof := smt.FilterProofs(nonExistentProofs, nonExistentKey)

		// Verify the non-existent proof works
		_, err = smt.VerifyAndGetVal(root, nonExistentProof, nonExistentKey)

		if err != nil {
			t.Fatalf("VerifyAndGetVal() error = %v", err)
		}

		// Only pass the first trie node in the proof
		_, err = smt.VerifyAndGetVal(root, nonExistentProof[:1], nonExistentKey)

		if err == nil {
			t.Errorf("VerifyAndGetVal() expected error, got nil")
		}
	})

	t.Run("Value exists but proof is incorrect (first value corrupted)", func(t *testing.T) {
		storageKey := utils.KeyContractStorage(address, libcommon.HexToHash("0x5").String())
		storageProof := smt.FilterProofs(proofs, storageKey)

		// Corrupt the proof by changing a byte
		if len(storageProof) > 0 && len(storageProof[0]) > 0 {
			storageProof[0][0] ^= 0xFF // Flip all bits in the first byte
		}

		_, err := smt.VerifyAndGetVal(root, storageProof, storageKey)

		if err == nil {
			if err == nil || !strings.Contains(err.Error(), "root mismatch at level 0") {
				t.Errorf("VerifyAndGetVal() expected error containing 'root mismatch at level 0', got %v", err)
			}
		}
	})

	t.Run("Value exists but proof is incorrect (last value corrupted)", func(t *testing.T) {
		storageKey := utils.KeyContractStorage(address, libcommon.HexToHash("0x5").String())
		storageProof := smt.FilterProofs(proofs, storageKey)

		// Corrupt the proof by changing the last byte of the last proof element
		if len(storageProof) > 0 {
			lastProof := storageProof[len(storageProof)-1]
			if len(lastProof) > 0 {
				lastProof[len(lastProof)-1] ^= 0xFF // Flip all bits in the last byte
			}
		}

		_, err := smt.VerifyAndGetVal(root, storageProof, storageKey)

		if err == nil {
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("root mismatch at level %d", len(storageProof)-1)) {
				t.Errorf("VerifyAndGetVal() expected error containing 'root mismatch at level %d', got %v", len(storageProof)-1, err)
			}
		}
	})

	t.Run("Value exists but proof is insufficient", func(t *testing.T) {
		storageKey := utils.KeyContractStorage(address, libcommon.HexToHash("0x5").String())
		storageProof := smt.FilterProofs(proofs, storageKey)

		// Modify the proof to claim the value doesn't exist
		if len(storageProof) > 0 {
			storageProof = storageProof[:len(storageProof)-2]
		}

		val, err := smt.VerifyAndGetVal(root, storageProof, storageKey)

		if err == nil || !strings.Contains(err.Error(), "insufficient") {
			t.Errorf("VerifyAndGetVal() expected error containing 'insufficient', got %v", err)
		}

		if len(val) != 0 {
			t.Errorf("VerifyAndGetVal() = %v, want empty value", val)
		}
	})
}

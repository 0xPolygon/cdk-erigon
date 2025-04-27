package blockinfo

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/holiman/uint256"
	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/core/types"
	ethTypes "github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/crypto"
	"github.com/ledgerwatch/erigon/smt/pkg/smt"
	smtutils "github.com/ledgerwatch/erigon/smt/pkg/utils"
	"github.com/ledgerwatch/erigon/zk/utils"
	"github.com/stretchr/testify/require"
)

/*
Contrived tests of the SMT inserts (compared with test cases from the JS implementation)
*/
func TestBlockInfoHeader(t *testing.T) {
	tests := []struct {
		BlockHash          string
		CoinbaseAddress    string
		NewBlockNumber     uint64
		BlockGasLimit      uint64
		FinalTimestamp     uint64
		FinalGER           string
		L1BlochHash        string
		FinalBlockInfoRoot string
	}{
		{
			BlockHash:          "0x1fe466d9df83e1d2a4c32e21c6078b8f5f590e7db30b006965faa2f27a9b4fea",
			CoinbaseAddress:    "0x617b3a3528F9cDd6630fd3301B9c8911F7Bf063D",
			NewBlockNumber:     1,
			BlockGasLimit:      4294967295,
			FinalTimestamp:     1944498031,
			FinalGER:           "0x0000000000000000000000000000000000000000000000000000000000000000",
			L1BlochHash:        "0x0000000000000000000000000000000000000000000000000000000000000000",
			FinalBlockInfoRoot: "0x64f37448decfd2837a23f825060a705b1135247a08f88a047ccde3aa58efb52b",
		}, {
			BlockHash:          "0x4a9bfcb163ec91c5beb22e6aca41592433092c8c7821b01d37fd0de483f9265d",
			CoinbaseAddress:    "0x617b3a3528F9cDd6630fd3301B9c8911F7Bf063D",
			NewBlockNumber:     1,
			BlockGasLimit:      4294967295,
			FinalTimestamp:     1944498031,
			FinalGER:           "0x0000000000000000000000000000000000000000000000000000000000000000",
			L1BlochHash:        "0x0000000000000000000000000000000000000000000000000000000000000000",
			FinalBlockInfoRoot: "0x445c76b4a370754cd2fbb52da85e9c5265e9a10244ebf751f0de27a252efe4a0",
		}, {
			BlockHash:          "0x4a9bfcb163ec91c5beb22e6aca41592433092c8c7821b01d37fd0de483f9265d",
			CoinbaseAddress:    "0x617b3a3528F9cDd6630fd3301B9c8911F7Bf063D",
			NewBlockNumber:     5,
			BlockGasLimit:      4294967295,
			FinalTimestamp:     1944498031,
			FinalGER:           "0x0000000000000000000000000000000000000000000000000000000000000000",
			L1BlochHash:        "0x0000000000000000000000000000000000000000000000000000000000000000",
			FinalBlockInfoRoot: "0xf8c8d52e97e83cbe07ad1883f6510ec2aafcde26e5d291290ecd240e76241bce",
		}, {
			BlockHash:          "0x045fa48a1342813a61c1dd2d235620d621b59cdda0bd07ff3536c6cf64f5e688",
			CoinbaseAddress:    "0x9aeCf44E36f20DC407d1A580630c9a2419912dcB",
			NewBlockNumber:     592221,
			BlockGasLimit:      utils.ForkId8BlockGasLimit,
			FinalTimestamp:     1708198045,
			FinalGER:           "0x0000000000000000000000000000000000000000000000000000000000000000",
			L1BlochHash:        "0x0000000000000000000000000000000000000000000000000000000000000000",
			FinalBlockInfoRoot: "0xdb45b84ec5ea8b706170c775c8d0a6ded9938d850c6e878c00439f9320f68583",
		}, {
			BlockHash:          "0x268a22af2bae40acd1cc4228896de4420c5f3bc3bbdd8515d6d01b1b99731f82",
			CoinbaseAddress:    "0x9aeCf44E36f20DC407d1A580630c9a2419912dcB",
			NewBlockNumber:     592223,
			BlockGasLimit:      utils.ForkId7BlockGasLimit,
			FinalTimestamp:     1708198051,
			FinalGER:           "0x0000000000000000000000000000000000000000000000000000000000000000",
			L1BlochHash:        "0x0000000000000000000000000000000000000000000000000000000000000000",
			FinalBlockInfoRoot: "0xadb3544de6a274492901e4c8b030342d26f586f5ad3788b0fb006c0f69395d56",
		},
	}

	for _, test := range tests {
		infoTree := NewBlockInfoTree()
		blockHash := common.HexToHash(test.BlockHash)
		coinbaseAddress := common.HexToAddress(test.CoinbaseAddress)
		ger := common.HexToHash(test.FinalGER)
		l1BlochHash := common.HexToHash(test.L1BlochHash)

		keys, vals, err := infoTree.GenerateBlockHeader(
			&blockHash,
			&coinbaseAddress,
			test.NewBlockNumber,
			test.BlockGasLimit,
			test.FinalTimestamp,
			&ger,
			&l1BlochHash,
		)
		if err != nil {
			t.Fatal(err)
		}
		insertBatchCfg := smt.NewInsertBatchConfig(context.Background(), "", false)
		root, err := infoTree.smt.InsertBatch(insertBatchCfg, keys, vals, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		rootHash := common.BigToHash(root.NewRootScalar.ToBigInt()).Hex()

		if rootHash != test.FinalBlockInfoRoot {
			t.Fatalf("expected root %s, got %s", test.FinalBlockInfoRoot, rootHash)
		}
	}
}

func TestSetBlockTx(t *testing.T) {
	tests := []struct {
		l2TxHash            common.Hash
		txIndex             int
		receipt             ethTypes.Receipt
		logIndex            int64
		cumulativeGasUsed   uint64
		effectivePercentage uint8
		finalBlockInfoRoot  string
	}{
		{
			l2TxHash: common.Hash{0},
			txIndex:  0,
			receipt: ethTypes.Receipt{
				Status: 1,
				Logs: []*types.Log{
					{
						Address: common.HexToAddress("0x0000000000000000000000000000000000000000"),
						Topics: []common.Hash{
							common.HexToHash("0000000000000000000000000000000000000000000000000000000000000001"),
						},
						Data: common.HexToHash("0000000000000000000000000000000000000000000000000000000000000000").Bytes(),
					},
					{
						Address: common.HexToAddress("0x0000000000000000000000000000000000000000"),
						Topics: []common.Hash{
							common.HexToHash("0000000000000000000000000000000000000000000000000000000000000004"),
							common.HexToHash("0000000000000000000000000000000000000000000000000000000000000005"),
							common.HexToHash("0000000000000000000000000000000000000000000000000000000000000006"),
						},
						Data: common.HexToHash("0000000000000000000000000000000000000000000000000000000000000000").Bytes(),
					},
					{
						Address: common.HexToAddress("0x0000000000000000000000000000000000000000"),
						Topics: []common.Hash{
							common.HexToHash("0000000000000000000000000000000000000000000000000000000000000001"),
							common.HexToHash("0000000000000000000000000000000000000000000000000000000000000002"),
						},
						Data: common.HexToHash("0000000000000000000000000000000000000000000000000000000000000000").Bytes(),
					},
				},
			},
			logIndex:            0,
			cumulativeGasUsed:   26336,
			effectivePercentage: 255,
			finalBlockInfoRoot:  "0x0dde47bb5f76e014b6b13de4be07529ef018b454f09f16ef4c9b15a8f9c59d4f",
		}, {
			l2TxHash: common.Hash{0},
			txIndex:  0,
			receipt: ethTypes.Receipt{
				Status: 0,
				Logs:   []*types.Log{},
			},
			logIndex:            0,
			cumulativeGasUsed:   21000,
			effectivePercentage: 0,
			finalBlockInfoRoot:  "0x20a3ac1075ef9bb2fa88967b2b3075221c32ab3ef3034a9d9c1520adc45100be",
		}, {
			l2TxHash: common.Hash{0},
			txIndex:  0,
			receipt: ethTypes.Receipt{
				Status: 1,
				Logs:   []*types.Log{},
			},
			logIndex:            0,
			cumulativeGasUsed:   21000,
			effectivePercentage: 255,
			finalBlockInfoRoot:  "0x3ea32169f0fe8b1c54a8d35cc31f9d14a39537d72dbadd2044822816860cf816",
		}, {
			l2TxHash: common.Hash{0},
			txIndex:  0,
			receipt: ethTypes.Receipt{
				Status: 1,
				Logs:   []*types.Log{},
			},
			logIndex:            0,
			cumulativeGasUsed:   10000,
			effectivePercentage: 255,
			finalBlockInfoRoot:  "0x668ff5e2a08822a32cb3929c012c0544ff0e14deb560817281184ced14f44edd",
		}, {
			l2TxHash: common.HexToHash("0x4844782b879fb11b10522bcc32c7efb607a2d1dc713f2d2678c994768465e113"),
			txIndex:  0,
			receipt: ethTypes.Receipt{
				Status: 1,
				Logs:   []*types.Log{},
			},
			logIndex:            0,
			cumulativeGasUsed:   21000,
			effectivePercentage: 255,
			finalBlockInfoRoot:  "0x2a667a8dfe091e5630167afd95190a6c97a31db3c719cc614a356167904e1c18",
		},
	}

	for _, test := range tests {
		infoTree := NewBlockInfoTree()
		keys, vals, err := infoTree.GenerateBlockTxKeysVals(
			&test.l2TxHash,
			test.txIndex,
			&test.receipt,
			test.logIndex,
			test.cumulativeGasUsed,
			test.effectivePercentage,
		)
		if err != nil {
			t.Fatal(err)
		}
		insertBatchCfg := smt.NewInsertBatchConfig(context.Background(), "", false)
		root, err2 := infoTree.smt.InsertBatch(insertBatchCfg, keys, vals, nil, nil)
		if err2 != nil {
			t.Fatal(err2)
		}

		rootHex := common.BigToHash(root.NewRootScalar.ToBigInt()).Hex()

		if rootHex != test.finalBlockInfoRoot {
			t.Fatalf("expected root %s, got %s", test.finalBlockInfoRoot, rootHex)
		}
	}
}

func TestBlockComulativeGasUsed(t *testing.T) {
	tests := []struct {
		gasUsed      uint64
		expectedRoot string
	}{
		{
			gasUsed:      26336,
			expectedRoot: "0x5cd280355924dcf29ac41ccae98d678091d182af191443f3c92562e1c1c64254",
		}, {
			gasUsed:      21000,
			expectedRoot: "0x9cfdda40abe9331804fe6b55be89421bd74ca56e9da719e39bbf5518e08155e1",
		}, {
			gasUsed:      10000,
			expectedRoot: "0x32cc19445bc8843c9f432cad24c3c6ea198734547d996bb977a2011c04d917f8",
		},
	}

	for i, test := range tests {
		infoTree := NewBlockInfoTree()

		key, val, err := generateBlockGasUsed(test.gasUsed)
		if err != nil {
			t.Fatal(err)
		}

		root, err2 := infoTree.smt.InsertKA(*key, smtutils.NodeValue8ToBigInt(val))
		if err2 != nil {
			t.Fatal(err2)
		}

		rootHex := common.BigToHash(root.NewRootScalar.ToBigInt()).Hex()

		// root taken from JS implementation
		if rootHex != test.expectedRoot {
			t.Fatalf("Test %d expected root %s, got %s", i+1, test.expectedRoot, rootHex)
		}
	}
}

func TestSetL2BlockHash(t *testing.T) {
	tests := []struct {
		blockHash    string
		expectedRoot string
	}{
		{
			blockHash:    "0x1fe466d9df83e1d2a4c32e21c6078b8f5f590e7db30b006965faa2f27a9b4fea",
			expectedRoot: "0x1db6a2e2ce5016d114c38a4530c66adfb1b24bf66714d20eb983ed4910ed6600",
		},
		{
			blockHash:    "0x4a9bfcb163ec91c5beb22e6aca41592433092c8c7821b01d37fd0de483f9265d",
			expectedRoot: "0xaa99d2be4188527344ef32d31024b127006e9fbbdb75862de564d448c47816be",
		},
	}

	for i, test := range tests {
		blockHash := common.HexToHash(test.blockHash)

		key, val, err := generateL2BlockHash(&blockHash)
		if err != nil {
			t.Fatal(err)
		}
		smt := smt.NewSMT(nil, true)

		root, err2 := smt.InsertKA(*key, smtutils.NodeValue8ToBigInt(val))
		if err2 != nil {
			t.Fatal(err2)
		}

		actualRoot := common.BigToHash(root.NewRootScalar.ToBigInt()).Hex()
		if actualRoot != test.expectedRoot {
			t.Fatalf("Test %d expected root %s, got %s", i+1, test.expectedRoot, actualRoot)
		}
	}
}

func TestSetCoinbase(t *testing.T) {
	tests := []struct {
		coinbaseAddress string
		expectedRoot    string
	}{
		{
			coinbaseAddress: "0x617b3a3528F9cDd6630fd3301B9c8911F7Bf063D",
			expectedRoot:    "0x27fb3bd76956839741006a2dd73bfffadb9573c6cd8ce60b0566b7c81a55b7b4",
		},
	}

	for i, test := range tests {
		smt := smt.NewSMT(nil, true)
		coinbaseAddress := common.HexToAddress(test.coinbaseAddress)

		key, val, err := generateCoinbase(&coinbaseAddress)
		if err != nil {
			t.Fatal(err)
		}
		root, err2 := smt.InsertKA(*key, smtutils.NodeValue8ToBigInt(val))
		if err2 != nil {
			t.Fatal(err2)
		}

		actualRoot := common.BigToHash(root.NewRootScalar.ToBigInt()).Hex()
		if actualRoot != test.expectedRoot {
			t.Fatalf("Test %d expected root %s, got %s", i+1, test.expectedRoot, actualRoot)
		}
	}
}

func TestSetBlockNumber(t *testing.T) {
	tests := []struct {
		blockNum     uint64
		expectedRoot string
	}{
		{
			blockNum:     1,
			expectedRoot: "0x45685d4b214d4eb330627ff12797a4063fefcc13579f5c1fe5f7131a397c26b4",
		}, {
			blockNum:     5,
			expectedRoot: "0xad832d8f6f2ca140d3aff0065d7fb920a643e3619ead5404832e54a511aeec6c",
		},
	}

	for i, test := range tests {
		smt := smt.NewSMT(nil, true)

		key, val, err := generateBlockNumber(test.blockNum)
		if err != nil {
			t.Fatal(err)
		}
		root, err2 := smt.InsertKA(*key, smtutils.NodeValue8ToBigInt(val))
		if err2 != nil {
			t.Fatal(err2)
		}

		actualRoot := common.BigToHash(root.NewRootScalar.ToBigInt()).Hex()
		// root taken from JS implementation
		if actualRoot != test.expectedRoot {
			t.Fatalf("Test %d expected root %s, got %s", i+1, test.expectedRoot, actualRoot)
		}
	}
}

func TestSetGasLimit(t *testing.T) {
	tests := []struct {
		gasLimit     uint64
		expectedRoot string
	}{
		{
			gasLimit:     4294967295,
			expectedRoot: "0xdfb45af6d25ba1d98cf29e5272049fc5007d63fe4a0c0ca2322ef826debb2b6c",
		},
	}

	for i, test := range tests {
		smt := smt.NewSMT(nil, true)

		key, val, err := generateGasLimit(test.gasLimit)
		if err != nil {
			t.Fatal(err)
		}
		root, err2 := smt.InsertKA(*key, smtutils.NodeValue8ToBigInt(val))
		if err2 != nil {
			t.Fatal(err2)
		}

		actualRoot := common.BigToHash(root.NewRootScalar.ToBigInt()).Hex()
		// root taken from JS implementation
		if actualRoot != test.expectedRoot {
			t.Fatalf("Test %d expected root %s, got %s", i+1, test.expectedRoot, actualRoot)
		}
	}
}

func TestSetTimestamp(t *testing.T) {
	tests := []struct {
		timestamp    uint64
		expectedRoot string
	}{
		{
			timestamp:    1944498031,
			expectedRoot: "0xe0ef08c2c9c75a9e7a9fceec0483414489be3b9d34312115a2eb9c30339a3922",
		},
	}

	for i, test := range tests {
		smt := smt.NewSMT(nil, true)

		key, val, err := generateTimestamp(test.timestamp)
		if err != nil {
			t.Fatal(err)
		}
		root, err2 := smt.InsertKA(*key, smtutils.NodeValue8ToBigInt(val))
		if err2 != nil {
			t.Fatal(err2)
		}

		actualRoot := common.BigToHash(root.NewRootScalar.ToBigInt()).Hex()
		// root taken from JS implementation
		if actualRoot != test.expectedRoot {
			t.Fatalf("Test %d expected root %s, got %s", i+1, test.expectedRoot, actualRoot)
		}
	}
}

func TestSetGer(t *testing.T) {
	tests := []struct {
		ger          string
		expectedRoot string
	}{
		{
			ger:          "0x819feaf48e670e06a9faa2ecce4b795f214ed1f0258b22e49db7691da8206663",
			expectedRoot: "0x61f1fac06c5b64bf969df3e57cea7418fdab1c38e3ee5ac654b2c74e27316bd4",
		}, {
			ger:          "0xb15aa2b6ef32f2b517e19672e43186094f7e0d37a4b60b77644ee33b5feb3f7f",
			expectedRoot: "0xf598491f603545710aa7ec6ad8c9b2f554c0f02eb04092d992228e9dfcb682e0",
		}, {
			ger:          "0x5f4e0c5cbfc891af492d7335d988c2578204a75c997bfad0e7ca8fc2bd4389c9",
			expectedRoot: "0x7a0b0cc58dc3777704c34d965f6b5d86146280c82b288c23a32aee1989d1a504",
		},
	}

	for i, test := range tests {
		smt := smt.NewSMT(nil, true)
		ger := common.HexToHash(test.ger)

		key, val, err := generateGer(&ger)
		if err != nil {
			t.Fatal(err)
		}
		root, err2 := smt.InsertKA(*key, smtutils.NodeValue8ToBigInt(val))
		if err2 != nil {
			t.Fatal(err2)
		}

		actualRoot := common.BigToHash(root.NewRootScalar.ToBigInt()).Hex()
		// root taken from JS implementation
		if actualRoot != test.expectedRoot {
			t.Fatalf("Test %d expected root %s, got %s", i+1, test.expectedRoot, actualRoot)
		}
	}
}

func TestSetL1BlockHash(t *testing.T) {
	tests := []struct {
		l1BlockHash  string
		expectedRoot string
	}{
		{
			l1BlockHash:  "0x819feaf48e670e06a9faa2ecce4b795f214ed1f0258b22e49db7691da8206663",
			expectedRoot: "0xc0cea75b3047bf5f28cf3affaeaf9842e68a5d29544a237e4e8bbea4b369d25f",
		}, {
			l1BlockHash:  "0xb15aa2b6ef32f2b517e19672e43186094f7e0d37a4b60b77644ee33b5feb3f7f",
			expectedRoot: "0x68909800f942475ab88aea079b7407131f7e1aad2de0a860803411f9560803a7",
		}, {
			l1BlockHash:  "0x5f4e0c5cbfc891af492d7335d988c2578204a75c997bfad0e7ca8fc2bd4389c9",
			expectedRoot: "0xcb2eb84e4e2070d4c7aa827ab796131339c20554a20592c0f80afa225a9e5901",
		},
	}

	for i, test := range tests {
		smt := smt.NewSMT(nil, true)
		l1BlockHash := common.HexToHash(test.l1BlockHash)

		key, val, err := generateL1BlockHash(&l1BlockHash)
		if err != nil {
			t.Fatal(err)
		}
		root, err2 := smt.InsertKA(*key, smtutils.NodeValue8ToBigInt(val))
		if err2 != nil {
			t.Fatal(err2)
		}

		actualRoot := common.BigToHash(root.NewRootScalar.ToBigInt()).Hex()
		// root taken from JS implementation
		if actualRoot != test.expectedRoot {
			t.Fatalf("Test %d expected root %s, got %s", i+1, test.expectedRoot, actualRoot)
		}
	}
}

func TestSetL2TxHash(t *testing.T) {
	smt := smt.NewSMT(nil, true)
	txIndex := big.NewInt(1)
	l2TxHash := common.HexToHash("0x000000000000000000000000000000005Ca1aB1E").Big()

	key, val, err := generateL2TxHash(txIndex, l2TxHash)
	if err != nil {
		t.Fatal(err)
	}

	root, err2 := smt.InsertKA(*key, smtutils.NodeValue8ToBigInt(val))
	if err2 != nil {
		t.Fatal(err2)
	}

	actualRoot := common.BigToHash(root.NewRootScalar.ToBigInt()).Hex()
	// root taken from JS implementation
	expectedRoot := "0xa9127a157cee3cd2452a194e4efc2f8a5612cfc36c66e768700727ede4d0e2e6"
	if actualRoot != expectedRoot {
		t.Fatalf("expected root %s, got %s", expectedRoot, actualRoot)
	}
}

func TestSetTxStatus(t *testing.T) {
	smt := smt.NewSMT(nil, true)
	txIndex := big.NewInt(1)
	status := common.HexToHash("0x000000000000000000000000000000005Ca1aB1E").Big()

	key, val, err := generateTxStatus(txIndex, status)
	if err != nil {
		t.Fatal(err)
	}
	root, err2 := smt.InsertKA(*key, smtutils.NodeValue8ToBigInt(val))
	if err2 != nil {
		t.Fatal(err2)
	}

	actualRoot := common.BigToHash(root.NewRootScalar.ToBigInt()).Hex()

	// root taken from JS implementation
	expectedRoot := "0x7cb6a0928f5165a422cfbe5f93d1cc9eda3f686715639823f6087818465fcbb8"
	if actualRoot != expectedRoot {
		t.Fatalf("expected root %s, got %s", expectedRoot, actualRoot)
	}
}

func TestSetCumulativeGasUsed(t *testing.T) {
	smt := smt.NewSMT(nil, true)
	txIndex := big.NewInt(1)
	cgu := common.HexToHash("0x000000000000000000000000000000005Ca1aB1E").Big()

	key, val, err := generateCumulativeGasUsed(txIndex, cgu)
	if err != nil {
		t.Fatal(err)
	}

	root, err2 := smt.InsertKA(*key, smtutils.NodeValue8ToBigInt(val))
	if err2 != nil {
		t.Fatal(err2)
	}

	actualRoot := common.BigToHash(root.NewRootScalar.ToBigInt()).Hex()

	// root taken from JS implementation
	expectedRoot := "0xc07ff46f07be5b81465c30848202acc4bf82805961d8a9f9ffe74e820e4bca68"
	if actualRoot != expectedRoot {
		t.Fatalf("expected root %s, got %s", expectedRoot, actualRoot)
	}
}

func TestSetTxEffectivePercentage(t *testing.T) {
	smt := smt.NewSMT(nil, true)
	txIndex := big.NewInt(1)
	egp := common.HexToHash("0x000000000000000000000000000000005Ca1aB1E").Big()

	key, val, err := generateTxEffectivePercentage(txIndex, egp)
	if err != nil {
		t.Fatal(err)
	}

	root, err2 := smt.InsertKA(*key, smtutils.NodeValue8ToBigInt(val))
	if err2 != nil {
		t.Fatal(err2)
	}

	actualRoot := common.BigToHash(root.NewRootScalar.ToBigInt()).Hex()

	// root taken from JS implementation
	expectedRoot := "0xf6b3130ecdd23bd9e47c4dda0fdde6bd0e0446c6d6927778e57e80016fa9fa23"
	if actualRoot != expectedRoot {
		t.Fatalf("expected root %s, got %s", expectedRoot, actualRoot)
	}
}

func TestSetTxLogs(t *testing.T) {
	smt := smt.NewSMT(nil, true)
	txIndex := big.NewInt(1)
	logIndex := big.NewInt(1)
	log := common.HexToHash("0x000000000000000000000000000000005Ca1aB1E").Big()

	key, val, err := generateTxLog(txIndex, logIndex, log)
	if err != nil {
		t.Fatal(err)
	}

	root, err2 := smt.InsertKA(*key, smtutils.NodeValue8ToBigInt(val))
	if err2 != nil {
		t.Fatal(err2)
	}

	actualRoot := common.BigToHash(root.NewRootScalar.ToBigInt()).Hex()

	// root taken from JS implementation
	expectedRoot := "0xaff38141ae4538baf61f08efe3019ef2d219f30b98b1d40a9813d502f6bacb12"
	if actualRoot != expectedRoot {
		t.Fatalf("expected root %s, got %s", expectedRoot, actualRoot)
	}
}

func TestBuildBlockInfoTree(t *testing.T) {
	// Prepare common parameters
	coinbaseAddr := common.HexToAddress("0x617b3a3528F9cDd6630fd3301B9c8911F7Bf063D")
	blockGasLimit := uint64(4294967295)
	blockTime := uint64(1944498031)
	blockGasUsed := uint64(0)
	ger := common.Hash{}
	l1BlockHash := common.Hash{}
	previousStateRoot := common.Hash{}

	t.Run("Basic concurrent processing", func(t *testing.T) {
		txInfos := createTestTransactions(10)
		blockNumber := uint64(1)

		root, err := BuildBlockInfoTree(
			&coinbaseAddr,
			blockNumber,
			blockTime,
			blockGasLimit,
			blockGasUsed,
			ger,
			l1BlockHash,
			previousStateRoot,
			&txInfos,
		)
		require.NoError(t, err)
		require.NotNil(t, root)
	})

	// Empty transaction list test
	t.Run("Empty transaction list", func(t *testing.T) {
		var txInfos []ExecutedTxInfo
		blockNumber := uint64(1)

		root, err := BuildBlockInfoTree(
			&coinbaseAddr,
			blockNumber,
			blockTime,
			blockGasLimit,
			blockGasUsed,
			ger,
			l1BlockHash,
			previousStateRoot,
			&txInfos,
		)
		require.NoError(t, err)
		require.NotNil(t, root)
	})

	// Large number of transactions
	t.Run("Large number of transactions", func(t *testing.T) {
		txInfos := createTestTransactions(1000)
		blockNumber := uint64(1)

		start := time.Now()
		root, err := BuildBlockInfoTree(
			&coinbaseAddr,
			blockNumber,
			blockTime,
			blockGasLimit,
			blockGasUsed,
			ger,
			l1BlockHash,
			previousStateRoot,
			&txInfos,
		)
		duration := time.Since(start)

		require.NoError(t, err)
		require.NotNil(t, root)
		require.Less(t, duration, 5*time.Second)
	})

	// Test concurrent correctness
	t.Run("Concurrent correctness", func(t *testing.T) {
		txInfos := createComplexTransactions(100)
		blockNumber := uint64(1)
		var results []*common.Hash

		for i := 0; i < 5; i++ {
			root, err := BuildBlockInfoTree(
				&coinbaseAddr,
				blockNumber,
				blockTime,
				blockGasLimit,
				blockGasUsed,
				ger,
				l1BlockHash,
				previousStateRoot,
				&txInfos,
			)
			require.NoError(t, err)
			results = append(results, root)
		}

		// Verify all results are consistent
		for i := 1; i < len(results); i++ {
			require.Equal(t, results[0], results[i])
		}
	})
}

// Helper function: create test transactions
func createTestTransactions(numTxs int) []ExecutedTxInfo {
	txInfos := make([]ExecutedTxInfo, numTxs)
	for i := 0; i < numTxs; i++ {
		to := common.HexToAddress("0x1275fbb540c8efc58b812ba83b0d0b8b9917ae98")
		from := common.HexToAddress("0x4d5Cf5032B2a844602278b01199ED191A86c93ff")
		value := uint256.NewInt(0)
		gasPrice := uint256.NewInt(1000000000)
		chainId := big.NewInt(1000)

		legacyTx := &types.LegacyTx{
			CommonTx: types.CommonTx{
				ChainID: uint256.MustFromBig(chainId),
				Nonce:   uint64(i),
				Gas:     100000,
				To:      &to,
				Value:   value,
				Data:    []byte{},
			},
			GasPrice: gasPrice,
		}

		var tx types.Transaction = legacyTx

		receipt := &types.Receipt{
			Status:            1,
			CumulativeGasUsed: 21000,
			Logs:              []*types.Log{},
			TxHash:            tx.Hash(),
			BlockNumber:       big.NewInt(int64(i + 1)),
		}

		txInfos[i] = ExecutedTxInfo{
			Tx:                tx,
			Receipt:           receipt,
			Signer:            &from,
			EffectiveGasPrice: 100,
		}
	}
	return txInfos
}

// Create transactions with complex dependencies
func createComplexTransactions(count int) []ExecutedTxInfo {
	txInfos := createTestTransactions(count)
	for i := 1; i < count; i++ {
		txInfos[i].Receipt.Logs = append(txInfos[i].Receipt.Logs, &types.Log{
			Address: *txInfos[i-1].Signer,
			Topics:  []common.Hash{common.BytesToHash([]byte(fmt.Sprintf("topic-%d", i)))},
		})
	}
	return txInfos
}

func TestBlockInfoTreeConsistency(t *testing.T) {
	testCases := []struct {
		name             string
		transactionCount int
		logsPerTx        []int // Number of logs per transaction
	}{
		{"Empty", 0, []int{}},
		{"SingleTx_NoLogs", 1, []int{0}},
		{"SingleTx_WithLogs", 1, []int{5}},
		{"MultiTx_EvenLogs", 10, []int{2, 2, 2, 2, 2, 2, 2, 2, 2, 2}},
		{"MultiTx_VaryingLogs", 5, []int{0, 10, 5, 3, 7}},
		{"LargeTx_Count", 50, make([]int, 50)}, // 50 transactions, each with no logs
		{"RealWorld_Scenario", 20, []int{3, 1, 0, 5, 2, 7, 0, 0, 4, 1, 2, 3, 0, 8, 1, 0, 2, 3, 1, 4}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Prepare test data
			txInfos := generateTestTransactions(t, tc.transactionCount, tc.logsPerTx)

			// Prepare block parameters
			coinbase := common.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7")
			blockNumber := uint64(12345)
			blockTime := uint64(time.Now().Unix())
			blockGasLimit := uint64(30000000)
			blockGasUsed := uint64(5000000)
			ger := common.HexToHash("0x9876543210abcdef9876543210abcdef9876543210abcdef9876543210abcdef")
			l1BlockHash := common.HexToHash("0xfedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210")
			previousStateRoot := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

			// Execute parallel version
			startParallel := time.Now()
			parallelRoot, err := BuildBlockInfoTree(
				&coinbase,
				blockNumber,
				blockTime,
				blockGasLimit,
				blockGasUsed,
				ger,
				l1BlockHash,
				previousStateRoot,
				&txInfos,
			)
			parallelDuration := time.Since(startParallel)
			require.NoError(t, err, "Parallel version execution failed")

			// Execute serial version
			startSerial := time.Now()
			serialRoot, err := BuildBlockInfoTreeSerial(
				&coinbase,
				blockNumber,
				blockTime,
				blockGasLimit,
				blockGasUsed,
				ger,
				l1BlockHash,
				previousStateRoot,
				&txInfos,
			)
			serialDuration := time.Since(startSerial)
			require.NoError(t, err, "Serial version execution failed")

			// Verify results are consistent
			require.NotNil(t, parallelRoot, "Parallel version returned nil root hash")
			require.NotNil(t, serialRoot, "Serial version returned nil root hash")
			require.Equal(t, serialRoot.String(), parallelRoot.String(),
				"Parallel and serial processing produced different root hashes")

			// Output performance comparison
			t.Logf("Performance comparison - Transactions: %d, Total logs: %d, Parallel: %v, Serial: %v, Speedup: %.2fx",
				tc.transactionCount,
				sumLogs(tc.logsPerTx),
				parallelDuration,
				serialDuration,
				float64(serialDuration)/float64(parallelDuration),
			)
		})
	}
}

// Test edge cases
func TestBlockInfoTreeEdgeCases(t *testing.T) {
	// Prepare basic parameters
	coinbase := common.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7")
	blockNumber := uint64(12345)
	blockTime := uint64(time.Now().Unix())
	blockGasLimit := uint64(30000000)
	blockGasUsed := uint64(5000000)
	ger := common.HexToHash("0x9876543210abcdef9876543210abcdef9876543210abcdef9876543210abcdef")
	l1BlockHash := common.HexToHash("0xfedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210")
	previousStateRoot := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	// Test 1: Single transaction with many logs
	t.Run("SingleTx_ManyLogs", func(t *testing.T) {
		txInfos := generateTestTransactions(t, 1, []int{1000})

		parallelRoot, err := BuildBlockInfoTree(
			&coinbase, blockNumber, blockTime, blockGasLimit, blockGasUsed,
			ger, l1BlockHash, previousStateRoot, &txInfos,
		)
		require.NoError(t, err)

		serialRoot, err := BuildBlockInfoTreeSerial(
			&coinbase, blockNumber, blockTime, blockGasLimit, blockGasUsed,
			ger, l1BlockHash, previousStateRoot, &txInfos,
		)
		require.NoError(t, err)

		require.Equal(t, serialRoot.String(), parallelRoot.String())
	})

	// Test 2: Special characters and large data logs
	t.Run("SpecialLogs", func(t *testing.T) {
		txInfos := generateTestTransactionsWithSpecialLogs(t)

		parallelRoot, err := BuildBlockInfoTree(
			&coinbase, blockNumber, blockTime, blockGasLimit, blockGasUsed,
			ger, l1BlockHash, previousStateRoot, &txInfos,
		)
		require.NoError(t, err)

		serialRoot, err := BuildBlockInfoTreeSerial(
			&coinbase, blockNumber, blockTime, blockGasLimit, blockGasUsed,
			ger, l1BlockHash, previousStateRoot, &txInfos,
		)
		require.NoError(t, err)

		require.Equal(t, serialRoot.String(), parallelRoot.String())
	})

	// Test 3: Multiple runs with random data
	t.Run("RandomData_MultipleRuns", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			txInfos := generateRandomTestTransactions(t, 20)

			parallelRoot, err := BuildBlockInfoTree(
				&coinbase, blockNumber, blockTime, blockGasLimit, blockGasUsed,
				ger, l1BlockHash, previousStateRoot, &txInfos,
			)
			require.NoError(t, err)

			serialRoot, err := BuildBlockInfoTreeSerial(
				&coinbase, blockNumber, blockTime, blockGasLimit, blockGasUsed,
				ger, l1BlockHash, previousStateRoot, &txInfos,
			)
			require.NoError(t, err)

			require.Equal(t, serialRoot.String(), parallelRoot.String(),
				"Random data run %d produced different root hashes", i)
		}
	})
}

// Test concurrency stability
func TestBlockInfoTreeConcurrencyStability(t *testing.T) {
	// Prepare basic parameters
	coinbase := common.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7")
	blockNumber := uint64(12345)
	blockTime := uint64(time.Now().Unix())
	blockGasLimit := uint64(30000000)
	blockGasUsed := uint64(5000000)
	ger := common.HexToHash("0x9876543210abcdef9876543210abcdef9876543210abcdef9876543210abcdef")
	l1BlockHash := common.HexToHash("0xfedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210")
	previousStateRoot := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	// Generate test transactions
	txInfos := generateTestTransactions(t, 30, []int{2, 3, 0, 1, 5, 0, 2, 3, 4, 1, 0, 3, 2, 1, 0, 4, 5, 2, 1, 3, 0, 2, 1, 3, 4, 0, 1, 2, 3, 0})

	// Run parallel version multiple times to ensure consistent results
	var firstRoot *common.Hash

	for i := 0; i < 10; i++ {
		root, err := BuildBlockInfoTree(
			&coinbase, blockNumber, blockTime, blockGasLimit, blockGasUsed,
			ger, l1BlockHash, previousStateRoot, &txInfos,
		)
		require.NoError(t, err)

		if i == 0 {
			firstRoot = root
		} else {
			require.Equal(t, firstRoot.String(), root.String(),
				"Parallel version produced different root hashes across multiple runs")
		}
	}

	// Verify consistency with serial version
	serialRoot, err := BuildBlockInfoTreeSerial(
		&coinbase, blockNumber, blockTime, blockGasLimit, blockGasUsed,
		ger, l1BlockHash, previousStateRoot, &txInfos,
	)
	require.NoError(t, err)
	require.Equal(t, serialRoot.String(), firstRoot.String())
}

// Helper function: Generate test transactions
func generateTestTransactions(t *testing.T, count int, logsPerTx []int) []ExecutedTxInfo {
	if count == 0 {
		return []ExecutedTxInfo{}
	}

	require.Equal(t, count, len(logsPerTx), "Transaction count and logs array length don't match")

	result := make([]ExecutedTxInfo, count)

	for i := 0; i < count; i++ {
		// Generate private key and address
		privateKey, err := crypto.GenerateKey()
		require.NoError(t, err)

		signer := crypto.PubkeyToAddress(privateKey.PublicKey)

		// Create transaction
		tx := ethTypes.NewTransaction(
			uint64(i), // nonce
			common.HexToAddress("0x8888f1f195afa192cfee860698584c030f4c9db1"), // to
			uint256.NewInt(1000000),         // value
			uint64(21000+i*1000),            // gas
			uint256.NewInt(1000000000),      // gasPrice
			[]byte("test transaction data"), // data
		)

		// Create receipt and logs
		logs := make([]*ethTypes.Log, logsPerTx[i])
		for j := 0; j < logsPerTx[i]; j++ {
			topics := []common.Hash{
				common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
			}
			if j%2 == 0 {
				topics = append(topics, common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000002"))
			}

			logs[j] = &ethTypes.Log{
				Address:     common.HexToAddress("0x8888f1f195afa192cfee860698584c030f4c9db1"),
				Topics:      topics,
				Data:        []byte{byte(i), byte(j), 0x01, 0x02, 0x03},
				BlockNumber: uint64(12345),
				TxHash:      common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
				TxIndex:     uint(i),
				BlockHash:   common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
				Index:       uint(j),
				Removed:     false,
			}
		}

		receipt := &ethTypes.Receipt{
			Status:            uint64(1),
			CumulativeGasUsed: uint64(21000 * (i + 1)),
			Logs:              logs,
			TxHash:            tx.Hash(),
			GasUsed:           uint64(21000 + i*1000),
			BlockNumber:       big.NewInt(12345),
			BlockHash:         common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"),
		}

		result[i] = ExecutedTxInfo{
			Tx:                tx,
			Receipt:           receipt,
			EffectiveGasPrice: uint8(100),
			Signer:            &signer,
		}
	}

	return result
}

// Helper function: Generate transactions with special logs
func generateTestTransactionsWithSpecialLogs(t *testing.T) []ExecutedTxInfo {
	txInfos := make([]ExecutedTxInfo, 3)

	// Generate private key and address
	privateKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	signer := crypto.PubkeyToAddress(privateKey.PublicKey)

	// Transaction 1: Empty logs
	tx1 := ethTypes.NewTransaction(0, common.HexToAddress("0x1111111111111111111111111111111111111111"), uint256.NewInt(1), 21000, uint256.NewInt(1), []byte{})
	receipt1 := &ethTypes.Receipt{
		Status:            1,
		CumulativeGasUsed: 21000,
		Logs:              []*ethTypes.Log{},
		TxHash:            tx1.Hash(),
		GasUsed:           21000,
	}
	txInfos[0] = ExecutedTxInfo{Tx: tx1, Receipt: receipt1, EffectiveGasPrice: 100, Signer: &signer}

	// Transaction 2: Large data logs
	tx2 := ethTypes.NewTransaction(1, common.HexToAddress("0x2222222222222222222222222222222222222222"), uint256.NewInt(2), 30000, uint256.NewInt(1), []byte{})
	bigData := make([]byte, 1000)
	for i := range bigData {
		bigData[i] = byte(i % 256)
	}
	logs2 := []*ethTypes.Log{
		{
			Address: common.HexToAddress("0x2222222222222222222222222222222222222222"),
			Topics:  []common.Hash{common.HexToHash("0xabcdef")},
			Data:    bigData,
			TxHash:  tx2.Hash(),
		},
	}
	receipt2 := &ethTypes.Receipt{
		Status:            1,
		CumulativeGasUsed: 51000,
		Logs:              logs2,
		TxHash:            tx2.Hash(),
		GasUsed:           30000,
	}
	txInfos[1] = ExecutedTxInfo{Tx: tx2, Receipt: receipt2, EffectiveGasPrice: 100, Signer: &signer}

	// Transaction 3: Special character logs
	tx3 := ethTypes.NewTransaction(2, common.HexToAddress("0x3333333333333333333333333333333333333333"), uint256.NewInt(3), 25000, uint256.NewInt(1), []byte{})
	specialData := []byte{0xFF, 0x00, 0xAA, 0x55, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0}
	logs3 := []*ethTypes.Log{
		{
			Address: common.HexToAddress("0x3333333333333333333333333333333333333333"),
			Topics: []common.Hash{
				common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
				common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
				common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
				common.HexToHash("0x4444444444444444444444444444444444444444444444444444444444444444"),
			},
			Data:   specialData,
			TxHash: tx3.Hash(),
		},
	}
	receipt3 := &ethTypes.Receipt{
		Status:            1,
		CumulativeGasUsed: 76000,
		Logs:              logs3,
		TxHash:            tx3.Hash(),
		GasUsed:           25000,
	}
	txInfos[2] = ExecutedTxInfo{Tx: tx3, Receipt: receipt3, EffectiveGasPrice: 100, Signer: &signer}

	return txInfos
}

// Helper function: Generate random test transactions
func generateRandomTestTransactions(t *testing.T, count int) []ExecutedTxInfo {
	logsPerTx := make([]int, count)
	for i := range logsPerTx {
		logsPerTx[i] = i % 10 // 0-9 logs per transaction
	}
	return generateTestTransactions(t, count, logsPerTx)
}

// Helper function: Calculate total number of logs
func sumLogs(logsPerTx []int) int {
	sum := 0
	for _, count := range logsPerTx {
		sum += count
	}
	return sum
}

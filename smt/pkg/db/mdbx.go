package db

import (
	"context"
	"encoding/hex"
	"math/big"
	"unsafe"

	"fmt"
	"strings"

	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/membatch"
	"github.com/ledgerwatch/erigon/smt/pkg/utils"
	"github.com/ledgerwatch/log/v3"
)

type SmtDbTx interface {
	GetOne(bucket string, key []byte) ([]byte, error)
	Put(bucket string, key []byte, value []byte) error
	Has(bucket string, key []byte) (bool, error)
	Delete(bucket string, key []byte) error
	ForEach(bucket string, start []byte, fn func(k, v []byte) error) error
	ForPrefix(bucket string, prefix []byte, fn func(k, v []byte) error) error
	Commit() error
	Rollback()
}

const TableSmt = "HermezSmt"
const TableStats = "HermezSmtStats"
const TableAccountValues = "HermezSmtAccountValues"
const TableMetadata = "HermezSmtMetadata"
const TableHashKey = "HermezSmtHashKey"

const MetaLastRoot = "lastRoot"
const MetaDepth = "depth"
const MetaLastHeight = "lastHeight" // For X Layer, split db and ac

var HermezSmtTables = []string{TableSmt, TableStats, TableAccountValues, TableMetadata, TableHashKey}

// For X Layer, split db and ac
type EriDb struct {
	kvTxSMT     kv.RwTx
	tx          SmtDbTx
	kvTxChainDB kv.RwTx
	*EriRoDb
}

type EriRoDb struct {
	kvTxRoSMT     kv.Getter
	kvTxRoChainDB kv.Getter
}

func CreateEriDbBuckets(tx kv.RwTx) error {
	// For X Layer, split db and ac
	for _, table := range HermezSmtTables {
		err := tx.CreateBucket(table)
		if err != nil {
			return err
		}
	}
	return nil
}

func NewEriDb(txsmt kv.RwTx, txcdb kv.RwTx) *EriDb {
	// For X Layer, split db and ac
	var tx kv.RwTx = txsmt
	if tx == nil {
		tx = txcdb
	}
	return &EriDb{
		tx:          tx,
		kvTxSMT:     tx,
		kvTxChainDB: txcdb,
		EriRoDb:     NewRoEriDb(txsmt, txcdb),
	}
}

func NewRoEriDb(txsmt, txcdb kv.Getter) *EriRoDb {
	// For X Layer, split db and ac
	var tx kv.Getter = txsmt
	if tx == nil {
		tx = txcdb
	}
	return &EriRoDb{
		kvTxRoSMT:     tx,
		kvTxRoChainDB: txcdb,
	}
}

func (m *EriDb) OpenBatch(quitCh <-chan struct{}) {
	batch := membatch.NewHashBatch(m.kvTxSMT, quitCh, "./tempdb", log.New())
	defer func() {
		batch.Close()
	}()
	m.tx = batch
	m.kvTxRoSMT = batch
}

func (m *EriDb) CommitBatch() error {
	batch, ok := m.tx.(kv.PendingMutations)
	if !ok {
		return nil // don't roll back a kvRw tx
	}
	// err := m.tx.Commit()
	err := batch.Flush(context.Background(), m.kvTxSMT)
	if err != nil {
		// m.tx.Rollback()
		batch.Close()
		return err
	}
	m.tx = m.kvTxSMT
	m.kvTxRoSMT = m.kvTxSMT
	return nil
}

func (m *EriDb) RollbackBatch() {
	if m.tx == nil {
		return
	}
	if _, ok := m.tx.(kv.PendingMutations); !ok {
		return // don't roll back a kvRw tx
	}
	m.tx.(kv.PendingMutations).Close()
	m.tx = m.kvTxSMT
	m.kvTxRoSMT = m.kvTxSMT
}

func (m *EriRoDb) GetLastRoot() (*big.Int, error) {
	fmt.Printf("erirodb")
	data, err := m.kvTxRoSMT.GetOne(TableStats, []byte(MetaLastRoot))
	if err != nil {
		return big.NewInt(0), err
	}

	if data == nil {
		return big.NewInt(0), nil
	}

	return utils.ConvertHexToBigInt(string(data)), nil
}

func (m *EriDb) SetLastRoot(r *big.Int) error {
	v := utils.ConvertBigIntToHex(r)
	return m.tx.Put(TableStats, []byte(MetaLastRoot), []byte(v))
}

func (m *EriRoDb) GetDepth() (uint8, error) {
	data, err := m.kvTxRoSMT.GetOne(TableStats, []byte(MetaDepth))
	if err != nil {
		return 0, err
	}

	if data == nil {
		return 0, nil
	}

	return data[0], nil
}

func (m *EriDb) SetDepth(depth uint8) error {
	return m.tx.Put(TableStats, []byte(MetaDepth), []byte{depth})
}

func (m *EriRoDb) Get(key utils.NodeKey) (utils.NodeValue12, error) {
	keyConc := utils.ArrayToScalar(key[:])
	k := utils.ConvertBigIntToHex(keyConc)

	data, err := m.kvTxRoSMT.GetOne(TableSmt, []byte(k))
	if err != nil {
		return utils.NodeValue12{}, err
	}

	if data == nil || len(data) == 0 {
		return utils.NodeValue12{}, nil
	}

	vConc := utils.ConvertHexToBigInt(string(data))
	val := utils.ScalarToNodeValue(vConc)

	return val, nil
}

func (m *EriDb) Insert(key utils.NodeKey, value utils.NodeValue12) error {
	keyConc := utils.ArrayToScalar(key[:])
	k := utils.ConvertBigIntToHex(keyConc)

	vals := make([]*big.Int, 12)
	copy(vals, value[:])

	vConc := utils.ArrayToScalarBig(vals)
	v := utils.ConvertBigIntToHex(vConc)

	return m.tx.Put(TableSmt, []byte(k), []byte(v))
}

func (m *EriDb) Delete(key string) error {
	return m.tx.Delete(TableSmt, []byte(key))
}

func (m *EriDb) DeleteByNodeKey(key utils.NodeKey) error {
	keyConc := utils.ArrayToScalar(key[:])
	k := utils.ConvertBigIntToHex(keyConc)
	// For X Layer, optimize the byte conversion
	return m.tx.Delete(TableSmt, unsafe.Slice(unsafe.StringData(k), len(k)))
}

func (m *EriRoDb) GetAccountValue(key utils.NodeKey) (utils.NodeValue8, error) {
	keyConc := utils.ArrayToScalar(key[:])
	k := utils.ConvertBigIntToHex(keyConc)

	// For X Layer, split db and ac
	data, err := m.kvTxRoSMT.GetOne(TableAccountValues, []byte(k))
	if err != nil {
		return utils.NodeValue8{}, err
	}

	if data == nil {
		return utils.NodeValue8{}, nil
	}

	vConc := utils.ConvertHexToBigInt(string(data))
	val := utils.ScalarToNodeValue8(vConc)

	return val, nil
}

func (m *EriDb) InsertAccountValue(key utils.NodeKey, value utils.NodeValue8) error {
	keyConc := utils.ArrayToScalar(key[:])
	k := utils.ConvertBigIntToHex(keyConc)

	vals := make([]*big.Int, 8)
	copy(vals, value[:]) // Replace the loop with the copy function

	vConc := utils.ArrayToScalarBig(vals)
	v := utils.ConvertBigIntToHex(vConc)

	return m.tx.Put(TableAccountValues, []byte(k), []byte(v))
}

func (m *EriDb) InsertKeySource(key utils.NodeKey, value []byte) error {
	keyConc := utils.ArrayToScalar(key[:])

	return m.tx.Put(TableMetadata, keyConc.Bytes(), value)
}

func (m *EriDb) DeleteKeySource(key utils.NodeKey) error {
	keyConc := utils.ArrayToScalar(key[:])

	return m.tx.Delete(TableMetadata, keyConc.Bytes())
}

func (m *EriRoDb) GetKeySource(key utils.NodeKey) ([]byte, error) {
	keyConc := utils.ArrayToScalar(key[:])

	// For X Layer, split db and ac
	data, err := m.kvTxRoSMT.GetOne(TableMetadata, keyConc.Bytes())
	if err != nil {
		return nil, err
	}

	if data == nil {
		return nil, ErrNotFound
	}

	return data, nil
}

func (m *EriDb) InsertHashKey(key utils.NodeKey, value utils.NodeKey) error {
	keyConc := utils.ArrayToScalar(key[:])

	valConc := utils.ArrayToScalar(value[:])

	return m.tx.Put(TableHashKey, keyConc.Bytes(), valConc.Bytes())
}

func (m *EriDb) DeleteHashKey(key utils.NodeKey) error {
	keyConc := utils.ArrayToScalar(key[:])
	return m.tx.Delete(TableHashKey, keyConc.Bytes())
}

func (m *EriRoDb) GetHashKey(key utils.NodeKey) (utils.NodeKey, error) {
	keyConc := utils.ArrayToScalar(key[:])

	// For X Layer, split db and ac
	data, err := m.kvTxRoSMT.GetOne(TableHashKey, keyConc.Bytes())
	if err != nil {
		return utils.NodeKey{}, err
	}

	if data == nil {
		return utils.NodeKey{}, fmt.Errorf("hash key %x not found", keyConc.Bytes())
	}

	nv := big.NewInt(0).SetBytes(data)

	na := utils.ScalarToArray(nv)

	return utils.NodeKey{na[0], na[1], na[2], na[3]}, nil
}

func (m *EriRoDb) GetCode(codeHash []byte) ([]byte, error) {
	codeHash = utils.ResizeHashTo32BytesByPrefixingWithZeroes(codeHash)

	// For X Layer, split db and ac
	data, err := m.kvTxRoChainDB.GetOne(kv.Code, codeHash)
	if err != nil {
		return nil, err
	}

	if data == nil {
		return nil, fmt.Errorf("code hash %x not found", codeHash)
	}

	return data, nil
}

func (m *EriDb) AddCode(code []byte) error {
	codeHash := utils.HashContractBytecode(hex.EncodeToString(code))

	codeHashBytes, err := hex.DecodeString(strings.TrimPrefix(codeHash, "0x"))
	if err != nil {
		return err
	}

	codeHashBytes = utils.ResizeHashTo32BytesByPrefixingWithZeroes(codeHashBytes)

	// For X Layer, split db and ac
	return m.kvTxChainDB.Put(kv.Code, codeHashBytes, code)
}

func (m *EriRoDb) PrintDb() {
	// For X Layer, split db and ac
	err := m.kvTxRoSMT.ForEach(TableSmt, []byte{}, func(k, v []byte) error {
		println(string(k), string(v))
		return nil
	})
	if err != nil {
		println(err)
	}
}

func (m *EriRoDb) GetDb() map[string][]string {
	transformedDb := make(map[string][]string)

	// For X Layer, split db and ac
	err := m.kvTxRoSMT.ForEach(TableSmt, []byte{}, func(k, v []byte) error {
		hk := string(k)

		vConc := utils.ConvertHexToBigInt(string(v))
		val := utils.ScalarToNodeValue(vConc)

		truncationLength := 12

		allFirst8PaddedWithZeros := true
		for i := 0; i < 8; i++ {
			if !strings.HasPrefix(fmt.Sprintf("%016s", val[i].Text(16)), "00000000") {
				allFirst8PaddedWithZeros = false
				break
			}
		}

		if allFirst8PaddedWithZeros {
			truncationLength = 8
		}

		outputArr := make([]string, truncationLength)
		for i := 0; i < truncationLength; i++ {
			if i < len(val) {
				outputArr[i] = fmt.Sprintf("%016s", val[i].Text(16))
			} else {
				outputArr[i] = "0000000000000000"
			}
		}

		transformedDb[hk] = outputArr
		return nil
	})

	if err != nil {
		log.Error(err.Error())
	}

	return transformedDb
}

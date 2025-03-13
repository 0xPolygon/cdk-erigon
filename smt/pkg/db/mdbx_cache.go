package db

import (
	"context"
	"encoding/hex"
	"math/big"

	"fmt"
	"strings"

	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/membatch"
	"github.com/ledgerwatch/erigon/smt/pkg/utils"
	"github.com/ledgerwatch/log/v3"
)

type EriCacheDb struct {
	kvTx        kv.Tx
	cacheTx     SmtDbTx
	kvTxChainDB kv.RwTx
	*EriRoDb
}

func NewEriCacheDb(ctx context.Context, txsmt kv.Tx, txcdb kv.RwTx) *EriCacheDb {
	batch := membatch.NewHashBatch(txsmt, ctx.Done(), "./tempdb", log.New())
	defer func() {
		batch.Close()
	}()

	return &EriCacheDb{
		cacheTx:     batch,
		kvTx:        txsmt,
		kvTxChainDB: txcdb,
		EriRoDb:     NewRoEriDb(batch, txcdb),
	}
}

func (m *EriCacheDb) OpenBatch(quitCh <-chan struct{}) {}

func (m *EriCacheDb) SetCache(smtCachedMapValue map[string]map[string][]byte) {
	if smtCachedMapValue == nil {
		smtCachedMapValue = make(map[string]map[string][]byte)
	}

	mapCache, ok := m.cacheTx.(*membatch.Mapmutation)
	if !ok {
		return // don't roll back a kvRw tx
	}

	mapCache.SetCache(smtCachedMapValue)

	//batch := membatch.NewHashBatchWithCache(m.kvTx, quitCh, "./tempdb", log.New(), smtCachedMapValue)
	// WARN: cannnot close batch here, or it will clean all the cache value
	//defer func() {
	//	batch.Close()
	//}()

	//m.cacheTx = batch
	//m.kvTxRo = batch
}

func (m *EriCacheDb) RetriveAndCleanCache() (map[string]map[string][]byte, map[string]map[string][]byte) {
	mapCache, ok := m.cacheTx.(*membatch.Mapmutation)
	if !ok {
		return nil, nil // don't roll back a kvRw tx
	}

	smtCache, deltaSmtCache := mapCache.RetrieveAndCleanSmtCache(HermezSmtTables)
	mapCache.ResetCacheContent()

	return smtCache, deltaSmtCache
}

func (m *EriCacheDb) CommitBatch() error {
	batch, ok := m.cacheTx.(kv.PendingMutations)
	if !ok {
		return nil // don't roll back a kvRw tx
	}
	batch.Close()

	m.cacheTx = batch
	m.kvTxRo = batch
	return nil
}

func (m *EriCacheDb) RollbackBatch() {
	batch, ok := m.cacheTx.(kv.PendingMutations)
	if !ok {
		return // don't roll back a kvRw tx
	}
	batch.Close()

	m.cacheTx = batch
	m.kvTxRo = batch
}

func (m *EriCacheDb) GetLastRoot() (*big.Int, error) {
	data, err := m.kvTxRo.GetOne(TableStats, []byte(MetaLastRoot))
	if err != nil {
		return big.NewInt(0), err
	}

	if data == nil {
		return big.NewInt(0), nil
	}

	return utils.ConvertHexToBigInt(string(data)), nil
}

func (m *EriCacheDb) SetLastRoot(r *big.Int) error {
	v := utils.ConvertBigIntToHex(r)
	return m.cacheTx.Put(TableStats, []byte(MetaLastRoot), []byte(v))
}

func (m *EriCacheDb) GetDepth() (uint8, error) {
	data, err := m.kvTxRo.GetOne(TableStats, []byte(MetaDepth))
	if err != nil {
		return 0, err
	}

	if data == nil {
		return 0, nil
	}

	return data[0], nil
}

func (m *EriCacheDb) SetDepth(depth uint8) error {
	return m.cacheTx.Put(TableStats, []byte(MetaDepth), []byte{depth})
}

func (m *EriCacheDb) Get(key utils.NodeKey) (utils.NodeValue12, error) {
	keyConc := utils.ArrayToScalar(key[:])
	k := utils.ConvertBigIntToHex(keyConc)

	data, err := m.kvTxRo.GetOne(TableSmt, []byte(k))
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

func (m *EriCacheDb) Insert(key utils.NodeKey, value utils.NodeValue12) error {
	keyConc := utils.ArrayToScalar(key[:])
	k := utils.ConvertBigIntToHex(keyConc)

	vals := make([]*big.Int, 12)
	copy(vals, value[:])

	vConc := utils.ArrayToScalarBig(vals)
	v := utils.ConvertBigIntToHex(vConc)

	return m.cacheTx.Put(TableSmt, []byte(k), []byte(v))
}

func (m *EriCacheDb) Delete(key string) error {
	return m.cacheTx.Delete(TableSmt, []byte(key))
}

func (m *EriCacheDb) DeleteByNodeKey(key utils.NodeKey) error {
	keyConc := utils.ArrayToScalar(key[:])
	k := utils.ConvertBigIntToHex(keyConc)
	return m.cacheTx.Delete(TableSmt, []byte(k))
}

func (m *EriCacheDb) GetAccountValue(key utils.NodeKey) (utils.NodeValue8, error) {
	keyConc := utils.ArrayToScalar(key[:])
	k := utils.ConvertBigIntToHex(keyConc)

	data, err := m.kvTxRo.GetOne(TableAccountValues, []byte(k))
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

func (m *EriCacheDb) InsertAccountValue(key utils.NodeKey, value utils.NodeValue8) error {
	keyConc := utils.ArrayToScalar(key[:])
	k := utils.ConvertBigIntToHex(keyConc)

	vals := make([]*big.Int, 8)
	copy(vals, value[:]) // Replace the loop with the copy function

	vConc := utils.ArrayToScalarBig(vals)
	v := utils.ConvertBigIntToHex(vConc)

	return m.cacheTx.Put(TableAccountValues, []byte(k), []byte(v))
}

func (m *EriCacheDb) InsertKeySource(key utils.NodeKey, value []byte) error {
	keyConc := utils.ArrayToScalar(key[:])

	return m.cacheTx.Put(TableMetadata, keyConc.Bytes(), value)
}

func (m *EriCacheDb) DeleteKeySource(key utils.NodeKey) error {
	keyConc := utils.ArrayToScalar(key[:])

	return m.cacheTx.Delete(TableMetadata, keyConc.Bytes())
}

func (m *EriCacheDb) GetKeySource(key utils.NodeKey) ([]byte, error) {
	keyConc := utils.ArrayToScalar(key[:])

	data, err := m.kvTxRo.GetOne(TableMetadata, keyConc.Bytes())
	if err != nil {
		return nil, err
	}

	if data == nil {
		return nil, ErrNotFound
	}

	return data, nil
}

func (m *EriCacheDb) InsertHashKey(key utils.NodeKey, value utils.NodeKey) error {
	keyConc := utils.ArrayToScalar(key[:])

	valConc := utils.ArrayToScalar(value[:])

	return m.cacheTx.Put(TableHashKey, keyConc.Bytes(), valConc.Bytes())
}

func (m *EriCacheDb) DeleteHashKey(key utils.NodeKey) error {
	keyConc := utils.ArrayToScalar(key[:])
	return m.cacheTx.Delete(TableHashKey, keyConc.Bytes())
}

func (m *EriCacheDb) GetHashKey(key utils.NodeKey) (utils.NodeKey, error) {
	keyConc := utils.ArrayToScalar(key[:])

	data, err := m.kvTxRo.GetOne(TableHashKey, keyConc.Bytes())
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

func (m *EriCacheDb) GetCode(codeHash []byte) ([]byte, error) {
	codeHash = utils.ResizeHashTo32BytesByPrefixingWithZeroes(codeHash)

	data, err := m.kvTxRoChainDB.GetOne(kv.Code, codeHash)
	if err != nil {
		return nil, err
	}

	if data == nil {
		return nil, fmt.Errorf("code hash %x not found", codeHash)
	}

	return data, nil
}

func (m *EriCacheDb) AddCode(code []byte) error {
	codeHash := utils.HashContractBytecode(hex.EncodeToString(code))

	codeHashBytes, err := hex.DecodeString(strings.TrimPrefix(codeHash, "0x"))
	if err != nil {
		return err
	}

	codeHashBytes = utils.ResizeHashTo32BytesByPrefixingWithZeroes(codeHashBytes)

	return m.kvTxChainDB.Put(kv.Code, codeHashBytes, code)
}

func (m *EriCacheDb) PrintDb() {
	err := m.kvTxRo.ForEach(TableSmt, []byte{}, func(k, v []byte) error {
		println(string(k), string(v))
		return nil
	})
	if err != nil {
		println(err)
	}
}

func (m *EriCacheDb) GetDb() map[string][]string {
	transformedDb := make(map[string][]string)

	err := m.kvTxRo.ForEach(TableSmt, []byte{}, func(k, v []byte) error {
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

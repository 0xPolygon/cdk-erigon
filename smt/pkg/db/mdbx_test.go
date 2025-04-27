package db

import (
	"context"
	"math/big"
	"testing"

	"github.com/ledgerwatch/erigon-lib/kv/mdbx"
	"github.com/ledgerwatch/erigon/smt/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestEriDb(t *testing.T) {
	dbi, _ := mdbx.NewTemporaryMdbx(context.Background(), t.TempDir())
	tx, _ := dbi.BeginRw(context.Background())
	db := NewEriDb(tx, nil)
	err := CreateEriDbBuckets(tx)
	assert.NoError(t, err)

	// The key and value we're going to test
	key := utils.NodeKey{1, 2, 3, 4}
	value := utils.NodeValue12{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4), big.NewInt(5), big.NewInt(6),
		big.NewInt(7), big.NewInt(8), big.NewInt(9), big.NewInt(10), big.NewInt(11), big.NewInt(12)}
	noValue := utils.NodeValue12{}

	// Testing Insert method
	err = db.Insert(key, value)
	assert.NoError(t, err)

	// Testing Get method
	retrievedValue, err := db.Get(key)
	assert.NoError(t, err)
	assert.Equal(t, value, retrievedValue)

	// Test Delete method
	err = db.DeleteByNodeKey(key)
	assert.NoError(t, err)
	retrievedValue, err = db.Get(key)
	assert.NoError(t, err)
	assert.Equal(t, noValue, retrievedValue)
}

func TestEriDbBatch(t *testing.T) {
	dbi, _ := mdbx.NewTemporaryMdbx(context.Background(), t.TempDir())
	tx, _ := dbi.BeginRw(context.Background())
	db := NewEriDb(tx, nil)
	err := CreateEriDbBuckets(tx)
	assert.NoError(t, err)

	// The key and value we're going to test
	key := utils.NodeKey{1, 2, 3, 4}
	value := utils.NodeValue12{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4), big.NewInt(5), big.NewInt(6),
		big.NewInt(7), big.NewInt(8), big.NewInt(9), big.NewInt(10), big.NewInt(11), big.NewInt(12)}

	quit := make(chan struct{})

	// Start a new batch
	db.OpenBatch(quit)

	// Inserting a key-value pair within a batch
	err = db.Insert(key, value)
	assert.NoError(t, err)

	// Commit the batch
	err = db.CommitBatch()
	assert.NoError(t, err)

	// Testing Get method after committing the batch
	retrievedValue, err := db.Get(key)
	assert.NoError(t, err)
	assert.Equal(t, value, retrievedValue)

	// Start another batch
	db.OpenBatch(quit)

	// Inserting a different key-value pair within a batch
	altKey := utils.NodeKey{5, 6, 7, 8}
	altValue := utils.NodeValue12{big.NewInt(13), big.NewInt(14), big.NewInt(15), big.NewInt(16), big.NewInt(17), big.NewInt(18),
		big.NewInt(19), big.NewInt(20), big.NewInt(21), big.NewInt(22), big.NewInt(23), big.NewInt(24)}

	err = db.Insert(altKey, altValue)
	assert.NoError(t, err)

	// Testing Get method before rollback or commit, expecting no value for the altKey
	altValRes, err := db.Get(altKey)
	assert.NoError(t, err)
	assert.Equal(t, altValue, altValRes)

	// Rollback the batch
	db.RollbackBatch()

	// Testing Get method after rollback, expecting no value for the altKey
	val, err := db.Get(altKey)
	assert.NoError(t, err)
	assert.Equal(t, utils.NodeValue12{}, val)
}

func setupTestDB(t *testing.T) (*EriDb, *EriRoDb) {
	dbi, err := mdbx.NewTemporaryMdbx(context.Background(), t.TempDir())
	assert.NoError(t, err)
	tx, err := dbi.BeginRw(context.Background())
	assert.NoError(t, err)
	err = CreateEriDbBuckets(tx)
	assert.NoError(t, err)
	err = tx.Commit()
	assert.NoError(t, err)
	tx, err = dbi.BeginRw(context.Background())
	assert.NoError(t, err)
	db := NewEriDb(tx, nil)
	return db, NewRoEriDb(tx, nil)
}

func TestEriRoDb_GetLastRoot(t *testing.T) {
	db, dbro := setupTestDB(t)

	// Test when data is not present
	root, err := dbro.GetLastRoot()
	assert.NoError(t, err)
	assert.Equal(t, big.NewInt(0), root)

	// Test when data is present
	expectedRoot := big.NewInt(12345)
	err = db.SetLastRoot(expectedRoot)
	assert.NoError(t, err)

	root, err = dbro.GetLastRoot()
	assert.NoError(t, err)
	assert.Equal(t, expectedRoot, root)
}

func TestEriRoDb_GetDepth(t *testing.T) {
	db, dbro := setupTestDB(t)

	// Test when data is not present
	depth, err := dbro.GetDepth()
	assert.NoError(t, err)
	assert.Equal(t, uint8(0), depth)

	// Test when data is present
	expectedDepth := uint8(5)
	err = db.SetDepth(expectedDepth)
	assert.NoError(t, err)

	depth, err = dbro.GetDepth()
	assert.NoError(t, err)
	assert.Equal(t, expectedDepth, depth)
}

func TestEriRoDb_Get(t *testing.T) {
	db, dbro := setupTestDB(t)

	key := utils.NodeKey{1, 2, 3, 4}
	expectedValue := utils.NodeValue12{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4), big.NewInt(5), big.NewInt(6), big.NewInt(7), big.NewInt(8), big.NewInt(9), big.NewInt(10), big.NewInt(11), big.NewInt(12)}

	// Test when data is not present
	value, err := dbro.Get(key)
	assert.NoError(t, err)
	assert.Equal(t, utils.NodeValue12{}, value)

	// Test when data is present
	keyConc := utils.ArrayToScalar(key[:])
	k := utils.ConvertBigIntToHex(keyConc)
	vConc := utils.ArrayToScalarBig(expectedValue[:])
	v := utils.ConvertBigIntToHex(vConc)

	err = db.tx.Put(TableSmt, []byte(k), []byte(v))
	assert.NoError(t, err)

	value, err = dbro.Get(key)
	assert.NoError(t, err)
	assert.Equal(t, expectedValue, value)
}

func TestEriRoDb_GetAccountValue(t *testing.T) {
	db, dbro := setupTestDB(t)

	key := utils.NodeKey{1, 2, 3, 4}
	expectedValue := utils.NodeValue8{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4), big.NewInt(5), big.NewInt(6), big.NewInt(7), big.NewInt(8)}

	// Test when data is not present
	value, err := dbro.GetAccountValue(key)
	assert.NoError(t, err)
	assert.Equal(t, utils.NodeValue8{}, value)

	// Test when data is present
	err = db.InsertAccountValue(key, expectedValue)
	assert.NoError(t, err)
	value, err = dbro.GetAccountValue(key)
	assert.NoError(t, err)
	assert.Equal(t, expectedValue, value)
}

func TestEriRoDb_GetKeySource(t *testing.T) {
	db, dbro := setupTestDB(t)

	key := utils.NodeKey{1, 2, 3, 4}
	expectedValue := []byte("source_value")

	// Test when data is not present
	value, err := dbro.GetKeySource(key)
	assert.Error(t, err)
	assert.Nil(t, value)

	// Test when data is present
	err = db.InsertKeySource(key, expectedValue)
	assert.NoError(t, err)
	value, err = dbro.GetKeySource(key)
	assert.NoError(t, err)
	assert.Equal(t, expectedValue, value)
}

func TestEriRoDb_GetHashKey(t *testing.T) {
	db, dbro := setupTestDB(t)

	key := utils.NodeKey{1, 2, 3, 4}
	expectedValue := utils.NodeKey{5, 6, 7, 8}

	// Test when data is not present
	value, err := dbro.GetHashKey(key)
	assert.Error(t, err)
	assert.Equal(t, utils.NodeKey{}, value)

	// Test when data is present
	err = db.InsertHashKey(key, expectedValue)
	assert.NoError(t, err)
	value, err = dbro.GetHashKey(key)
	assert.NoError(t, err)
	assert.Equal(t, expectedValue, value)
}

/*
func codeToCodeHash(code []byte) ([]byte, error) {
	codeHash := utils.HashContractBytecode(hex.EncodeToString(code))
	codeHashBytes, err := hex.DecodeString(strings.TrimPrefix(codeHash, "0x"))
	if err != nil {
		return nil, err
	}
	return utils.ResizeHashTo32BytesByPrefixingWithZeroes(codeHashBytes), nil
}

// ! This test is commented out because the Code table is not in Smt database
func TestEriRoDb_GetCode(t *testing.T) {
	db, dbro := setupTestDB(t)

	expectedValue := []byte("code_value")
	codeHash, err := codeToCodeHash(expectedValue)
	assert.NoError(t, err)

	// Test when data is not present
	value, err := dbro.GetCode([]byte(codeHash))
	assert.Error(t, err)
	assert.Nil(t, value)

	// Test when data is present
	err = db.AddCode(expectedValue)
	assert.NoError(t, err)
	value, err = dbro.GetCode([]byte(codeHash))
	assert.NoError(t, err)
	assert.Equal(t, expectedValue, value)
}
*/

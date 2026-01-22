package hermez_db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteForkIdBlock(t *testing.T) {
	tx, cleanup := GetDbTx()
	defer cleanup()
	db := NewHermezDb(tx)

	require.NoError(t, db.WriteForkIdBlockOnce(1, 100))
	require.NoError(t, db.WriteForkIdBlockOnce(2, 200))
	require.NoError(t, db.WriteForkIdBlockOnce(3, 300))
	require.NoError(t, db.WriteForkIdBlockOnce(4, 400))

	forkBlocks, err := db.GetAllForkBlocks()
	require.NoError(t, err)
	assert.Len(t, forkBlocks, 4)

	require.NoError(t, db.DeleteForkIdBlock(200, 300))

	forkBlocks, err = db.GetAllForkBlocks()
	require.NoError(t, err)
	assert.Len(t, forkBlocks, 2)

	blockNum, found, err := db.GetForkIdBlock(1)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, uint64(100), blockNum)

	_, found, err = db.GetForkIdBlock(2)
	require.NoError(t, err)
	assert.False(t, found)

	_, found, err = db.GetForkIdBlock(3)
	require.NoError(t, err)
	assert.False(t, found)

	blockNum, found, err = db.GetForkIdBlock(4)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, uint64(400), blockNum)
}

func TestDeleteForkIdBlockEmptyRange(t *testing.T) {
	tx, cleanup := GetDbTx()
	defer cleanup()
	db := NewHermezDb(tx)

	require.NoError(t, db.WriteForkIdBlockOnce(1, 100))
	require.NoError(t, db.WriteForkIdBlockOnce(2, 200))

	require.NoError(t, db.DeleteForkIdBlock(500, 600))

	forkBlocks, err := db.GetAllForkBlocks()
	require.NoError(t, err)
	assert.Len(t, forkBlocks, 2)
}

func TestDeleteForkIdBlockSingleMatch(t *testing.T) {
	tx, cleanup := GetDbTx()
	defer cleanup()
	db := NewHermezDb(tx)

	require.NoError(t, db.WriteForkIdBlockOnce(1, 100))
	require.NoError(t, db.WriteForkIdBlockOnce(2, 200))
	require.NoError(t, db.WriteForkIdBlockOnce(3, 300))

	require.NoError(t, db.DeleteForkIdBlock(200, 200))

	forkBlocks, err := db.GetAllForkBlocks()
	require.NoError(t, err)
	assert.Len(t, forkBlocks, 2)

	_, found, err := db.GetForkIdBlock(2)
	require.NoError(t, err)
	assert.False(t, found)
}

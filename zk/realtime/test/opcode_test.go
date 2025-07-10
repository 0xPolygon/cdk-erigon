package test

import (
	"context"
	"math/big"
	"testing"

	"github.com/ledgerwatch/erigon/crypto"
	"github.com/ledgerwatch/erigon/ethclient"
	"github.com/stretchr/testify/require"
)

func TestIterativeCreate2AndDestroy(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	ctx := context.Background()
	client, err := ethclient.Dial(DefaultL2NetworkURL)
	require.NoError(t, err)

	privateKey, err := crypto.HexToECDSA(DefaultL2AdminPrivateKey[2:])
	require.NoError(t, err)

	// Deploy Factory contract
	factoryAddr := DeployFactoryContract(t, ctx, client)

	salt := big.NewInt(42) // Use a fixed salt for deterministic address
	for i := 0; i < 5; i++ {
		// Deploy initial destroy contract
		SendDeployDestroyContractTx(t, ctx, client, privateKey, factoryAddr, salt)

		destroyAddr := GetContractAddressFromFactory(t, ctx, client, factoryAddr, salt)
		code, err := RealtimeGetCode(destroyAddr)
		require.NoError(t, err)
		require.NotEmpty(t, code, "Destroy contract code should exist after deploy")

		SendDestroyContractTx(t, ctx, client, privateKey, destroyAddr)

		code, err = RealtimeGetCode(destroyAddr)
		require.NoError(t, err)
		require.Equal(t, code, "0x", "Destroy contract code should not exist after destroy")
	}
}

func TestMultipleCreate2AndDestroy(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	ctx := context.Background()
	client, err := ethclient.Dial(DefaultL2NetworkURL)
	require.NoError(t, err)

	privateKey, err := crypto.HexToECDSA(DefaultL2AdminPrivateKey[2:])
	require.NoError(t, err)

	// Deploy Factory contract
	factoryAddr := DeployFactoryContract(t, ctx, client)

	for i := 50; i < 70; i++ {
		salt := big.NewInt(int64(i))

		// Deploy initial destroy contract
		SendDeployDestroyContractTx(t, ctx, client, privateKey, factoryAddr, salt)

		destroyAddr := GetContractAddressFromFactory(t, ctx, client, factoryAddr, salt)
		code, err := RealtimeGetCode(destroyAddr)
		require.NoError(t, err)
		require.NotEmpty(t, code, "Destroy contract code should exist after deploy")

		SendDestroyContractTx(t, ctx, client, privateKey, destroyAddr)

		code, err = RealtimeGetCode(destroyAddr)
		require.NoError(t, err)
		require.Equal(t, code, "0x", "Destroy contract code should not exist after destroy")
	}
}

func TestCreate2AndDestroyInSameTx(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	ctx := context.Background()
	client, err := ethclient.Dial(DefaultL2NetworkURL)
	require.NoError(t, err)

	privateKey, err := crypto.HexToECDSA(DefaultL2AdminPrivateKey[2:])
	require.NoError(t, err)

	// Deploy create destory contract
	createDestroyAddr := DeployCreateDestroyContract(t, ctx, client)

	for i := 43; i < 53; i++ {
		salt := big.NewInt(int64(i))

		// Call createAndDestroy to create a new contract and destroy it in the same tx
		SendCreateAndDestroyTx(t, ctx, client, privateKey, createDestroyAddr, salt)

		destroyAddr := GetContractAddressFromCreateDestroy(t, ctx, client, createDestroyAddr, salt)
		code, err := RealtimeGetCode(destroyAddr)
		require.NoError(t, err)
		require.Equal(t, code, "0x", "Destroyable contract code should not exist")
	}
}

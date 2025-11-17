package jsonrpc

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/common/hexutility"
	"github.com/erigontech/erigon-lib/common/length"
	libcrypto "github.com/erigontech/erigon-lib/crypto"
	"github.com/erigontech/erigon/accounts/abi"
	"github.com/erigontech/erigon/rpc"
	"github.com/erigontech/erigon/turbo/adapter/ethapi"
)

// PrivacyAPI exposes convenience read endpoints for ACL/claim state.
type PrivacyAPI interface {
	ListOrgs(ctx context.Context) ([]PrivacyOrg, error)
	ListOrgUsers(ctx context.Context, orgName string) ([]common.Address, error)
	ListUserClaims(ctx context.Context, user common.Address) ([]PrivacyUserClaim, error)
}

var (
	claimReaderID = libcrypto.Keccak256Hash([]byte("reader"))
	claimWriterID = libcrypto.Keccak256Hash([]byte("writer"))
	claimAdminID  = libcrypto.Keccak256Hash([]byte("admin"))

	registrySelector = [4]byte{0x7b, 0x10, 0x39, 0x99} // keccak256("registry()")[:4]
)

type privacyAPI struct {
	eth          *APIImpl
	abi          abi.ABI
	registryAddr common.Address
	orgNameCache map[common.Hash]string
}

type PrivacyOrg struct {
	OrgID     common.Hash      `json:"orgId"`
	Name      string           `json:"name"`
	Exists    bool             `json:"exists"`
	Contracts []common.Address `json:"contracts"`
}

type PrivacyUserClaim struct {
	OrgID       common.Hash   `json:"orgId"`
	OrgName     string        `json:"orgName"`
	ClaimHashes []common.Hash `json:"claimHashes"`
}

func NewPrivacyAPI(eth *APIImpl) PrivacyAPI {
	parsedABI, err := abi.JSON(strings.NewReader(claimRegistryABI))
	if err != nil {
		panic(err)
	}
	return &privacyAPI{
		eth:          eth,
		abi:          parsedABI,
		orgNameCache: make(map[common.Hash]string),
	}
}

func (api *privacyAPI) ensureACLConfigured() error {
	if !api.eth.aclEnabled {
		return errors.New("ACL is disabled")
	}
	if api.eth.aclAddress == (common.Address{}) {
		return errors.New("ACL contract address is not configured")
	}
	return nil
}

func (api *privacyAPI) callRegistry(ctx context.Context, method string, args ...interface{}) ([]byte, error) {
	addr, err := api.registryAddress(ctx)
	if err != nil {
		return nil, err
	}
	input, err := api.abi.Pack(method, args...)
	if err != nil {
		return nil, err
	}
	to := addr
	data := hexutility.Bytes(input)
	callArgs := ethapi.CallArgs{
		To:   &to,
		Data: &data,
	}
	out, err := api.eth.Call(ctx, callArgs, rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber), nil)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), out...), nil
}

func (api *privacyAPI) registryAddress(ctx context.Context) (common.Address, error) {
	if api.registryAddr != (common.Address{}) {
		return api.registryAddr, nil
	}
	if err := api.ensureACLConfigured(); err != nil {
		return common.Address{}, err
	}
	data := hexutility.Bytes(registrySelector[:])
	callArgs := ethapi.CallArgs{
		To:   &api.eth.aclAddress,
		Data: &data,
	}
	out, err := api.eth.Call(ctx, callArgs, rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber), nil)
	if err != nil {
		return common.Address{}, err
	}
	if len(out) != length.Hash {
		return common.Address{}, fmt.Errorf("registry() returned %d bytes, expected %d", len(out), length.Hash)
	}
	addr := common.BytesToAddress(out[length.Hash-length.Addr:])
	if addr == (common.Address{}) {
		return common.Address{}, errors.New("checker returned zero registry address")
	}
	api.registryAddr = addr
	return addr, nil
}

func (api *privacyAPI) resolveOrgName(ctx context.Context, orgID common.Hash) (string, error) {
	if name, ok := api.orgNameCache[orgID]; ok {
		return name, nil
	}
	raw, err := api.callRegistry(ctx, "orgNames", orgID)
	if err != nil {
		return "", err
	}
	var name string
	if err := api.abi.UnpackIntoInterface(&name, "orgNames", raw); err != nil {
		return "", err
	}
	if name == "" {
		name = orgID.Hex()
	}
	api.orgNameCache[orgID] = name
	return name, nil
}

func (api *privacyAPI) ListOrgs(ctx context.Context) ([]PrivacyOrg, error) {
	raw, err := api.callRegistry(ctx, "getOrgIds")
	if err != nil {
		return nil, err
	}
	var orgIds []common.Hash
	if err := api.abi.UnpackIntoInterface(&orgIds, "getOrgIds", raw); err != nil {
		return nil, err
	}

	orgs := make([]PrivacyOrg, 0, len(orgIds))
	for _, orgID := range orgIds {
		var exists bool
		if raw, err = api.callRegistry(ctx, "orgExists", orgID); err != nil {
			return nil, err
		}
		if err := api.abi.UnpackIntoInterface(&exists, "orgExists", raw); err != nil {
			return nil, err
		}
		name, err := api.resolveOrgName(ctx, orgID)
		if err != nil {
			return nil, err
		}
		contracts, err := api.fetchOrgContracts(ctx, orgID)
		if err != nil {
			return nil, err
		}
		orgs = append(orgs, PrivacyOrg{
			OrgID:     orgID,
			Name:      name,
			Exists:    exists,
			Contracts: contracts,
		})
	}
	return orgs, nil
}

func (api *privacyAPI) ListOrgUsers(ctx context.Context, orgName string) ([]common.Address, error) {
	orgID, err := parseOrgIdentifier(orgName)
	if err != nil {
		return nil, err
	}
	raw, err := api.callRegistry(ctx, "getOrgMembers", orgID)
	if err != nil {
		return nil, err
	}
	var members []common.Address
	if err := api.abi.UnpackIntoInterface(&members, "getOrgMembers", raw); err != nil {
		return nil, err
	}
	return members, nil
}

func (api *privacyAPI) ListUserClaims(ctx context.Context, user common.Address) ([]PrivacyUserClaim, error) {
	raw, err := api.callRegistry(ctx, "getUserClaimOrgs", user)
	if err != nil {
		return nil, err
	}
	var orgIds []common.Hash
	if err := api.abi.UnpackIntoInterface(&orgIds, "getUserClaimOrgs", raw); err != nil {
		return nil, err
	}
	result := make([]PrivacyUserClaim, 0, len(orgIds))
	for _, orgID := range orgIds {
		claims := api.fetchClaimsFor(ctx, orgID, user)
		if len(claims) == 0 {
			continue
		}
		name, err := api.resolveOrgName(ctx, orgID)
		if err != nil {
			return nil, err
		}
		result = append(result, PrivacyUserClaim{
			OrgID:       orgID,
			OrgName:     name,
			ClaimHashes: claims,
		})
	}
	return result, nil
}

func (api *privacyAPI) fetchClaimsFor(ctx context.Context, orgID common.Hash, user common.Address) []common.Hash {
	ids := []common.Hash{claimReaderID, claimWriterID, claimAdminID}
	var held []common.Hash
	for _, claimID := range ids {
		raw, err := api.callRegistry(ctx, "hasScopedClaim", orgID, user, claimID)
		if err != nil {
			continue
		}
		var has bool
		if err := api.abi.UnpackIntoInterface(&has, "hasScopedClaim", raw); err != nil {
			continue
		}
		if has {
			held = append(held, claimID)
		}
	}
	return held
}

func (api *privacyAPI) fetchOrgContracts(ctx context.Context, orgID common.Hash) ([]common.Address, error) {
	raw, err := api.callRegistry(ctx, "getOrgContracts", orgID)
	if err != nil {
		return nil, err
	}
	var contracts []common.Address
	if err := api.abi.UnpackIntoInterface(&contracts, "getOrgContracts", raw); err != nil {
		return nil, err
	}
	return contracts, nil
}

func parseOrgIdentifier(input string) (common.Hash, error) {
	if strings.HasPrefix(input, "0x") {
		bytes, err := hex.DecodeString(strings.TrimPrefix(input, "0x"))
		if err != nil {
			return common.Hash{}, fmt.Errorf("invalid org identifier %s: %w", input, err)
		}
		if len(bytes) != length.Hash {
			return common.Hash{}, fmt.Errorf("org identifier must be 32 bytes, got %d", len(bytes))
		}
		return common.BytesToHash(bytes), nil
	}
	return libcrypto.Keccak256Hash([]byte(input)), nil
}

const claimRegistryABI = `[
	{"inputs":[],"name":"getOrgIds","outputs":[{"internalType":"bytes32[]","name":"","type":"bytes32[]"}],"stateMutability":"view","type":"function"},
	{"inputs":[{"internalType":"bytes32","name":"orgId","type":"bytes32"}],"name":"getOrgContracts","outputs":[{"internalType":"address[]","name":"","type":"address[]"}],"stateMutability":"view","type":"function"},
	{"inputs":[{"internalType":"bytes32","name":"orgId","type":"bytes32"}],"name":"getOrgMembers","outputs":[{"internalType":"address[]","name":"","type":"address[]"}],"stateMutability":"view","type":"function"},
	{"inputs":[{"internalType":"address","name":"user","type":"address"}],"name":"getUserClaimOrgs","outputs":[{"internalType":"bytes32[]","name":"","type":"bytes32[]"}],"stateMutability":"view","type":"function"},
	{"inputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"name":"orgExists","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},
	{"inputs":[{"internalType":"bytes32","name":"","type":"bytes32"}],"name":"orgNames","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"},
	{"inputs":[{"internalType":"bytes32","name":"orgId","type":"bytes32"},{"internalType":"address","name":"user","type":"address"},{"internalType":"bytes32","name":"claimId","type":"bytes32"}],"name":"hasScopedClaim","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"}
]`

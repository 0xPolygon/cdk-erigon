package jsonrpc

import (
	libcommon "github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon/core/vm"
	"github.com/erigontech/erigon/eth/ethconfig"
)

// ACLRuntime groups runtime ACL settings so callers can pass them around
// and apply them to a vm.Config without duplicating field assignments.
type ACLRuntime struct {
	Enabled     bool
	Address     libcommon.Address
	FailOpen    bool
	Bypass      []libcommon.Address
	OwnerBypass bool
}

// ApplyVM copies this ACLRuntime into the provided vm.Config.
func (a ACLRuntime) ApplyVM(cfg *vm.Config) {
	if cfg == nil {
		return
	}
	if !a.Enabled {
		return
	}
	cfg.SetACL(vm.ACL{
		Enabled:     true,
		Address:     a.Address,
		FailOpen:    a.FailOpen,
		Bypass:      a.Bypass,
		OwnerBypass: a.OwnerBypass,
	})
}

// ACLFromConfig maps ethconfig.Config.ACL to ACLRuntime.
func ACLFromConfig(c *ethconfig.Config) ACLRuntime {
	if c == nil {
		return ACLRuntime{}
	}
	return ACLRuntime{
		Enabled:     c.ACL.Enabled,
		Address:     c.ACL.ContractAddress,
		FailOpen:    c.ACL.FailOpen,
		Bypass:      append([]libcommon.Address(nil), c.ACL.Bypass...),
		OwnerBypass: c.ACL.OwnerBypass,
	}
}

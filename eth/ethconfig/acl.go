package ethconfig

import (
    "github.com/erigontech/erigon-lib/common"
)

// ACLConfig holds optional Access-Control-Firewall settings.
// This is a stub for future integration; it is not wired into flags or runtime yet.
type ACLConfig struct {
    Enabled          bool           // enable ACL admission checks at the node level
    ContractAddress  common.Address // ACL proxy contract to query (checkPermittedOrRevert)
    FailOpen         bool           // if true, bypass on ACL call failure (not recommended in production)
    // Bypass allows designated superusers to skip ACL checks entirely.
    // Useful for bootstrap/admin operations like deploying contracts or updating ACL itself.
    Bypass           []common.Address
    // OwnerBypass, if true, treats the owner() of the ACL proxy contract as a superuser
    // who bypasses ACL checks. Owner is resolved via a STATICCALL to the ACL proxy.
    OwnerBypass      bool
}

// DefaultACLConfig provides disabled-by-default settings.
var DefaultACLConfig = ACLConfig{
    Enabled:         false,
    ContractAddress: common.Address{},
    FailOpen:        false,
    Bypass:          nil,
    OwnerBypass:     false,
}

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
}

// DefaultACLConfig provides disabled-by-default settings.
var DefaultACLConfig = ACLConfig{
    Enabled:         false,
    ContractAddress: common.Address{},
    FailOpen:        false,
}

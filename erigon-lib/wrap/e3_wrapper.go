package wrap

import (
	"github.com/ledgerwatch/erigon-lib/kv"
)

type TxContainer struct {
	Tx    kv.RwTx
	TxSmt kv.RwTx
	Ttx   kv.TemporalTx
}

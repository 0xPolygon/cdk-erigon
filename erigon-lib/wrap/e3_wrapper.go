package wrap

import (
	"github.com/ledgerwatch/erigon-lib/kv"
)

type TxContainer struct {
	Tx  kv.RwTx
	Ttx kv.TemporalTx

	// For X Layer, split db
	TxSmt kv.RwTx
}

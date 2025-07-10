package jsonrpc

type StreamCriteria struct {
	NewHeads             bool
	TransactionExtraInfo bool
	TransactionReceipt   bool
	TransactionInnerTxs  bool
}

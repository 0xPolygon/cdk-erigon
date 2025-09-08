package utils

type ProcessStep struct {
	ID  uint64
	Key string
}

var (
	// RPC
	StepRPCReceiveTx    = ProcessStep{15010, "xlayer_rpc_receive_tx"}
	StepRPCReceiveBlock = ProcessStep{15060, "xlayer_rpc_receive_block"}
	StepRPCFinishBlock  = ProcessStep{15062, "xlayer_rpc_finish_block"}

	// Realtime
	StepRealtimeReceiveTx  = ProcessStep{15061, "xlayer_realtime_receive_tx"}
	StepRealtimeCloseBlock = ProcessStep{15063, "xlayer_realtime_close_block"}

	// Sequencer
	StepSeqBeginBlock        = ProcessStep{15030, "xlayer_seq_begin_block"}
	StepSeqReceiveTx         = ProcessStep{15032, "xlayer_seq_receive_tx"}
	StepSeqPackageTx         = ProcessStep{15034, "xlayer_seq_package_tx"}
	StepSeqEndBlock          = ProcessStep{15036, "xlayer_seq_end_block"}
	StepSeqVerifyBlockBegin  = ProcessStep{15038, "xlayer_seq_verify_block_begin"}
	StepSeqVerifyBlockResult = ProcessStep{15040, "xlayer_seq_verify_block_result"}
	StepSeqDsSent            = ProcessStep{15042, "xlayer_seq_ds_sent"}
)

const (
	Chain = "xlayer"

	ServiceNameSequencer = "okx-defi-xlayer-egseqz-pro"
	ServiceNameRPC       = "okx-defi-xlayer-rpcpay-pro"

	Business = "xlayer"
	ChainID  = 196

	Client               string = ""
	Status               string = ""
	Index                string = ""
	InnerIndex           string = ""
	ReferId              string = ""
	DepositConfirmHeight string = ""
	TokenID              string = ""
	MevSupplier          string = ""
	BusinessHash         string = ""
	ContractAddress      string = ""
)

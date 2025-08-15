package utils

const (
	PreForkId7BlockGasLimit = 30_000_000
	ForkId7BlockGasLimit    = 18446744073709551615 // 0xffffffffffffffff
	ForkId8BlockGasLimit    = 1125899906842624     // 0x4000000000000
	
	ForkId13BlockGasLimit   = 30000000             // 30000000
)

func GetBlockGasLimitForFork(forkId uint64) uint64 {
	if forkId >= 7 {
		// the gas limit for fork 8 was actually used for fork 7 and above after a re-hash
		return ForkId8BlockGasLimit
	}

	return PreForkId7BlockGasLimit
}

func GetBlockGasLimit() uint64 {
	return ForkId13BlockGasLimit
}

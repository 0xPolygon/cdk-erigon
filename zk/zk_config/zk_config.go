package zk_config

var ZKDynamicConfigPath string
var ZkUnionConfigPath string
var Type1Enabled bool

func IsType1Rollup() bool {
	return Type1Enabled
}

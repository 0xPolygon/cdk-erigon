package apollo

import (
	"testing"
	"time"

	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/node/nodecfg"
	"github.com/stretchr/testify/require"
)

func TestApolloClient(t *testing.T) {
	// Skip this test as it requires a running Apollo server at http://127.0.0.1:18080
	t.Skip("Skipping TestApolloClient because it requires a running Apollo server at http://127.0.0.1:18080")

	c := &ethconfig.Config{
		Zk: &ethconfig.Zk{
			XLayer: ethconfig.XLayerConfig{
				Apollo: ethconfig.ApolloClientConfig{
					IP:            "http://127.0.0.1:18080",
					AppID:         "SampleApp",
					NamespaceName: "jsonrpc-tester.txt",
					Enable:        true,
				},
			},
		},
	}
	client := NewClient(c)

	// Test load config cache
	loaded := client.LoadConfig()
	require.Equal(t, true, loaded)

	apolloNodeCfg := &UnsafeGetApolloConfig().NodeCfg
	apolloEthCfg := &UnsafeGetApolloConfig().EthCfg

	t.Log("Logging init apollo config")
	logTestNodeConfig(t, apolloNodeCfg)
	logTestEthConfig(t, apolloEthCfg)

	// Fire config changes on both ethconfig and nodecfg
	time.Sleep(30 * time.Second)

	t.Log("Logging after apollo config")
	logTestNodeConfig(t, apolloNodeCfg)
	logTestEthConfig(t, apolloEthCfg)
}

func logTestEthConfig(t *testing.T, ethCfg *ethconfig.Config) {
	t.Log("---------- Logging eth backend config ----------")
	t.Log("zkevm.apollo-enabled: ", ethCfg.Zk.XLayer.Apollo.Enable)
	t.Log("zkevm.apollo-ip-addr: ", ethCfg.Zk.XLayer.Apollo.IP)
	t.Log("zkevm.apollo-app-id: ", ethCfg.Zk.XLayer.Apollo.AppID)
	t.Log("zkevm.apollo-namespace-name: ", ethCfg.Zk.XLayer.Apollo.NamespaceName)
	t.Log("zkevm.nacos-urls: ", ethCfg.Zk.XLayer.Nacos.URLs)
	t.Log("zkevm.nacos-namespace-id: ", ethCfg.Zk.XLayer.Nacos.NamespaceId)
	t.Log("zkevm.nacos-application-name: ", ethCfg.Zk.XLayer.Nacos.ApplicationName)
	t.Log("zkevm.nacos-external-listen-addr: ", ethCfg.Zk.XLayer.Nacos.ExternalListenAddr)
	t.Log("zkevm.l1-rollup-id: ", ethCfg.Zk.L1RollupId)
	t.Log("zkevm.l1-first-block: ", ethCfg.Zk.L1FirstBlock)
	t.Log("zkevm.l1-block-range: ", ethCfg.Zk.L1BlockRange)
	t.Log("zkevm.l1-query-delay: ", ethCfg.Zk.L1QueryDelay)
	t.Log("zkevm.sequencer-block-seal-time: ", ethCfg.Zk.SequencerBlockSealTime)
	t.Log("zkevm.sequencer-batch-seal-time: ", ethCfg.Zk.SequencerBatchSealTime)
}

func logTestNodeConfig(t *testing.T, nodeCfg *nodecfg.Config) {
	t.Log("---------- Logging node config ----------")
	t.Log("http.addr: ", nodeCfg.Http.HttpListenAddress)
	t.Log("http.port: ", nodeCfg.Http.HttpPort)
	t.Log("http.api: ", nodeCfg.Http.API)
	t.Log("http.timeouts.read: ", nodeCfg.Http.HTTPTimeouts.ReadTimeout)
	t.Log("http.timeouts.write: ", nodeCfg.Http.HTTPTimeouts.WriteTimeout)
	t.Log("ws: ", nodeCfg.Http.WebsocketEnabled)
}

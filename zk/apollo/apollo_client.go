package apollo

import (
	"fmt"
	"strings"

	"github.com/apolloconfig/agollo/v4"
	"github.com/apolloconfig/agollo/v4/env/config"
	"github.com/apolloconfig/agollo/v4/storage"
	"github.com/ledgerwatch/erigon/turbo/logging"
	"github.com/urfave/cli/v2"

	"github.com/ledgerwatch/erigon/cmd/utils"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	erigoncli "github.com/ledgerwatch/erigon/turbo/cli"
	"github.com/ledgerwatch/erigon/turbo/debug"
	"github.com/ledgerwatch/log/v3"
)

// Client is the apollo client
type Client struct {
	*agollo.Client
	namespaceMap map[string]string
	flags        []cli.Flag
}

// NewClient creates a new apollo client
func NewClient(ethCfg *ethconfig.Config) *Client {
	if ethCfg == nil || !ethCfg.Zk.XLayer.Apollo.Enable || ethCfg.Zk.XLayer.Apollo.IP == "" || ethCfg.Zk.XLayer.Apollo.AppID == "" || ethCfg.Zk.XLayer.Apollo.NamespaceName == "" {
		log.Info(fmt.Sprintf("apollo is not enabled, config: %+v", ethCfg.Zk.XLayer.Apollo))
		return nil
	}
	c := &config.AppConfig{
		IP:             ethCfg.Zk.XLayer.Apollo.IP,
		AppID:          ethCfg.Zk.XLayer.Apollo.AppID,
		NamespaceName:  ethCfg.Zk.XLayer.Apollo.NamespaceName,
		Cluster:        "default",
		IsBackupConfig: false,
	}

	client, err := agollo.StartWithConfig(func() (*config.AppConfig, error) {
		return c, nil
	})
	if err != nil {
		utils.Fatalf("failed init apollo: %v", err)
	}

	nsMap := make(map[string]string)
	namespaces := strings.Split(ethCfg.Zk.XLayer.Apollo.NamespaceName, ",")
	for _, namespace := range namespaces {
		prefix, err := getNamespacePrefix(namespace)
		if err != nil {
			utils.Fatalf("failed init apollo: %v", err)
		}

		_, found := nsMap[prefix]
		if found {
			utils.Fatalf("failed init apollo: duplicate apollo namespace prefix being set")
		}
		nsMap[prefix] = namespace
	}

	apc := &Client{
		Client:       client,
		namespaceMap: nsMap,
		flags:        append(erigoncli.DefaultFlags, debug.Flags...),
	}

	apc.flags = append(apc.flags, utils.MetricFlags...)
	apc.flags = append(apc.flags, logging.Flags...)

	client.AddChangeListener(&CustomChangeListener{apc})

	return apc
}

// LoadConfig loads the config
func (c *Client) LoadConfig() (loaded bool) {
	if c == nil {
		return false
	}
	for prefix, namespace := range c.namespaceMap {
		cache := c.GetConfigCache(namespace)
		if cache != nil {
			cache.Range(func(key, value interface{}) bool {
				loaded = true
				switch prefix {
				case Sequencer:
					c.loadSequencer(value)
				case JsonRPC:
					c.loadJsonRPC(value)
				case L2GasPricer:
					c.loadL2GasPricer(value)
				case Pool:
					c.loadPool(value)
				}
				return true
			})
		}
	}
	return
}

// CustomChangeListener is the custom change listener
type CustomChangeListener struct {
	*Client
}

// OnChange is the change listener
func (c *CustomChangeListener) OnChange(changeEvent *storage.ChangeEvent) {
	for key, value := range changeEvent.Changes {
		if value.ChangeType == storage.MODIFIED || value.ChangeType == storage.ADDED {
			if value.ChangeType == storage.ADDED {
				value.OldValue = ""
			}

			suffix, err := getNamespaceSuffix(changeEvent.Namespace)
			if err != nil {
				log.Warn(fmt.Sprintf("not processing change event: %v", err))
				continue
			}
			switch suffix {
			case Halt:
				c.fireHalt(key, value)
				continue
			}

			prefix, err := getNamespacePrefix(changeEvent.Namespace)
			if err != nil {
				log.Warn(fmt.Sprintf("not processing change event: %v", err))
				continue
			}

			ctx, config, err := c.getConfigContext(value.NewValue)
			if err != nil {
				log.Error(fmt.Sprintf("invalid new value, prefix: %s, newValue: %v, err: %v", prefix, value.NewValue, err))
				return
			}

			switch prefix {
			case Sequencer:
				c.fireSequencer(ctx, value)
			case JsonRPC:
				c.fireJsonRPC(ctx, value)
			case L2GasPricer:
				c.fireL2GasPricer(ctx, value)
			case Pool:
				c.firePool(ctx, value)
			}

			UnsafeGetApolloConfig().Lock()
			defer UnsafeGetApolloConfig().Unlock()

			UnsafeGetApolloConfig().EthCfg.XLayer.ApolloChanged = getChangedFlags(config)
			ethCfgStream.Pub(&UnsafeGetApolloConfig().EthCfg)
			nodeCfgStream.Pub(&UnsafeGetApolloConfig().NodeCfg)
		}
	}
}

// OnNewestChange is the newest change listener
func (c *CustomChangeListener) OnNewestChange(event *storage.FullChangeEvent) {
}

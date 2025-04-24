package apollo

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/apolloconfig/agollo/v4/storage"
	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v2"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/eth/ethconfig"
	"github.com/ledgerwatch/erigon/node/nodecfg"
	"github.com/ledgerwatch/log/v3"
)

func (c *Client) getConfigContext(value interface{}) (*cli.Context, error) {
	config := make(map[string]interface{})
	err := yaml.Unmarshal([]byte(value.(string)), config)
	if err != nil {
		log.Error(fmt.Sprintf("failed to load config: %v error: %v", value, err))
		return nil, err
	}

	// sets global flags to value in apollo config
	ctx := createMockContext(c.flags)
	for key, value := range config {
		if !ctx.IsSet(key) {
			if reflect.ValueOf(value).Kind() == reflect.Slice {
				sliceInterface := value.([]interface{})
				s := make([]string, len(sliceInterface))
				for i, v := range sliceInterface {
					s[i] = fmt.Sprintf("%v", v)
				}
				err := ctx.Set(key, strings.Join(s, ","))
				if err != nil {
					return nil, fmt.Errorf("failed setting %s flag with values=%s error=%s", key, s, err)
				}
			} else {
				err := ctx.Set(key, fmt.Sprintf("%v", value))
				if err != nil {
					return nil, fmt.Errorf("failed setting %s flag with value=%v error=%s", key, value, err)
				}
			}
		}
	}

	return ctx, nil
}

const (
	HaltKey         = "Halt"
	NamespaceSplits = 2
	maxHaltDelay    = 20
)

func (c *Client) fireHalt(key string, value *storage.ConfigChange) {
	switch key {
	case HaltKey:
		if value.OldValue.(string) != value.NewValue.(string) {
			random, _ := rand.Int(rand.Reader, big.NewInt(maxHaltDelay))
			delay := time.Second * time.Duration(random.Int64())
			log.Info(fmt.Sprintf("halt changed from %s to %s delay halt %v", value.OldValue.(string), value.NewValue.(string), delay))
			time.Sleep(delay)
			os.Exit(1)
		}
	}
}

var ethCfgStream = &NotificationPubSub[ethconfig.Config]{}
var nodeCfgStream = &NotificationPubSub[nodecfg.Config]{}

type NotificationPubSub[T ethconfig.Config | nodecfg.Config] struct {
	chans map[uint]chan *T
	id    uint
	mu    sync.RWMutex
}

func (ps *NotificationPubSub[T]) Sub() (ch chan *T, remove func()) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.chans == nil {
		ps.chans = make(map[uint]chan *T)
	}
	ps.id++
	id := ps.id
	ch = make(chan *T, 8)
	ps.chans[id] = ch
	return ch, func() { ps.remove(id) }
}

func (ps *NotificationPubSub[T]) Pub(reply *T) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	for _, ch := range ps.chans {
		common.PrioritizedSend(ch, reply)
	}
}

func (ps *NotificationPubSub[T]) remove(id uint) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ch, ok := ps.chans[id]
	if !ok { // double-unsubscribe support
		return
	}
	close(ch)
	delete(ps.chans, id)
}

func GetEthConfigStream() *NotificationPubSub[ethconfig.Config] {
	return ethCfgStream
}

func GetNodeConfigStream() *NotificationPubSub[nodecfg.Config] {
	return nodeCfgStream
}

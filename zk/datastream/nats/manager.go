// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package nats

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// Config contains configuration parameters for the NATS server
type Config struct {
	// Host is the hostname or IP to bind to
	Host string

	// Port is the port to listen on. Use -1 for a random port
	Port int

	// ServerName is the name of the NATS server, used for identification
	ServerName string

	// ClusterName is the name of the NATS cluster
	ClusterName string

	// HTTPHost is the host for the HTTP monitoring interface
	HTTPHost string

	// HTTPPort is the port for the HTTP monitoring interface (0 means disabled)
	HTTPPort int

	// JetStreamEnabled enables JetStream for persistent messaging
	JetStreamEnabled bool

	// StorageDir is the directory where JetStream will store data
	StorageDir string

	// MaxMemory is the maximum memory that can be used (in bytes)
	MaxMemory int64

	// MaxStorage is the maximum storage that can be used (in bytes)
	MaxStorage int64

	// Debug enables debug logging
	Debug bool
}

// DefaultConfig returns default configuration values
func DefaultConfig() Config {
	return Config{
		Host:             "127.0.0.1",
		Port:             4222,
		ServerName:       "erigon-nats",
		ClusterName:      "erigon-cluster",
		HTTPHost:         "127.0.0.1",
		HTTPPort:         8222,
		JetStreamEnabled: true,
		StorageDir:       "",
		MaxMemory:        1 * 1024 * 1024 * 1024,  // 1GB
		MaxStorage:       10 * 1024 * 1024 * 1024, // 10GB
		Debug:            false,
	}
}

// Manager manages the lifecycle of an embedded NATS server
type Manager struct {
	config Config
	server *server.Server
	logger log.Logger
	lock   sync.RWMutex
	url    string
}

// NewManager creates a new NATS server manager
func NewManager(config Config, logger log.Logger) *Manager {
	return &Manager{
		config: config,
		logger: logger,
	}
}

// Start initializes and starts the NATS server
func (m *Manager) Start() error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.server != nil {
		return fmt.Errorf("NATS server already running")
	}

	opts := server.Options{
		Host:                  m.config.Host,
		Port:                  m.config.Port,
		ServerName:            m.config.ServerName,
		NoLog:                 !m.config.Debug,
		NoSigs:                true,
		JetStream:             m.config.JetStreamEnabled,
		Debug:                 m.config.Debug,
		TraceVerbose:          m.config.Debug,
		MaxPayload:            8 * 1024 * 1024,  // 8MB
		MaxPending:            64 * 1024 * 1024, // 64MB
		DisableShortFirstPing: false,
		Cluster: server.ClusterOpts{
			Name: m.config.ClusterName,
		},
	}

	// Enable HTTP monitoring if port is non-zero
	if m.config.HTTPPort > 0 {
		opts.HTTPHost = m.config.HTTPHost
		opts.HTTPPort = m.config.HTTPPort
	}

	// Configure JetStream if enabled
	if m.config.JetStreamEnabled {
		// Use provided storage directory or generate a temp one
		storeDir := m.config.StorageDir
		if storeDir == "" {
			storeDir = filepath.Join("data", "nats-storage")
		}

		opts.StoreDir = storeDir
		opts.JetStreamMaxMemory = m.config.MaxMemory
		opts.JetStreamMaxStore = m.config.MaxStorage
	}

	// Create the server
	natsServer, err := server.NewServer(&opts)
	if err != nil {
		return fmt.Errorf("failed to create NATS server: %w", err)
	}

	// Start the server
	go natsServer.Start()

	// Wait for server to be ready
	if !natsServer.ReadyForConnections(5 * time.Second) {
		natsServer.Shutdown()
		return fmt.Errorf("NATS server failed to start within timeout")
	}

	m.server = natsServer
	m.url = natsServer.ClientURL()

	if m.config.Debug {
		m.server.ConfigureLogger()
	}

	m.logger.Info("NATS server started successfully",
		"url", m.url,
		"name", m.config.ServerName,
		"cluster", m.config.ClusterName,
		"http_monitoring", fmt.Sprintf("%s:%d", m.config.HTTPHost, m.config.HTTPPort),
		"jetstream", m.config.JetStreamEnabled,
		"storage_dir", opts.StoreDir)

	return nil
}

// Stop shuts down the NATS server
func (m *Manager) Stop() {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.server == nil {
		return
	}

	m.logger.Info("Shutting down NATS server")
	m.server.Shutdown()
	m.server = nil
	m.url = ""
}

// URL returns the client URL for connecting to the NATS server
func (m *Manager) URL() (string, error) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	if m.server == nil {
		return "", fmt.Errorf("NATS server not running")
	}

	return m.url, nil
}

// Connect creates a new NATS connection to the managed server
func (m *Manager) Connect(options ...nats.Option) (*nats.Conn, error) {
	url, err := m.URL()
	if err != nil {
		return nil, err
	}

	return nats.Connect(url, options...)
}

// Server returns the underlying NATS server instance
func (m *Manager) Server() *server.Server {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.server
}

// IsRunning returns true if the NATS server is currently running
func (m *Manager) IsRunning() bool {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.server != nil
}

# NATS JetStream Tests

This directory contains tests that verify the behavior of NATS JetStream as an embedded service, which can be used for message streaming in the Erigon datastreaming infrastructure.

## Test Overview

### Basic NATS Functionality (TestEmbeddedNatsServer)

`nats_embedded_test.go` demonstrates:
- Starting an embedded NATS server
- Creating a stream
- Publishing a message
- Verifying a subscriber receives the message

This test confirms the basic functionality of NATS as an embedded service within our application.

### Server-Client Architecture (TestNatsServerClientArrangement)

`nats_server_client_test.go` demonstrates:
- Creating a single embedded NATS server
- Setting up separate server-side and client-side connections
- Publishing messages from the server-side
- Receiving messages on the client-side

This test simulates the architecture where one node acts as a server (e.g., sequencer) and another acts as a client (e.g., RPC node).

### Bidirectional Communication (TestNatsBidirectionalCommunication)

`nats_bidirectional_test.go` demonstrates:
- Setting up a server and client connection to a NATS server
- Creating separate streams for each direction of communication
- Client sending a message to the server
- Server receiving the message and responding back
- Client receiving the server's response

This test verifies that bidirectional communication works properly, which is important for scenarios where nodes need to exchange information in both directions.

### Cluster Replication (TestNatsClusterReplication)

`nats_cluster_test.go` demonstrates:
- Creating a cluster of multiple NATS servers (3 in this case)
- Configuring servers to form a proper cluster with routes between them
- Creating a stream with replication across all cluster nodes
- Publishing messages to one server
- Verifying messages are replicated and can be consumed from another server
- Testing fault tolerance by shutting down one server and verifying data is still accessible

This test is crucial for understanding how NATS can be deployed in a distributed environment with high availability requirements, ensuring messages are replicated across multiple nodes.

### Embedded Cluster (TestNatsEmbeddedCluster)

`nats_embedded_cluster_test.go` demonstrates:
- Creating a cluster of multiple embedded NATS servers (3 in this case)
- Configuring the embedded servers to form a proper cluster with routes between them
- Creating a stream with replication across all cluster nodes
- Publishing messages to one server
- Verifying messages are replicated and can be consumed from another server
- Testing fault tolerance by shutting down one server and verifying data is still accessible

This test confirms that we can run multiple embedded NATS servers within the same process and have them form a proper cluster with replicated JetStream streams, which is useful for testing or specialized deployment scenarios.

### Leaf Node Architecture (TestNatsLeafNodeSetup)

`nats_leaf_node_test.go` demonstrates:
- Setting up a hub and spoke architecture with a central NATS server and multiple leaf nodes
- Creating streams on each node (hub and leaves)
- Verifying that JetStream domains are isolated between nodes
- Confirming each node can access its own streams but not streams from other nodes

This test explores the leaf node topology, which can be useful for geo-distributed deployments where each region has its own JetStream domain but still participates in the broader NATS network.

### Hub-to-Leaf Distribution (TestNatsHubToLeafDistribution)

`nats_hub_to_leaf_test.go` demonstrates:
- Setting up a hub and spoke architecture with a central NATS server and multiple leaf nodes
- Publishing messages from the hub that are received by all leaf nodes
- Testing request-reply patterns between hub and leaves
- Verifying that multiple messages published on the hub are reliably received by all leaves

This test shows how leaf nodes can subscribe to subjects and receive messages published by the hub, which is useful for broadcasting data from a central node to edge nodes. It also demonstrates bidirectional communication with request-reply patterns.

### Hybrid Messaging Approach (TestNatsHybridMessaging)

`nats_hybrid_messaging_test.go` demonstrates:
- Setting up a hub with JetStream enabled for persistence
- Connecting multiple leaf nodes without JetStream to the hub
- Publishing messages to both JetStream (for persistence) and regular NATS subjects (for distribution)
- Verifying messages are both persisted on the hub and delivered to leaf nodes
- Demonstrating recovery scenarios where a leaf node can reconnect and recover missed messages from the hub's JetStream

This test showcases a practical hybrid approach that provides both persistent storage on the hub and real-time distribution to leaf nodes. It's particularly useful for scenarios where you need reliable message delivery, persistence for history/replay, and the ability to recover after disconnections.

### Late-Joining Leaves with Replay Options (TestNatsLateJoiningLeaves)

`nats_late_joining_leaves_test.go` demonstrates:
- A hub server publishing messages to JetStream before any leaf nodes connect
- Late-joining leaf nodes connecting at different times and accessing the hub's JetStream
- Different replay options for late-joining nodes:
  - Replaying all messages from the beginning
  - Starting from a specific sequence number
  - Starting from a specific timestamp
- Receiving new messages as they are published after joining

This test is especially useful for situations where new clients need to join an existing system and catch up on historical data, such as when adding new RPC nodes that need to sync from a specific block height or from the beginning of the chain.

### JetStream Replication to Leaf Nodes (TestNatsJetStreamReplicationToLeaf)

`nats_jetstream_replication_to_leaf_test.go` demonstrates:
- Setting up both hub and leaf nodes with JetStream enabled
- Publishing messages to the hub's JetStream
- Implementing custom replication from hub to leaf by reading from the hub and republishing to the leaf
- Verifying that messages can be read directly from the leaf's local JetStream

This test showcases how to create a fully distributed architecture where each leaf node maintains its own local copy of data from the hub. This is particularly useful for high-availability scenarios where leaf nodes need local access to data even if the hub becomes temporarily unavailable, or for edge computing scenarios where local processing of data is required.

### Automatic JetStream Replication (TestNatsAutoReplication)

`nats_auto_replication_test.go` demonstrates:
- Setting up a proper NATS cluster with multiple servers
- Configuring JetStream with a replication factor greater than 1
- Verifying automatic replication of messages across cluster nodes
- Testing fault tolerance by shutting down one server and confirming data availability

This test shows how to use NATS' built-in clustering capabilities to automatically replicate JetStream data without any custom code. By simply setting the `Replicas` field in the stream configuration, NATS handles all the replication internally, providing high availability and fault tolerance.

### Stream Sourcing and Mirroring (TestNatsStreamSourcing)

`nats_stream_sourcing_test.go` demonstrates:
- Using JetStream's built-in Mirror feature to create exact copies of streams
- Using the Sources feature to combine multiple streams into one
- Automatic propagation of new messages to mirrors and sourced streams
- No custom code needed for replication - it's all handled by JetStream

This test showcases JetStream's powerful stream replication capabilities. Mirrors create exact copies of other streams, while Sources allow aggregating data from multiple streams. Both approaches provide automatic, continuous replication with no manual coding required.

### Dynamic Cluster Scaling (TestNatsDynamicCluster)

`nats_dynamic_cluster_test.go` demonstrates:
- Starting with a single NATS server with JetStream
- Dynamically adding additional servers to the cluster at runtime
- Automatic replication of JetStream data to newly joined servers
- Reading from new servers to verify full data availability
- Fault tolerance with continued operation after the original seed server is shut down

This test showcases how to scale a NATS cluster horizontally by adding new servers dynamically as needed. Unlike pre-configured clusters, this approach allows starting with minimal infrastructure and growing incrementally as demand increases. The test also demonstrates how the cluster maintains high availability even when the original seed server is removed, with new servers still able to publish and consume messages reliably.

### Message Ordering (TestNatsMessageOrdering)

`nats_message_ordering_test.go` demonstrates:
- Publishing multiple messages (100) to a NATS stream
- Verifying that messages are received in the same order they were published

This test is crucial for our datastreaming needs, as it confirms that NATS JetStream preserves the ordering of messages, which is essential for blockchain data consistency.

### Message Persistence (TestNatsPersistence)

`nats_persistence_test.go` demonstrates:
- Configuring NATS with persistent storage
- Publishing messages to a stream
- Shutting down the server
- Restarting the server with the same storage location
- Verifying that previously published messages are still available

This test verifies NATS' ability to persist data, which is critical for recovery scenarios in our datastreaming architecture.

## Running the Tests

From this directory, run:

```
go test -v
```

## Integration with Erigon Datastreamer

These tests demonstrate key capabilities that will be used when implementing NATS as the datastreamer for Erigon:

1. **Embedded Service**: NATS can run as an embedded service within the Erigon node, eliminating the need for a separate server deployment.

2. **Server-Client Architecture**: The tests show how to set up a server-client relationship between nodes, which maps to our sequencer and RPC nodes setup.

3. **Bidirectional Communication**: NATS supports bidirectional message exchange, allowing for more complex node interaction patterns if needed.

4. **Clustering and Replication**: NATS can operate in a clustered mode with message replication, providing high availability and fault tolerance for critical blockchain data.

5. **Multiple Topologies**: NATS supports various deployment topologies including simple clusters, embedded clusters, leaf nodes, and superclusters, offering flexibility for different network architectures.

6. **Message Ordering**: NATS JetStream guarantees that messages are delivered in the same order they were published, which is essential for blockchain data synchronization.

7. **Persistence**: JetStream's persistence capabilities ensure that data can be recovered in case of node restarts or failures.

8. **Performance**: The tests show that NATS can handle high-throughput messaging with low latency, which is important for real-time blockchain data streaming. 

## Choosing the Right Leaf Node Pattern

The tests demonstrate several different ways to implement a hub-and-spoke architecture with NATS leaf nodes, each with different trade-offs:

1. **Basic Leaf Node Setup** (`nats_leaf_node_test.go`): Each leaf node has its own isolated JetStream domain. This is useful when you want each edge node to have its own persistent storage that's independent of the hub.

2. **Hub-to-Leaf Distribution** (`nats_hub_to_leaf_test.go`): Messages published on the hub are automatically received by all leaf nodes that have subscribed to the relevant subjects. This works with regular NATS messaging (not JetStream) and is perfect for real-time broadcasting scenarios.

3. **Hybrid Messaging Approach** (`nats_hybrid_messaging_test.go`): Combines the benefits of JetStream on the hub (for persistence and recovery) with regular NATS messaging for real-time distribution to leaf nodes. This pattern is recommended when you need:
   - Centralized persistence on the hub
   - Real-time message distribution to leaf nodes
   - The ability for leaf nodes to recover missed messages after disconnections
   - Optional: Leaf nodes can connect directly to the hub to replay historical data

4. **Late-Joining Leaves with Replay Options** (`nats_late_joining_leaves_test.go`): Extends the hybrid approach with more sophisticated replay options for late-joining nodes:
   - New nodes can replay all messages from the beginning of the stream
   - Nodes can start consuming from a specific sequence number (e.g., block height)
   - Nodes can start from a specific timestamp
   This pattern is ideal when you need to support nodes joining an existing network at any time with flexible replay options.

5. **JetStream Replication to Leaf Nodes** (`nats_jetstream_replication_to_leaf_test.go`): Creates a fully distributed architecture where:
   - Both hub and leaf nodes have JetStream enabled
   - Data is replicated from hub to leaf nodes
   - Leaf nodes can access data locally without connecting to the hub
   - Provides higher availability and local processing capabilities
   This pattern is suited for mission-critical deployments where leaf nodes need to operate autonomously even if disconnected from the hub.

6. **Automatic JetStream Replication** (`nats_auto_replication_test.go`): Provides effortless replication in a clustered setup:
   - Uses a proper NATS cluster with multiple servers
   - Automatic replication via the `Replicas` parameter in stream configuration
   - No custom replication code needed - NATS handles it internally
   - Built-in fault tolerance for high availability
   This pattern is ideal for high-throughput, mission-critical systems where data durability is essential.

7. **Dynamic Cluster Scaling** (`nats_dynamic_cluster_test.go`): Enables growing your infrastructure dynamically:
   - Start with a minimal NATS setup (single server)
   - Add new servers to the cluster as your system grows
   - Automatic data replication to new servers
   - Fault tolerance with continued operation even if original servers go down
   This pattern is ideal for systems that need to scale horizontally over time without downtime or data migration.

8. **Stream Sourcing and Mirroring** (`nats_stream_sourcing_test.go`): Leverages JetStream's built-in replication features:
   - Mirror streams create exact copies of other streams
   - Source streams can combine multiple streams into one
   - Automatic propagation of new messages to all replicas
   - Flexible topologies for complex data flows
   This pattern works well for scenarios requiring data aggregation, fan-out, or exact copies of streams across different contexts.

For Erigon's datastreaming needs, a combination of the hybrid approach and late-joining pattern is likely the most suitable because it provides:
- Persistence of blockchain data on the sequencer (hub)
- Real-time distribution of new blocks/transactions to RPC nodes (leaves)
- Recovery capabilities for RPC nodes that temporarily disconnect
- The ability for new RPC nodes to sync historical data from a specific block height or from genesis 

For larger deployments requiring high availability and horizontal scaling, the dynamic cluster approach with automatic replication offers the most robust solution, allowing the network to grow over time while maintaining data integrity and fault tolerance. 
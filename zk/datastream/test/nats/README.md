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

5. **Message Ordering**: NATS JetStream guarantees that messages are delivered in the same order they were published, which is essential for blockchain data synchronization.

6. **Persistence**: JetStream's persistence capabilities ensure that data can be recovered in case of node restarts or failures.

7. **Performance**: The tests show that NATS can handle high-throughput messaging with low latency, which is important for real-time blockchain data streaming. 
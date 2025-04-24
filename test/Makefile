DOCKER_COMPOSE := docker compose -f docker-compose.yml
DOCKER_STATELESS_EXECUTOR := xlayer-executor
DOCKER_STATELESS_EXECUTOR_2 := xlayer-executor-2
DOCKER_STATELESS_EXECUTOR_3 := xlayer-executor-3
DOCKER_SEQ_SENDER := xlayer-seqs
DOCKER_AGGREGATOR := xlayer-agg
DOCKER_AGGREGATOR_DB := xlayer-agg-db
DOCKER_BRIDGE_DB := xlayer-bridge-db
DOCKER_BRIDGE_REDIS := xlayer-bridge-redis
DOCKER_BRIDGE_ZOOKEEPER := kafka-zookeeper
DOCKER_BRIDGE_COIN_KAFKA := xlayer-bridge-coin-kafka
DOCKER_BRIDGE_SERVICE := xlayer-bridge-service
DOCKER_BRIDGE_UI := xlayer-bridge-ui
DOCKER_PROVER := xlayer-prover
DOCKER_APPROVE := xlayer-approve
DOCKER_SEQ := xlayer-seq
DOCKER_RPC := xlayer-rpc
DOCKER_L1_NETWORK := xlayer-mock-l1-network
DOCKER_DATA_AVAILABILITY := xlayer-da
DOCKER_DATA_AVAILABILITY_DB := xlayer-da-db
DOCKER_POOL_DB := xlayer-pool-db
DOCKER_POOL_MANAGER := xlayer-pool-manager
DOCKER_SIGNER := xlayer-signer
DOCKER_DS := xlayer-ds
DOCKER_PROMETHEUS := prometheus
DOCKER_GRAFANA := grafana
DOCKER_MAINNET_SEQ := xlayer-mainnet-seq
DOCKER_MAINNET_RPC := xlayer-mainnet-rpc

# Apollo
DOCKER_COMPOSE_APOLLO := docker compose -f docker-compose-apollo.yml
DOCKER_APOLLO_CONFIGSERVICE := apollo-configservice
DOCKER_APOLLO_ADMINSERVICE := apollo-adminservice
DOCKER_APOLLO_PORTAL := apollo-portal
DOCKER_APOLLO_MYSQL := mysql

RUN_DOCKER_STATELESS_EXECUTOR := $(DOCKER_COMPOSE) up -d $(DOCKER_STATELESS_EXECUTOR)
RUN_DOCKER_STATELESS_EXECUTOR_2 := $(DOCKER_COMPOSE) up -d $(DOCKER_STATELESS_EXECUTOR_2)
RUN_DOCKER_STATELESS_EXECUTOR_3 := $(DOCKER_COMPOSE) up -d $(DOCKER_STATELESS_EXECUTOR_3)
RUN_DOCKER_SEQ_SENDER := $(DOCKER_COMPOSE) up -d $(DOCKER_SEQ_SENDER)
RUN_DOCKER_AGGREGATOR := $(DOCKER_COMPOSE) up -d $(DOCKER_AGGREGATOR)
RUN_DOCKER_AGGREGATOR_DB := $(DOCKER_COMPOSE) up -d $(DOCKER_AGGREGATOR_DB)
RUN_DOCKER_BRIDGE_DB := $(DOCKER_COMPOSE) up -d $(DOCKER_BRIDGE_DB)
RUN_DOCKER_BRIDGE_REDIS := $(DOCKER_COMPOSE) up -d $(DOCKER_BRIDGE_REDIS)
RUN_DOCKER_BRIDGE_ZOOKEEPER := $(DOCKER_COMPOSE) up -d $(DOCKER_BRIDGE_ZOOKEEPER)
RUN_DOCKER_BRIDGE_COIN_KAFKA := $(DOCKER_COMPOSE) up -d $(DOCKER_BRIDGE_COIN_KAFKA)
RUN_DOCKER_BRIDGE_SERVICE := $(DOCKER_COMPOSE) up -d $(DOCKER_BRIDGE_SERVICE)
RUN_DOCKER_BRIDGE_UI := $(DOCKER_COMPOSE) up -d $(DOCKER_BRIDGE_UI)
RUN_DOCKER_PROVER := $(DOCKER_COMPOSE) up -d $(DOCKER_PROVER)
RUN_DOCKER_APPROVE := $(DOCKER_COMPOSE) up -d $(DOCKER_APPROVE)
RUN_DOCKER_SEQ := $(DOCKER_COMPOSE) up -d $(DOCKER_SEQ)
RUN_DOCKER_RPC := $(DOCKER_COMPOSE) up -d $(DOCKER_RPC)
RUN_DOCKER_MAINNET_SEQ := $(DOCKER_COMPOSE) up -d $(DOCKER_MAINNET_SEQ)
RUN_DOCKER_MAINNET_RPC := $(DOCKER_COMPOSE) up -d $(DOCKER_MAINNET_RPC)
RUN_DOCKER_L1_NETWORK := $(DOCKER_COMPOSE) up -d $(DOCKER_L1_NETWORK)
RUN_DOCKER_DATA_AVAILABILITY := $(DOCKER_COMPOSE) up -d $(DOCKER_DATA_AVAILABILITY)
RUN_DOCKER_DATA_AVAILABILITY_DB := $(DOCKER_COMPOSE) up -d $(DOCKER_DATA_AVAILABILITY_DB)
RUN_DOCKER_POOL_DB := $(DOCKER_COMPOSE) up -d $(DOCKER_POOL_DB)
RUN_DOCKER_POOL_MANAGER := $(DOCKER_COMPOSE) up -d $(DOCKER_POOL_MANAGER)
RUN_DOCKER_SIGNER := $(DOCKER_COMPOSE) up -d $(DOCKER_SIGNER)
RUN_DOCKER_DS := $(DOCKER_COMPOSE) up -d $(DOCKER_DS)
RUN_DOCKER_PROMETHEUS := $(DOCKER_COMPOSE) up -d $(DOCKER_PROMETHEUS)
RUN_DOCKER_GRAFANA := $(DOCKER_COMPOSE) up -d $(DOCKER_GRAFANA)

# Apollo
RUN_DOCKER_APOLLO_CONFIGSERVICE := $(DOCKER_COMPOSE_APOLLO) up -d $(DOCKER_APOLLO_CONFIGSERVICE)
RUN_DOCKER_APOLLO_ADMINSERVICE := $(DOCKER_COMPOSE_APOLLO) up -d $(DOCKER_APOLLO_ADMINSERVICE)
RUN_DOCKER_APOLLO_PORTAL := $(DOCKER_COMPOSE_APOLLO) up -d $(DOCKER_APOLLO_PORTAL)
RUN_DOCKER_APOLLO_MYSQL := $(DOCKER_COMPOSE_APOLLO) up -d $(DOCKER_APOLLO_MYSQL)
RUN_DOCKER_APOLLO_SEQ := $(DOCKER_COMPOSE_APOLLO) up -d $(DOCKER_SEQ)
RUN_DOCKER_APOLLO_RPC := $(DOCKER_COMPOSE_APOLLO) up -d $(DOCKER_RPC)


STOP_DOCKER_SEQ := $(DOCKER_COMPOSE) stop $(DOCKER_SEQ)
STOP_DOCKER_RPC := $(DOCKER_COMPOSE) stop $(DOCKER_RPC)
STOP := $(DOCKER_COMPOSE) down --remove-orphans; sleep 3; rm -rf data

.PHONY: build-docker
build-docker: ## Build cdk-erigoin for sequencer and rpc
	docker build -t cdk-erigon -f ../Dockerfile.local ../

.PHONY: run
run: build-docker ## Runs a full node
	# base services
	$(RUN_DOCKER_STATELESS_EXECUTOR)
	$(RUN_DOCKER_L1_NETWORK)
	$(RUN_DOCKER_DATA_AVAILABILITY_DB)
	$(RUN_DOCKER_POOL_DB)
	$(RUN_DOCKER_AGGREGATOR_DB)
	sleep 3
	$(RUN_DOCKER_DATA_AVAILABILITY)
	$(RUN_DOCKER_APPROVE)

	# app services
	sleep 3
	$(RUN_DOCKER_SEQ)
	$(RUN_DOCKER_PROVER)
	sleep 10
	$(RUN_DOCKER_DS)
	$(RUN_DOCKER_SIGNER)
	$(RUN_DOCKER_SEQ_SENDER)
	$(RUN_DOCKER_AGGREGATOR)
	$(RUN_DOCKER_POOL_MANAGER)
	$(RUN_DOCKER_RPC)

	# monitoring services
	$(RUN_DOCKER_PROMETHEUS)
	$(RUN_DOCKER_GRAFANA)

.PHONY: all
all: build-docker ## Runs a full node
	# base services
	$(RUN_DOCKER_STATELESS_EXECUTOR)
	$(RUN_DOCKER_L1_NETWORK)
	$(RUN_DOCKER_DATA_AVAILABILITY_DB)
	$(RUN_DOCKER_POOL_DB)
	$(RUN_DOCKER_AGGREGATOR_DB)
	$(RUN_DOCKER_BRIDGE_DB)
	$(RUN_DOCKER_BRIDGE_REDIS)
	$(RUN_DOCKER_BRIDGE_ZOOKEEPER)
	$(RUN_DOCKER_BRIDGE_COIN_KAFKA)
	sleep 3
	$(RUN_DOCKER_DATA_AVAILABILITY)
	$(RUN_DOCKER_APPROVE)

	# app services
	sleep 3
	$(RUN_DOCKER_SEQ)
	$(RUN_DOCKER_PROVER)
	sleep 10
	$(RUN_DOCKER_DS)
	$(RUN_DOCKER_SIGNER)
	$(RUN_DOCKER_SEQ_SENDER)
	$(RUN_DOCKER_AGGREGATOR)
	$(RUN_DOCKER_POOL_MANAGER)
	$(RUN_DOCKER_RPC)

	# bridge services
	sleep 30
	$(RUN_DOCKER_BRIDGE_SERVICE)
	sleep 3
	$(RUN_DOCKER_BRIDGE_UI)

	# monitoring services
	$(RUN_DOCKER_PROMETHEUS)
	$(RUN_DOCKER_GRAFANA)

.PHONY: stop
stop: ## Stops all services
	$(STOP)

.PHONY: min-run
min-run: build-docker ## Runs a minimal node
	$(RUN_DOCKER_L1_NETWORK)
	sleep 3
	$(RUN_DOCKER_SEQ)
	$(RUN_DOCKER_PROMETHEUS)
	$(RUN_DOCKER_GRAFANA)

.PHONY: test
test: test-e2e ## Runs all e2e tests
	echo "Tests passed"
	sleep 3

.PHONY: test-e2e
test-e2e: stop all ## Runs group xlayer feature e2e tests checking race conditions
	sleep 3
	docker ps -a
	trap '$(STOP)' EXIT; MallocNanoZone=0 go test -count=1 -failfast -race -v -p 1 -timeout 600s ../ci/e2e-1/...

.PHONY: min-apollo
min-apollo: build-docker
	$(RUN_DOCKER_APOLLO_MYSQL)
	sleep 10
	$(RUN_DOCKER_APOLLO_CONFIGSERVICE)
	$(RUN_DOCKER_APOLLO_ADMINSERVICE)
	sleep 10
	$(RUN_DOCKER_APOLLO_PORTAL)

	# app services
	$(RUN_DOCKER_L1_NETWORK)
	sleep 3
	$(RUN_DOCKER_APOLLO_SEQ)
	$(RUN_DOCKER_PROMETHEUS)
	$(RUN_DOCKER_GRAFANA)

.PHONY: apollo
apollo:  build-docker
	$(RUN_DOCKER_APOLLO_MYSQL)
	sleep 10
	$(RUN_DOCKER_APOLLO_CONFIGSERVICE)
	$(RUN_DOCKER_APOLLO_ADMINSERVICE)
	sleep 10
	$(RUN_DOCKER_APOLLO_PORTAL)

	# base services
	$(RUN_DOCKER_STATELESS_EXECUTOR)
	$(RUN_DOCKER_L1_NETWORK)
	$(RUN_DOCKER_DATA_AVAILABILITY_DB)
	$(RUN_DOCKER_POOL_DB)
	$(RUN_DOCKER_AGGREGATOR_DB)
	sleep 3
	$(RUN_DOCKER_DATA_AVAILABILITY)
	$(RUN_DOCKER_APPROVE)

	# app services
	sleep 3
	$(RUN_DOCKER_APOLLO_SEQ)
	$(RUN_DOCKER_PROVER)
	sleep 10
	$(RUN_DOCKER_DS)
	$(RUN_DOCKER_SIGNER)
	$(RUN_DOCKER_SEQ_SENDER)
	$(RUN_DOCKER_AGGREGATOR)
	$(RUN_DOCKER_POOL_MANAGER)
	$(RUN_DOCKER_APOLLO_RPC)

	# monitoring services
	$(RUN_DOCKER_PROMETHEUS)
	$(RUN_DOCKER_GRAFANA)
	

# Target to add data loss testing logic
.PHONY: test-data-loss
test-data-loss: data-loss-1 data-loss-2 data-loss-3 data-loss-4 data-loss-5 data-loss-6 data-loss-7 data-loss-8

# interrupt before doFinishBlockAndUpdateState (before RemoveMinedTransactions)
# txs are still in txpool, data is lost in chaindata and not written to datastream yet
data-loss-1:
	make stop
	make run
	sleep 10
	$(STOP_DOCKER_SEQ)
	echo "data-loss-1-step-1"
	go test -count=1 -failfast -race -v -p 1 -timeout 10s -run TestModifyCodeCase1 ./e2e/data_loss_test.go
	make build-docker
	$(RUN_DOCKER_SEQ)
	sleep 20
	echo "data-loss-1-step-2"
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestStressAndStopSeq ./e2e/data_loss_test.go
	docker wait $(DOCKER_SEQ) > /dev/null
	docker logs $(DOCKER_SEQ) > data/seq_1.log 2>&1

	git checkout -- ../zk/stages/stage_sequence_execute.go
	make build-docker
	$(RUN_DOCKER_SEQ)
	sleep 60
	echo "data-loss-1-step-3"
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestCheckVerify ./e2e/data_loss_test.go
	docker logs $(DOCKER_SEQ) > data/seq_2.log 2>&1
	echo "data-loss-1-step-4"
	make data-recovery

# interrupt before 1st CommitAndStart (before RemoveMinedTransactions)
# txs are still in txpool, data is lost in chaindata and not written to datastream yet
data-loss-2:
	make stop
	make run
	sleep 10
	$(STOP_DOCKER_SEQ)
	echo "data-loss-2-step-1"
	go test -count=1 -failfast -race -v -p 1 -timeout 10s -run TestModifyCodeCase2 ./e2e/data_loss_test.go
	make build-docker
	$(RUN_DOCKER_SEQ)
	sleep 20
	echo "data-loss-2-step-2"
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestStressAndStopSeq ./e2e/data_loss_test.go
	docker wait $(DOCKER_SEQ) > /dev/null
	docker logs $(DOCKER_SEQ) > data/seq_1.log 2>&1

	git checkout -- ../zk/stages/stage_sequence_execute.go
	make build-docker
	$(RUN_DOCKER_SEQ)
	sleep 60
	echo "data-loss-2-step-3"
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestCheckVerify ./e2e/data_loss_test.go
	docker logs $(DOCKER_SEQ) > data/seq_2.log 2>&1
	echo "data-loss-2-step-4"
	make data-recovery

# before 2nd CommitAndStart (after RemoveMinedTransactions and updateStreamAndCheckRollback)
# txs are removed from txpool, data is lost in chaindata
# assuming current block having been written to ds
data-loss-3:
	make stop
	make run
	sleep 10
	$(STOP_DOCKER_SEQ)
	echo "data-loss-3-step-1"
	go test -count=1 -failfast -race -v -p 1 -timeout 10s -run TestModifyCodeCase3 ./e2e/data_loss_test.go
	make build-docker
	$(RUN_DOCKER_SEQ)
	sleep 20
	echo "data-loss-3-step-2"
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestStressAndStopSeq ./e2e/data_loss_test.go
	docker wait $(DOCKER_SEQ) > /dev/null
	docker logs $(DOCKER_SEQ) > data/seq_1.log 2>&1

	git checkout -- ../zk/stages/stage_sequence_execute.go
	make build-docker
	$(RUN_DOCKER_SEQ)
	sleep 60
	echo "data-loss-3-step-3"
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestCheckVerify ./e2e/data_loss_test.go
	docker logs $(DOCKER_SEQ) > data/seq_2.log 2>&1
	echo "data-loss-3-step-4"
	make data-recovery

# before the last sdb.tx.Commit (after RemoveMinedTransactions and updateStreamAndCheckRollback)
# txs are removed from txpool, data is lost in chaindata
# assuming current block having been written to ds
data-loss-4:
	make stop
	make run
	sleep 10
	$(STOP_DOCKER_SEQ)
	echo "data-loss-4-step-1"
	go test -count=1 -failfast -race -v -p 1 -timeout 10s -run TestModifyCodeCase4 ./e2e/data_loss_test.go
	make build-docker
	$(RUN_DOCKER_SEQ)
	sleep 20
	echo "data-loss-4-step-2"
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestStressAndStopSeq ./e2e/data_loss_test.go
	docker wait $(DOCKER_SEQ) > /dev/null
	docker logs $(DOCKER_SEQ) > data/seq_1.log 2>&1

	git checkout -- ../zk/stages/stage_sequence_execute.go
	make build-docker
	$(RUN_DOCKER_SEQ)
	sleep 60
	echo "data-loss-4-step-3"
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestCheckVerify ./e2e/data_loss_test.go
	docker logs $(DOCKER_SEQ) > data/seq_2.log 2>&1
	echo "data-loss-4-step-4"
	make data-recovery

# between RemoveMinedTransactions and updateStreamAndCheckRollback
# txs are removed from txpool, data is not written to datastream yet
data-loss-5:
	make stop
	make run
	sleep 10
	$(STOP_DOCKER_SEQ)
	echo "data-loss-5-step-1"
	go test -count=1 -failfast -race -v -p 1 -timeout 10s -run TestModifyCodeCase5 ./e2e/data_loss_test.go
	make build-docker
	$(RUN_DOCKER_SEQ)
	sleep 20
	echo "data-loss-5-step-2"
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestStressAndStopSeq ./e2e/data_loss_test.go
	docker wait $(DOCKER_SEQ) > /dev/null
	docker logs $(DOCKER_SEQ) > data/seq_1.log 2>&1

	git checkout -- ../zk/stages/stage_sequence_execute.go
	make build-docker
	$(RUN_DOCKER_SEQ)
	sleep 60
	echo "data-loss-5-step-3"
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestCheckVerify ./e2e/data_loss_test.go
	docker logs $(DOCKER_SEQ) > data/seq_2.log 2>&1
	echo "data-loss-5-step-4"
	make data-recovery

# between doFinishBlockAndUpdateState and SetSmtCache
# stage.Execution is updated, blockCache hasn't been set, data is not written to datastream yet
data-loss-8:
	make stop
	make run
	sleep 10
	$(STOP_DOCKER_SEQ)
	echo "data-loss-8-step-1"
	go test -count=1 -failfast -race -v -p 1 -timeout 10s -run TestModifyCodeCase8 ./e2e/data_loss_test.go
	make build-docker
	$(RUN_DOCKER_SEQ)
	sleep 20
	echo "data-loss-8-step-2"
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestStressAndStopSeq ./e2e/data_loss_test.go
	docker wait $(DOCKER_SEQ) > /dev/null
	docker logs $(DOCKER_SEQ) > data/seq_1.log 2>&1

	git checkout -- ../zk/stages/stage_sequence_execute.go
	make build-docker
	$(RUN_DOCKER_SEQ)
	sleep 60
	echo "data-loss-8-step-3"
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestCheckVerify ./e2e/data_loss_test.go
	docker logs $(DOCKER_SEQ) > data/seq_2.log 2>&1
	echo "data-loss-8-step-4"
	make data-recovery

# comment handleShutdown, disable the graceful shutdown
# shutdown manually
# align chaindata to smt
data-loss-6:
	make stop
	make run
	sleep 10
	$(STOP_DOCKER_SEQ)
	echo "data-loss-6-step-1"
	go test -count=1 -failfast -race -v -p 1 -timeout 10s -run TestModifyCodeCase6 ./e2e/data_loss_test.go
	make build-docker
	$(RUN_DOCKER_SEQ)
	sleep 20
	echo "data-loss-6-step-2"
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestStressAndStopSeq ./e2e/data_loss_test.go
	sleep 3
	$(STOP_DOCKER_SEQ)
	docker wait $(DOCKER_SEQ) > /dev/null
	docker logs $(DOCKER_SEQ) > data/seq_1.log 2>&1

	git checkout -- ../turbo/stages/stageloop.go
	make build-docker
	$(RUN_DOCKER_SEQ)
	sleep 60
	echo "data-loss-6-step-3"
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestCheckVerify ./e2e/data_loss_test.go
	docker logs $(DOCKER_SEQ) > data/seq_2.log 2>&1
	echo "data-loss-6-step-4"
	make data-recovery

# comment smt alignment, disable the auto recovery
# shutdown manually
# recover from graceful exit
data-loss-7:
	make stop
	make run
	sleep 10
	$(STOP_DOCKER_SEQ)
	echo "data-loss-7-step-1"
	go test -count=1 -failfast -race -v -p 1 -timeout 10s -run TestModifyCodeCase7 ./e2e/data_loss_test.go
	make build-docker
	$(RUN_DOCKER_SEQ)
	sleep 20
	echo "data-loss-7-step-2"
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestStressAndStopSeq ./e2e/data_loss_test.go
	sleep 3
	$(STOP_DOCKER_SEQ)
	docker wait $(DOCKER_SEQ) > /dev/null
	docker logs $(DOCKER_SEQ) > data/seq_1.log 2>&1

	git checkout -- ../zk/stages/stage_sequence_execute.go
	make build-docker
	$(RUN_DOCKER_SEQ)
	sleep 60
	echo "data-loss-7-step-3"
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestCheckVerify ./e2e/data_loss_test.go
	docker logs $(DOCKER_SEQ) > data/seq_2.log 2>&1
	echo "data-loss-7-step-4"
	make data-recovery

# replace seq data from rpc
data-recovery:
	$(STOP_DOCKER_SEQ)
	sleep 10
	$(STOP_DOCKER_RPC)
	sleep 10
	rm -rf data/seq/*; cp -r data/rpc/* data/seq/
	$(RUN_DOCKER_SEQ)
	sleep 10
	$(RUN_DOCKER_RPC)
	sleep 3
	go test -count=1 -failfast -race -v -p 1 -timeout 300s -run TestCheckVerify ./e2e/data_loss_test.go


.PHONY: test-executor
test-executor: stop run ## Runs group 2 to check if the true executor is working
	sleep 30 # wait for the executor to be ready
	docker ps -a
	trap '$(STOP)' EXIT; MallocNanoZone=0 go test -count=1 -failfast -race -v -p 1 -timeout 600s ../ci/e2e-1/... -run "^TestEthTransfer$$"

mainnet: build-docker
	# base services
	$(RUN_DOCKER_MAINNET_SEQ)
	# monitoring services
	$(RUN_DOCKER_PROMETHEUS)
	$(RUN_DOCKER_GRAFANA)

mainnet-executor: 
	chmod +x ./scripts/mainnet-executor.sh
	./scripts/mainnet-executor.sh 
	$(RUN_DOCKER_STATELESS_EXECUTOR)
	$(RUN_DOCKER_STATELESS_EXECUTOR_2)
	$(RUN_DOCKER_STATELESS_EXECUTOR_3)
	make mainnet

mainnet-rpc: build-docker
	$(RUN_DOCKER_MAINNET_RPC)

vmtouch:
	docker exec -it xlayer-seq sh -c "vmtouch -t /home/erigon/data/chaindata/mdbx.dat"
	docker exec -it xlayer-seq sh -c "vmtouch -t /home/erigon/data/smt/mdbx.dat"

vmtouch-evict:
	docker exec -it xlayer-seq sh -c "vmtouch -e /home/erigon/data/chaindata/mdbx.dat"

vmtouch-check:
	docker exec -it xlayer-seq sh -c "vmtouch /home/erigon/data/chaindata/mdbx.dat"
	docker exec -it xlayer-seq sh -c "vmtouch /home/erigon/data/smt/mdbx.dat"

.PHONY: seq
seq: build-docker ## Runs a full node
	# base services
	$(RUN_DOCKER_SEQ)
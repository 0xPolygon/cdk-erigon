DOCKER_COMPOSE := docker compose -f docker-compose.yml
DOCKER_BRIDGE_DB := xlayer-bridge-db
DOCKER_BRIDGE_SERVICE := xlayer-bridge-service
DOCKER_BRIDGE_UI := xlayer-bridge-ui
DOCKER_APPROVE := xlayer-approve
DOCKER_SEQ := xlayer-seq
DOCKER_RPC := xlayer-rpc
DOCKER_RPC_2 := xlayer-rpc-2
DOCKER_L1_NETWORK := xlayer-mock-l1-network
DOCKER_POOL_DB := xlayer-pool-db
DOCKER_POOL_MANAGER := xlayer-pool-manager
DOCKER_DS := xlayer-ds
DOCKER_PROMETHEUS := prometheus
DOCKER_GRAFANA := grafana
DOCKER_MAINNET_SEQ := xlayer-mainnet-seq
DOCKER_MAINNET_RPC := xlayer-mainnet-rpc
DOCKER_KAFKA := xlayer-kafka

# Apollo
DOCKER_COMPOSE_APOLLO := docker compose -f docker-compose-apollo.yml
DOCKER_APOLLO_CONFIGSERVICE := apollo-configservice
DOCKER_APOLLO_ADMINSERVICE := apollo-adminservice
DOCKER_APOLLO_PORTAL := apollo-portal
DOCKER_APOLLO_MYSQL := mysql

RUN_DOCKER_BRIDGE_DB := $(DOCKER_COMPOSE) up -d $(DOCKER_BRIDGE_DB)
RUN_DOCKER_BRIDGE_SERVICE := $(DOCKER_COMPOSE) up -d $(DOCKER_BRIDGE_SERVICE)
RUN_DOCKER_BRIDGE_UI := $(DOCKER_COMPOSE) up -d $(DOCKER_BRIDGE_UI)
RUN_DOCKER_APPROVE := $(DOCKER_COMPOSE) up -d $(DOCKER_APPROVE)
RUN_DOCKER_SEQ := $(DOCKER_COMPOSE) up -d $(DOCKER_SEQ)
RUN_DOCKER_RPC := $(DOCKER_COMPOSE) up -d $(DOCKER_RPC)
RUN_DOCKER_RPC_2 := $(DOCKER_COMPOSE) up -d $(DOCKER_RPC_2)
RUN_DOCKER_MAINNET_SEQ := $(DOCKER_COMPOSE) up -d $(DOCKER_MAINNET_SEQ)
RUN_DOCKER_MAINNET_RPC := $(DOCKER_COMPOSE) up -d $(DOCKER_MAINNET_RPC)
RUN_DOCKER_L1_NETWORK := $(DOCKER_COMPOSE) up -d $(DOCKER_L1_NETWORK)
RUN_DOCKER_POOL_DB := $(DOCKER_COMPOSE) up -d $(DOCKER_POOL_DB)
RUN_DOCKER_POOL_MANAGER := $(DOCKER_COMPOSE) up -d $(DOCKER_POOL_MANAGER)
RUN_DOCKER_DS := $(DOCKER_COMPOSE) up -d $(DOCKER_DS)
RUN_DOCKER_PROMETHEUS := $(DOCKER_COMPOSE) up -d $(DOCKER_PROMETHEUS)
RUN_DOCKER_GRAFANA := $(DOCKER_COMPOSE) up -d $(DOCKER_GRAFANA)
RUN_DOCKER_KAFKA := $(DOCKER_COMPOSE) up -d $(DOCKER_KAFKA)

# Apollo
RUN_DOCKER_APOLLO_CONFIGSERVICE := $(DOCKER_COMPOSE_APOLLO) up -d $(DOCKER_APOLLO_CONFIGSERVICE)
RUN_DOCKER_APOLLO_ADMINSERVICE := $(DOCKER_COMPOSE_APOLLO) up -d $(DOCKER_APOLLO_ADMINSERVICE)
RUN_DOCKER_APOLLO_PORTAL := $(DOCKER_COMPOSE_APOLLO) up -d $(DOCKER_APOLLO_PORTAL)
RUN_DOCKER_APOLLO_MYSQL := $(DOCKER_COMPOSE_APOLLO) up -d $(DOCKER_APOLLO_MYSQL)


STOP_DOCKER_SEQ := $(DOCKER_COMPOSE) stop $(DOCKER_SEQ)
STOP_DOCKER_RPC := $(DOCKER_COMPOSE) stop $(DOCKER_RPC)
STOP := $(DOCKER_COMPOSE) down --remove-orphans; sleep 3; rm -rf data/*;

INIT_BRIDGE_ACCOUNT := cast send -f 0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534  --private-key 0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2 --value 0.01ether 0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266 --legacy --rpc-url http://127.0.0.1:8123 --async
INIT_POLYGON_ZKEVM_BRIDGE_ACCOUNT := cast send -f 0x8943545177806ed17b9f23f0a21ee5948ecaa776  --private-key 0xbcdf20249abf0ed6d944c0288fad489e33f66b3960d9e6229c1cd214ed3bbe31 --value 0.01ether 0x3b59731225d8567ad89343a4669509fa91ba37ee --legacy --rpc-url http://127.0.0.1:8127 --async


.PHONY: build-docker
build-docker: ## Build cdk-erigoin for sequencer and rpc
	docker build -t cdk-erigon -f ../Dockerfile.local ../

.PHONY: run
run: stop build-docker
	$(DOCKER_COMPOSE) up -d xlayer-mock-l1-network
	$(DOCKER_COMPOSE) up -d xlayer-agg-db
	$(DOCKER_COMPOSE) up -d xlayer-bridge-db
	$(DOCKER_COMPOSE) up -d xlayer-pool-db
	$(DOCKER_COMPOSE) up -d xlayer-agglayer-prover
	$(DOCKER_COMPOSE) up -d xlayer-agglayer
	$(DOCKER_COMPOSE) up -d xlayer-bridge-redis
	$(DOCKER_COMPOSE) up -d kafka-zookeeper
	$(DOCKER_COMPOSE) up -d xlayer-bridge-coin-kafka

	sleep 3
	$(DOCKER_COMPOSE) up -d xlayer-approve
	$(DOCKER_COMPOSE) up -d xlayer-seq
	$(DOCKER_COMPOSE) up -d xlayer-pool-manager

	sleep 5
	RT_ENABLED=0 $(DOCKER_COMPOSE) up -d xlayer-rpc

	sleep 10
	$(DOCKER_COMPOSE) up -d xlayer-bridge-service
	$(DOCKER_COMPOSE) up -d xlayer-bridge-ui
	$(INIT_BRIDGE_ACCOUNT)
	
	mkdir -p data/aggkit
	chmod -R 777 data 
	$(DOCKER_COMPOSE) up -d xlayer-aggkit

.PHONY: run
realtime-run: stop build-docker
	$(DOCKER_COMPOSE) up -d xlayer-mock-l1-network
	$(DOCKER_COMPOSE) up -d xlayer-agg-db
	$(DOCKER_COMPOSE) up -d xlayer-bridge-db
	$(DOCKER_COMPOSE) up -d xlayer-pool-db
	$(DOCKER_COMPOSE) up -d xlayer-agglayer-prover
	$(DOCKER_COMPOSE) up -d xlayer-agglayer
	$(DOCKER_COMPOSE) up -d xlayer-bridge-redis
	$(DOCKER_COMPOSE) up -d kafka-zookeeper
	$(DOCKER_COMPOSE) up -d xlayer-bridge-coin-kafka
	$(RUN_DOCKER_KAFKA)

	sleep 15
	$(DOCKER_COMPOSE) up -d xlayer-approve
	$(DOCKER_COMPOSE) up -d xlayer-seq
	$(DOCKER_COMPOSE) up -d xlayer-pool-manager

	sleep 5
	RT_ENABLED=1 $(DOCKER_COMPOSE) up -d xlayer-rpc
	$(DOCKER_COMPOSE) up -d xlayer-rpc-2

	sleep 10
	$(DOCKER_COMPOSE) up -d xlayer-bridge-service
	$(DOCKER_COMPOSE) up -d xlayer-bridge-ui
	$(INIT_BRIDGE_ACCOUNT)
	
	mkdir -p data/aggkit
	chmod -R 777 data 
	$(DOCKER_COMPOSE) up -d xlayer-aggkit

.PHONY: run
multi-run: run
	bash initL2-2.sh

	$(DOCKER_COMPOSE) up -d polygonzkevm-agg-db
	$(DOCKER_COMPOSE) up -d polygonzkevm-bridge-db
	$(DOCKER_COMPOSE) up -d polygonzkevm-seq

	sleep 5
	$(DOCKER_COMPOSE) up -d polygonzkevm-rpc

	sleep 10
	$(DOCKER_COMPOSE) up -d polygonzkevm-bridge-service
	$(DOCKER_COMPOSE) up -d polygonzkevm-bridge-ui

	sleep 10
	${INIT_POLYGON_ZKEVM_BRIDGE_ACCOUNT}
	$(DOCKER_COMPOSE) up -d polygonzkevm-cdk-node

.PHONY: all
all: run ## Runs a full node

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

.PHONY: test # Run all tests on Linux, but some are not supported on Mac
test: 
	@echo "Starting tests at $$(date)"
	@ERROR_COUNT=0; \
	FAILED_TESTS=""; \
	/bin/bash -c ' \
		run_test() { \
			echo "\n========== Running $$1... =========="; \
			if make $$1; then \
				echo "✅ $$1 passed"; \
			else \
				echo "❌ $$1 FAILED"; \
				ERROR_COUNT=$$((ERROR_COUNT+1)); \
				FAILED_TESTS="$$FAILED_TESTS\n  - $$1"; \
			fi; \
		}; \
		export ERROR_COUNT=0; \
		export FAILED_TESTS=""; \
		run_test test-e2e; \
		run_test test-data-loss; \
		run_test tests; \
		run_test lint; \
		run_test check_chinese_characters; \
		run_test test-unwind-default; \
		run_test test-unwind-split-db; \
		run_test kurtosis-cdk; \
		run_test resequence-default; \
		run_test resequence-ac-split; \
		run_test test-realtime-e2e; \
		echo "\n========== Tests completed at $$(date) =========="; \
		if [ $$ERROR_COUNT -gt 0 ]; then \
			echo "❌ $$ERROR_COUNT tests failed:"; \
			echo -e "$$FAILED_TESTS"; \
			exit 1; \
		else \
			echo "✅ All tests passed successfully!"; \
		fi \
	'

.PHONY: test-unwind-default
test-unwind-default:
	cd ../ && make test-unwind-default
	echo "Unwind tests passed"

.PHONY: test-unwind-split-db
test-unwind-split-db:
	cd ../ && make test-unwind-split-db
	kill 
	echo "Unwind tests passed"

.PHONY: check_chinese_characters
check_chinese_characters:
	cd ../ && bash .github/scripts/check_chinese_characters.sh
	echo "Chinese characters check passed"

resequence-default:
	cd ../ && bash .github/scripts/make-init-cdk.sh default && bash ./.github/scripts/test_resequence.sh default
	echo "Default resequence tests passed"

resequence-ac-split:
	cd ../ && bash .github/scripts/make-init-cdk.sh ac-split && bash ./.github/scripts/test_resequence.sh ac-split
	echo "AC split resequence tests passed"

.PHONY: kurtosis-cdk
kurtosis-cdk:
	cd ../ && bash .github/scripts/make-init-cdk.sh ac-split && bash ./.github/scripts/make-cdk.sh
	echo "Kurtosis cdk tests passed"

.PHONY: tests
tests:
	mkdir -p $(CURDIR)/app/tmp
	cd ../ && TMPDIR=$(CURDIR)/app/tmp make -B test
	echo "Unit tests passed"

.PHONY: lint
lint:
	cd ../docs/endpoints && make check-doc
	echo "Lint passed"

.PHONY: test-e2e
test-e2e: stop all ## Runs group xlayer feature e2e tests checking race conditions
	sleep 3
	docker ps -a
	trap '$(STOP)' EXIT; MallocNanoZone=0 go test -count=1 -failfast -race -v -p 1 -timeout 600s ../ci/e2e-1/...
	echo "E2E tests passed"

.PHONY: test-realtime-e2e
test-realtime-e2e: stop realtime-run
	sleep 3
	docker ps -a
	trap '$(STOP)' EXIT; MallocNanoZone=0 go test -count=1 -failfast -race -v -p 1 -timeout 1200s ../ci/e2e-1/... ../zk/realtime/test/...
	echo "Realtime E2E tests passed"

.PHONY: test-realtime-e2e-10
test-realtime-e2e-10: ## Runs test-realtime-e2e 10 times, stops immediately if any run fails
	@echo "Starting 10 iterations of test-realtime-e2e at $$(date)"
	@for i in $$(seq 1 10); do \
		echo "\n========== Running iteration $$i/10... =========="; \
		if make test-realtime-e2e; then \
			echo "✅ Iteration $$i passed"; \
		else \
			echo "❌ Iteration $$i FAILED - stopping immediately"; \
			exit 1; \
		fi; \
	done; \
	echo "\n========== All 10 iterations completed successfully at $$(date) =========="

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
	$(DOCKER_COMPOSE_APOLLO) up -d xlayer-seq
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

	$(DOCKER_COMPOSE) up -d xlayer-mock-l1-network
	$(DOCKER_COMPOSE) up -d xlayer-agg-db
	$(DOCKER_COMPOSE) up -d xlayer-bridge-db
	$(DOCKER_COMPOSE) up -d xlayer-pool-db
	$(DOCKER_COMPOSE) up -d xlayer-agglayer-prover
	$(DOCKER_COMPOSE) up -d xlayer-agglayer
	$(DOCKER_COMPOSE) up -d xlayer-bridge-redis
	$(DOCKER_COMPOSE) up -d kafka-zookeeper
	$(DOCKER_COMPOSE) up -d xlayer-bridge-coin-kafka
	$(RUN_DOCKER_KAFKA)

	sleep 15
	$(DOCKER_COMPOSE) up -d xlayer-approve
	$(DOCKER_COMPOSE_APOLLO) up -d xlayer-seq
	$(DOCKER_COMPOSE) up -d xlayer-pool-manager

	sleep 5
	RT_ENABLED=0 $(DOCKER_COMPOSE_APOLLO) up -d xlayer-rpc
	$(DOCKER_COMPOSE) up -d xlayer-rpc-2

	sleep 10
	$(DOCKER_COMPOSE) up -d xlayer-bridge-service
	$(DOCKER_COMPOSE) up -d xlayer-bridge-ui
	$(INIT_BRIDGE_ACCOUNT)
	
	mkdir -p data/aggkit
	chmod -R 777 data 
	$(DOCKER_COMPOSE) up -d xlayer-aggkit

.PHONY: test-data-stream-unwind
test-data-stream-unwind:
	make stop
	make run
	sleep 30
	$(STOP_DOCKER_SEQ)
	echo "test-data-stream-unwind-step-1"
	go test -count=1 -failfast -race -v -p 1 -timeout 10s -run TestAddFlag ./e2e/data_stream_unwind_test.go
	$(RUN_DOCKER_SEQ)
	sleep 10
	echo "test-data-stream-unwind-step-2"
	docker logs xlayer-seq 2>&1 | grep -q "Unwinding data stream to block number.*blockNumber=5" && echo "✔ Found unwind message for block 5" || echo "✘ Missing unwind message for block 5"
	docker logs xlayer-seq 2>&1 | grep -q "\[stream-catchup\] Getting progress.*previousProgress=4" && echo "✔ Found stream-catchup progress message" || echo "✘ Missing stream-catchup progress message"
	

# Target to add data loss testing logic
.PHONY: test-data-loss
test-data-loss: data-loss-1 data-loss-2 data-loss-3 data-loss-4 data-loss-5 data-loss-6 data-loss-7 data-loss-8
	echo "Data loss tests passed"

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

	git checkout -- ../turbo/stages/stageloop_xlayer.go
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

	git checkout -- ../zk/stages/stage_sequence_execute_xlayer.go
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


mainnet: build-docker
	# base services
	$(RUN_DOCKER_MAINNET_SEQ)
	# monitoring services
	$(RUN_DOCKER_PROMETHEUS)
	$(RUN_DOCKER_GRAFANA)

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

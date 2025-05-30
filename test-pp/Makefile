
DOCKER_COMPOSE := docker compose -f docker-compose.yml

STOP := $(DOCKER_COMPOSE) down --remove-orphans; sleep 3; rm -rf data

INIT_BRIDGE_ACCOUNT := cast send -f 0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534  --private-key 0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2 --value 0.01ether 0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266 --legacy --rpc-url http://127.0.0.1:8123
INIT_POLYGON_ZKEVM_BRIDGE_ACCOUNT := cast send -f 0x8943545177806ed17b9f23f0a21ee5948ecaa776  --private-key 0xbcdf20249abf0ed6d944c0288fad489e33f66b3960d9e6229c1cd214ed3bbe31 --value 0.01ether 0x3b59731225d8567ad89343a4669509fa91ba37ee --legacy --rpc-url http://127.0.0.1:8127

.PHONY: run
run: stop build-docker
	$(DOCKER_COMPOSE) up -d xlayer-mock-l1-network
	$(DOCKER_COMPOSE) up -d xlayer-agg-db
	$(DOCKER_COMPOSE) up -d xlayer-bridge-db
	$(DOCKER_COMPOSE) up -d xlayer-pool-db
	$(DOCKER_COMPOSE) up -d xlayer-agglayer-prover
	$(DOCKER_COMPOSE) up -d xlayer-agglayer

	sleep 3
	$(DOCKER_COMPOSE) up -d xlayer-approve
	$(DOCKER_COMPOSE) up -d xlayer-seq
	$(DOCKER_COMPOSE) up -d xlayer-pool-manager

	sleep 5
	$(DOCKER_COMPOSE) up -d xlayer-rpc

	sleep 10
	$(DOCKER_COMPOSE) up -d xlayer-bridge-service
	$(DOCKER_COMPOSE) up -d xlayer-bridge-ui
	$(INIT_BRIDGE_ACCOUNT)
	
	$(DOCKER_COMPOSE) up -d xlayer-cdk-node

.PHONY: multi-run
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
	
.PHONY: stop
stop: ## Stops all services
	$(STOP)

.PHONY: build-docker
build-docker: ## Build cdk-erigoin for sequencer and rpc
	docker build -t cdk-erigon -f ../Dockerfile.local ../

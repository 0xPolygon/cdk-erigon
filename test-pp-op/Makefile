
DOCKER_COMPOSE := docker compose -f docker-compose.yml

STOP := $(DOCKER_COMPOSE) down --remove-orphans; \
	sleep 3; \
	rm -rf data; \
	rm -Rf ./l1-geth/consensus/beacondata; \
	rm -Rf ./l1-geth/consensus/validatordata; \
	rm -Rf ./l1-geth/consensus/genesis.ssz; \
	rm -Rf ./l1-geth/execution/geth; \
    rm -Rf ./config-op/artifact.json; \
	rm -Rf ./config-op/31337-deploy.json; \
    rm -Rf ./config-op/genesis.json; \
	rm -Rf ./config-op/rollup.json; \
	rm -Rf ./config-op/state_dump.json; \
	rm -Rf ./l1-geth/execution/genesis.json; \
	rm -Rf ./config-op/genesis-op-raw.json; \
	rm -Rf ./config-op/intent.toml; \
	rm -Rf ./config-op/state.json; \
	rm -Rf ./config-op/superchain.json; \
	rm -Rf ./config-op/implementations.json;
	
DEPLOY_ERC20_L1 := cd contracts && forge create OKBToken.sol:StandardERC20 --private-key 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 --rpc-url http://127.0.0.1:8545 --legacy --broadcast --constructor-args "OKBToken" "OKB" 1000000000
INIT_ACCOUNT_1 := cast send -f 0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266  --private-key 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 --value 1000ether 0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534 --legacy --rpc-url http://127.0.0.1:8545
INIT_ACCOUNT_2 := cast send -f 0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266  --private-key 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 --value 1000ether 0x70997970c51812dc3a010c7d01b50e0d17dc79c8 --legacy --rpc-url http://127.0.0.1:8545
INIT_ACCOUNT_3 := cast send -f 0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266  --private-key 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 --value 1000ether 0x14dC79964da2C08b23698B3D3cc7Ca32193d9955 --legacy --rpc-url http://127.0.0.1:8545
INIT_ACCOUNT_4 := cast send -f 0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266  --private-key 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 --value 1000ether 0x7d18a1b858253b5588f61fb5739d52e4b84e2cda --legacy --rpc-url http://127.0.0.1:8545

INIT_BRIDGE_ACCOUNT := cast send -f 0x8f8E2d6cF621f30e9a11309D6A56A876281Fd534  --private-key 0x815405dddb0e2a99b12af775fd2929e526704e1d1aea6a0b4e74dc33e2f7fcd2 --value 0.01ether 0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266 --legacy --rpc-url http://127.0.0.1:8123
INIT_POLYGON_ZKEVM_BRIDGE_ACCOUNT := cast send -f 0x8943545177806ed17b9f23f0a21ee5948ecaa776  --private-key 0xbcdf20249abf0ed6d944c0288fad489e33f66b3960d9e6229c1cd214ed3bbe31 --value 0.01ether 0x3b59731225d8567ad89343a4669509fa91ba37ee --legacy --rpc-url http://127.0.0.1:8127

.PHONY: run
run: stop build-docker
	@echo "🚀 Starting L1 PoS environment automatic dependency management..."
	$(DOCKER_COMPOSE) up -d l1-validator
	@echo "🎉 L1 PoS environment started!"
	sleep 30
	$(DEPLOY_ERC20_L1)
	$(INIT_ACCOUNT_1)
	$(INIT_ACCOUNT_2)
	$(INIT_ACCOUNT_3)
	$(INIT_ACCOUNT_4)
	sleep 5
	./init.sh
	sleep 5
	$(DOCKER_COMPOSE) up -d xlayer-bridge-db
	$(DOCKER_COMPOSE) up -d xlayer-pool-db
	sleep 5
	$(DOCKER_COMPOSE) up -d xlayer-agglayer-prover
	$(DOCKER_COMPOSE) up -d xlayer-agglayer
	sleep 5

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
	
	$(DOCKER_COMPOSE) up -d xlayer-agg-sender

.PHONY: mainnet
mainnet: build-docker
	@echo "🚀 Starting XLayer Fake Mainnet ..."
	$(DOCKER_COMPOSE) up -d l1-validator
	sleep 20
	$(INIT_ACCOUNT_1)
	$(INIT_ACCOUNT_2)
	$(INIT_ACCOUNT_3)
	$(INIT_ACCOUNT_4)
	$(DOCKER_COMPOSE) up -d xlayer-mainnet-seq

.PHONY: stop
stop: ## Stops all services
	$(STOP)

.PHONY: build-docker
build-docker: ## Build cdk-erigoin for sequencer and rpc
	docker build -t cdk-erigon -f ../Dockerfile.local ../

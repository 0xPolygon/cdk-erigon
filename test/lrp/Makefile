# global & shell env variables
USER_PREFIX = $(shell awk '/^user:/ {print $$2}' lrp.config.yaml 2>/dev/null)
GIT_ROOT = $(shell awk '/^gitRootPath:/ {print $$2}' lrp.config.yaml 2>/dev/null)
ifeq ($(GIT_ROOT),)
GIT_ROOT = $(shell git rev-parse --show-toplevel)
endif
IMAGE_TAG = $(USER_PREFIX)-$(shell cd $(GIT_ROOT) && git rev-parse --short HEAD)
export USER_PREFIX IMAGE_TAG
PORT_DIFF = $(shell awk '/^portDiff:/ {print $$2}' lrp.config.yaml 2>/dev/null | grep -E '^[0-9]+$$|^-[0-9]+$$' || echo 0)
PPROF_PORT = $(shell echo $$((6060 + $(PORT_DIFF))))
RPC_PORT = $(shell echo $$((8545 + $(PORT_DIFF))))
DS_PORT = $(shell echo $$((6900 + $(PORT_DIFF))))
METRICS_PORT = $(shell echo $$((9095 + $(PORT_DIFF))))
EXTERNAL_DS_PORT = $(shell echo $$((16900 + $(PORT_DIFF))))
export PPROF_PORT RPC_PORT DS_PORT METRICS_PORT EXTERNAL_DS_PORT

DOCKER_COMPOSE := docker compose -f docker-compose.yml -p $(USER_PREFIX)-xlayer-lrp
DOCKER_MAINNET_UNWIND := xlayer-mainnet-unwind
DOCKER_MAINNET_UNWIND_WITH_STANDALONE_SMT_DB := xlayer-mainnet-unwind-with-standalone-smt-db
DOCKER_MAINNET_UNWIND_CONTAINER := $(USER_PREFIX)-xlayer-mainnet-unwind
DOCKER_MAINNET_REPLAY := xlayer-mainnet-replay
DOCKER_MAINNET_REPLAY_WITH_EXTERNAL_DATASTREAM := xlayer-mainnet-replay-with-external-datastream
DOCKER_MAINNET_REPLAY_WITH_VMTOUCH := xlayer-mainnet-replay-with-vmtouch
DOCKER_MAINNET_REPLAY_WITH_EXTERNAL_DATASTREAM_AND_VMTOUCH := xlayer-mainnet-replay-with-external-datastream-and-vmtouch
DOCKER_MAINNET_REPLAY_CONTAINER := $(USER_PREFIX)-xlayer-mainnet-replay
DOCKER_MAINNET_UNWIND_SEQ := xlayer-mainnet-unwind-seq
DOCKER_MAINNET_UNWIND_SEQ_WITH_STANDALONE_SMT_DB := xlayer-mainnet-unwind-seq-with-standalone-smt-db
DOCKER_MAINNET_UNWIND_SEQ_CONTAINER := $(USER_PREFIX)-xlayer-mainnet-unwind-seq
DOCKER_MAINNET_L1_SYNC := xlayer-mainnet-l1-sync
DOCKER_MAINNET_L1_SYNC_CONTAINER := $(USER_PREFIX)-xlayer-mainnet-l1-sync

RUN_DOCKER_STATELESS_EXECUTOR := $(DOCKER_COMPOSE) up -d $(DOCKER_STATELESS_EXECUTOR)
RUN_DOCKER_MAINNET_UNWIND := $(DOCKER_COMPOSE) --profile legacy-db up -d $(DOCKER_MAINNET_UNWIND)
RUN_DOCKER_MAINNET_UNWIND_WITH_STANDALONE_SMT_DB := $(DOCKER_COMPOSE) --profile split-db up -d $(DOCKER_MAINNET_UNWIND_WITH_STANDALONE_SMT_DB)
RUN_DOCKER_MAINNET_REPLAY := $(DOCKER_COMPOSE) --profile internal-ds up -d $(DOCKER_MAINNET_REPLAY)
RUN_DOCKER_MAINNET_REPLAY_WITH_EXTERNAL_DATASTREAM := $(DOCKER_COMPOSE) --profile external-ds up -d $(DOCKER_MAINNET_REPLAY_WITH_EXTERNAL_DATASTREAM)
RUN_DOCKER_MAINNET_REPLAY_WITH_VMTOUCH := $(DOCKER_COMPOSE) --profile internal-ds-vmtouch up -d $(DOCKER_MAINNET_REPLAY_WITH_VMTOUCH)
RUN_DOCKER_MAINNET_REPLAY_WITH_EXTERNAL_DATASTREAM_AND_VMTOUCH := $(DOCKER_COMPOSE) --profile external-ds-vmtouch up -d $(DOCKER_MAINNET_REPLAY_WITH_EXTERNAL_DATASTREAM_AND_VMTOUCH)
RUN_DOCKER_MAINNET_UNWIND_SEQ := $(DOCKER_COMPOSE) --profile legacy-db up -d $(DOCKER_MAINNET_UNWIND_SEQ)
RUN_DOCKER_MAINNET_UNWIND_SEQ_WITH_STANDALONE_SMT_DB := $(DOCKER_COMPOSE) --profile split-db up -d $(DOCKER_MAINNET_UNWIND_SEQ_WITH_STANDALONE_SMT_DB)
RUN_DOCKER_MAINNET_L1_SYNC := $(DOCKER_COMPOSE) up -d $(DOCKER_MAINNET_L1_SYNC)

UNWIND_LOG_FILE := unwind.log
REPLAY_LOG_FILE := replay.log
REPLAY_TPS_FILE := tps.log

.PHONY: lrp-build-docker
lrp-build-docker: # Build cdk-erigoin for lrp
	@if [ -d $(GIT_ROOT) ]; then \
		cd $(GIT_ROOT); \
		docker build -t cdk-erigon:$(IMAGE_TAG) -f Dockerfile.lrp . ; \
	else \
		echo "Error: git root <$(GIT_ROOT)> does not exist"; \
		exit 1; \
	fi

.PHONY: lrp-config
lrp-config:
	@while true; do \
		read -p "Enter fromBatchNumber: " FROM_BATCH_NO; \
		if [ "$$FROM_BATCH_NO" -ge 134466 ]; then \
			break; \
		else \
			echo "Error: fromBatchNumber must be greater than or equal to 134466"; \
		fi; \
	done; \
	while true; do \
		read -p "Enter toBatchNumber: " TO_BATCH_NO; \
		if [ "$$TO_BATCH_NO" = "0" ] || [ "$$TO_BATCH_NO" -ge "$$FROM_BATCH_NO" ]; then \
			break; \
		else \
			echo "Error: toBatchNumber must be greater than or equal to fromBatchNumber ($$FROM_BATCH_NO)"; \
		fi; \
	done; \
	read -p "Use external datastream? (y/n): " USE_EXTERNAL_DS; \
	if [ "$$USE_EXTERNAL_DS" = "y" ]; then \
		EXTERNAL_DS="true"; \
	else \
		EXTERNAL_DS="false"; \
	fi; \
	read -p "Enter user: " USER_NAME; \
	if [ -z "$$USER_NAME" ]; then \
		USER_NAME=$$(if [ -n "$$SUDO_USER" ]; then echo "$$SUDO_USER"; else echo "$$USER"; fi); \
	fi; \
	read -p "Enter portDiff: " PORT_DIFF; \
	UNWIND_BATCH_NO=$$((FROM_BATCH_NO - 1)); \
	GIT_COMMIT=$$(git rev-parse --short HEAD); \
	if [ ! -f lrp.config.yaml ]; then \
		echo "user: $$USER_NAME" > lrp.config.yaml; \
		echo "gitCommit: $$GIT_COMMIT" >> lrp.config.yaml; \
		echo "portDiff: $$PORT_DIFF" >> lrp.config.yaml; \
		echo "fromBatchNumber: $$FROM_BATCH_NO" >> lrp.config.yaml; \
		echo "toBatchNumber: $$TO_BATCH_NO" >> lrp.config.yaml; \
		echo "processCount: 1" >> lrp.config.yaml; \
		echo "useExternalDatastream: $$EXTERNAL_DS" >> lrp.config.yaml; \
		echo "srcMainnetDataPath: " >> lrp.config.yaml; \
		echo "externalDatastreamPath: " >> lrp.config.yaml; \
		echo "gitRootPath: " >> lrp.config.yaml; \
	else \
		if [ "$(shell uname)" = "Darwin" ]; then \
			sed -i '' "s/^fromBatchNumber:.*/fromBatchNumber: $$FROM_BATCH_NO/" lrp.config.yaml; \
			sed -i '' "s/^toBatchNumber:.*/toBatchNumber: $$TO_BATCH_NO/" lrp.config.yaml; \
			sed -i '' "s/^useExternalDatastream:.*/useExternalDatastream: $$EXTERNAL_DS/" lrp.config.yaml; \
			sed -i '' "s/^user:.*/user: $$USER_NAME/" lrp.config.yaml; \
			sed -i '' "s/^portDiff:.*/portDiff: $$PORT_DIFF/" lrp.config.yaml; \
			sed -i '' "s/^gitCommit:.*/gitCommit: $$GIT_COMMIT/" lrp.config.yaml; \
			if ! grep -q "^fromBatchNumber:" lrp.config.yaml; then echo "fromBatchNumber: $$FROM_BATCH_NO" >> lrp.config.yaml; fi; \
			if ! grep -q "^toBatchNumber:" lrp.config.yaml; then echo "toBatchNumber: $$TO_BATCH_NO" >> lrp.config.yaml; fi; \
			if ! grep -q "^useExternalDatastream:" lrp.config.yaml; then echo "useExternalDatastream: $$EXTERNAL_DS" >> lrp.config.yaml; fi; \
			if ! grep -q "^user:" lrp.config.yaml; then echo "user: $$USER_NAME" >> lrp.config.yaml; fi; \
			if ! grep -q "^portDiff:" lrp.config.yaml; then echo "portDiff: $$PORT_DIFF" >> lrp.config.yaml; fi; \
			if ! grep -q "^gitCommit:" lrp.config.yaml; then echo "gitCommit: $$GIT_COMMIT" >> lrp.config.yaml; fi; \
		else \
			sed -i "s/^fromBatchNumber:.*/fromBatchNumber: $$FROM_BATCH_NO/" lrp.config.yaml; \
			sed -i "s/^toBatchNumber:.*/toBatchNumber: $$TO_BATCH_NO/" lrp.config.yaml; \
			sed -i "s/^useExternalDatastream:.*/useExternalDatastream: $$EXTERNAL_DS/" lrp.config.yaml; \
			sed -i "s/^user:.*/user: $$USER_NAME/" lrp.config.yaml; \
			sed -i "s/^portDiff:.*/portDiff: $$PORT_DIFF/" lrp.config.yaml; \
			sed -i "s/^gitCommit:.*/gitCommit: $$GIT_COMMIT/" lrp.config.yaml; \
			if ! grep -q "^fromBatchNumber:" lrp.config.yaml; then echo "fromBatchNumber: $$FROM_BATCH_NO" >> lrp.config.yaml; fi; \
			if ! grep -q "^toBatchNumber:" lrp.config.yaml; then echo "toBatchNumber: $$TO_BATCH_NO" >> lrp.config.yaml; fi; \
			if ! grep -q "^useExternalDatastream:" lrp.config.yaml; then echo "useExternalDatastream: $$EXTERNAL_DS" >> lrp.config.yaml; fi; \
			if ! grep -q "^user:" lrp.config.yaml; then echo "user: $$USER_NAME" >> lrp.config.yaml; fi; \
			if ! grep -q "^portDiff:" lrp.config.yaml; then echo "portDiff: $$PORT_DIFF" >> lrp.config.yaml; fi; \
			if ! grep -q "^gitCommit:" lrp.config.yaml; then echo "gitCommit: $$GIT_COMMIT" >> lrp.config.yaml; fi; \
		fi; \
	fi

.PHONY: lrp-config-apply
lrp-config-apply:
	@FROM_BATCH_NO=$$(awk '/^fromBatchNumber:/ {print $$2}' lrp.config.yaml); \
	UNWIND_BATCH_NO=$$((FROM_BATCH_NO - 1)); \
	TO_BATCH_NO=$$(awk '/^toBatchNumber:/ {print $$2}' lrp.config.yaml); \
	EXTERNAL_DS=$$(awk '/^useExternalDatastream:/ {print $$2}' lrp.config.yaml); \
	if [ "$$EXTERNAL_DS" = "true" ]; then \
		DS_PATH=$$(awk '/^externalDatastreamPath:/ {print $$2}' lrp.config.yaml); \
		if [ ! -z "$$DS_PATH" ]; then \
			EXPANDED_PATH=$$(echo "$$DS_PATH" | envsubst)/data-stream; \
		else \
			SRC_PATH=$$(awk '/^srcMainnetDataPath:/ {print $$2}' lrp.config.yaml); \
			if [ -z "$$SRC_PATH" ]; then \
				echo "Error: Neither externalDatastreamPath nor srcMainnetDataPath is set in lrp.config.yaml"; \
				exit 1; \
			fi; \
			EXPANDED_PATH=$$(echo "$$SRC_PATH" | envsubst)/data-stream; \
		fi; \
		if [ "$(shell uname)" = "Darwin" ]; then \
			sed -i '' "s|- .*:/home/data-stream.bin|- $$EXPANDED_PATH.bin:/home/data-stream.bin|" docker-compose.yml; \
			sed -i '' "s|- .*:/home/data-stream.db/|- $$EXPANDED_PATH.db:/home/data-stream.db/|" docker-compose.yml; \
		else \
			sed -i "s|- .*:/home/data-stream.bin|- $$EXPANDED_PATH.bin:/home/data-stream.bin|" docker-compose.yml; \
			sed -i "s|- .*:/home/data-stream.db/|- $$EXPANDED_PATH.db:/home/data-stream.db/|" docker-compose.yml; \
		fi; \
	fi; \
	if [ "$(shell uname)" = "Darwin" ]; then \
		sed -i '' "s/--unwind-batch-no=.*/--unwind-batch-no=$$UNWIND_BATCH_NO/" docker-compose.yml; \
		sed -i '' "s/--zkevm.sequencer-replay-halt-on-batch-number=[0-9]*/--zkevm.sequencer-replay-halt-on-batch-number=$$TO_BATCH_NO/" docker-compose.yml; \
		sed -i '' "s/--zkevm.sequencer-replay-external-datastream=\(true\|false\)/--zkevm.sequencer-replay-external-datastream=$$EXTERNAL_DS/" docker-compose.yml; \
	else \
		sed -i "s/--unwind-batch-no=.*/--unwind-batch-no=$$UNWIND_BATCH_NO/" docker-compose.yml; \
		sed -i "s/--zkevm.sequencer-replay-halt-on-batch-number=[0-9]*/--zkevm.sequencer-replay-halt-on-batch-number=$$TO_BATCH_NO/" docker-compose.yml; \
		sed -i "s/--zkevm.sequencer-replay-external-datastream=\(true\|false\)/--zkevm.sequencer-replay-external-datastream=$$EXTERNAL_DS/" docker-compose.yml; \
	fi

.PHONY: lrp-mainnet
lrp-mainnet: lrp-config-apply lrp-build-docker
	@if [ -f "./mainnet/seq/smt/mdbx.dat" ]; then \
		echo "Found standalone SMT database, using split-db mode..."; \
		$(RUN_DOCKER_MAINNET_UNWIND_WITH_STANDALONE_SMT_DB); \
	else \
		echo "No standalone SMT database found, using legacy-db mode..."; \
		$(RUN_DOCKER_MAINNET_UNWIND); \
	fi
	@docker wait $(DOCKER_MAINNET_UNWIND_CONTAINER) > /dev/null; \
	docker logs $(DOCKER_MAINNET_UNWIND_CONTAINER) > $(UNWIND_LOG_FILE) 2>&1
	@# $(RUN_DOCKER_STATELESS_EXECUTOR)
	@# sleep 3
	@EXTERNAL_DS=$$(awk '/^useExternalDatastream:/ {print $$2}' lrp.config.yaml); \
	if [ "$$EXTERNAL_DS" = "true" ]; then \
		$(RUN_DOCKER_MAINNET_REPLAY_WITH_EXTERNAL_DATASTREAM); \
	else \
		$(RUN_DOCKER_MAINNET_REPLAY); \
	fi

.PHONY: lrp-mainnet-unwind
lrp-mainnet-unwind: lrp-config-apply lrp-build-docker
	@if [ -f "./mainnet/seq/smt/mdbx.dat" ]; then \
		echo "Found standalone SMT database, using split-db mode..."; \
		$(RUN_DOCKER_MAINNET_UNWIND_WITH_STANDALONE_SMT_DB); \
	else \
		echo "No standalone SMT database found, using legacy-db mode..."; \
		$(RUN_DOCKER_MAINNET_UNWIND); \
	fi
	@docker wait $(DOCKER_MAINNET_UNWIND_CONTAINER) > /dev/null; \
	docker logs $(DOCKER_MAINNET_UNWIND_CONTAINER) > $(UNWIND_LOG_FILE) 2>&1

.PHONY: lrp-mainnet-replay
lrp-mainnet-replay: lrp-config-apply lrp-build-docker
	@# $(RUN_DOCKER_STATELESS_EXECUTOR)
	@# sleep 3
	@EXTERNAL_DS=$$(awk '/^useExternalDatastream:/ {print $$2}' lrp.config.yaml); \
	if [ "$$EXTERNAL_DS" = "true" ]; then \
		$(RUN_DOCKER_MAINNET_REPLAY_WITH_EXTERNAL_DATASTREAM); \
	else \
		$(RUN_DOCKER_MAINNET_REPLAY); \
	fi

.PHONY: lrp-mainnet-replay-pause
lrp-mainnet-replay-pause:
	@EXTERNAL_DS=$$(awk '/^useExternalDatastream:/ {print $$2}' lrp.config.yaml); \
	if [ "$$EXTERNAL_DS" = "true" ]; then \
		docker stop $(DOCKER_MAINNET_REPLAY_CONTAINER) --time 600; \
	else \
		echo "Warning: useExternalDatastream is set to false, batches data will be lost"; \
	fi

.PHONY: lrp-mainnet-unwind-seq
lrp-mainnet-unwind-seq: lrp-config-apply lrp-build-docker
	@if [ -f "./mainnet/seq/smt/mdbx.dat" ]; then \
		echo "Found standalone SMT database, using split-db mode..."; \
		$(RUN_DOCKER_MAINNET_UNWIND_SEQ_WITH_STANDALONE_SMT_DB); \
	else \
		echo "No standalone SMT database found, using legacy-db mode..."; \
		$(RUN_DOCKER_MAINNET_UNWIND_SEQ); \
	fi
	@docker wait $(DOCKER_MAINNET_UNWIND_SEQ_CONTAINER) > /dev/null; \
	docker logs $(DOCKER_MAINNET_UNWIND_SEQ_CONTAINER) > $(UNWIND_LOG_FILE) 2>&1

.PHONY: lrp-mainnet-data-refresh
lrp-mainnet-data-refresh:
	@SRC_PATH=$$(awk '/^srcMainnetDataPath:/ {print $$2}' lrp.config.yaml); \
	if [ -z "$$SRC_PATH" ]; then \
		echo "Error: srcMainnetDataPath not found in lrp.config.yaml"; \
		echo "Please add 'srcMainnetDataPath: /path/to/mainnet/data' to lrp.config.yaml"; \
		exit 1; \
	fi; \
	EXPANDED_PATH=$$(echo "$$SRC_PATH" | envsubst); \
	if [ ! -d "$$EXPANDED_PATH" ]; then \
		echo "Error: Source directory $$EXPANDED_PATH does not exist"; \
		exit 1; \
	fi; \
	mkdir -p mainnet; \
	rm -rf mainnet/*; \
	cp -r "$$EXPANDED_PATH" mainnet/seq

.PHONY: lrp-mainnet-data-compact
lrp-mainnet-data-compact:
	@if [ ! -f ./mainnet/seq/chaindata/mdbx.dat ]; then \
		echo "Error: Database file not found at ./mainnet/seq/chaindata/mdbx.dat"; \
		exit 1; \
	fi; \
	mkdir -p tmp; \
	echo "Compacting database file..."; \
	../../build/bin/mdbx_copy -c ./mainnet/seq/chaindata/mdbx.dat ./tmp/mdbx.dat; \
	if [ $$? -eq 0 ]; then \
		echo "Replacing original database file with compacted version..."; \
		mv ./mainnet/seq/chaindata/mdbx.dat ./mainnet/seq/chaindata/mdbx.dat.bak; \
		mv ./tmp/mdbx.dat ./mainnet/seq/chaindata/mdbx.dat; \
		rm -f ./mainnet/seq/chaindata/mdbx.dat.bak; \
		rm -rf tmp; \
		echo "Database compaction completed successfully."; \
	else \
		echo "Error: Database compaction failed"; \
		rm -rf tmp; \
		exit 1; \
	fi

.PHONY: lrp-mainnet-tps
lrp-mainnet-tps: # Filter logs for LRP & Compute TPS
	@docker logs $(DOCKER_MAINNET_REPLAY_CONTAINER) > $(REPLAY_LOG_FILE) 2>&1; \
	python process_log.py $(REPLAY_LOG_FILE) 2>&1 | tee $(REPLAY_TPS_FILE)

.PHONY: lrp-stop
lrp-stop:
	@docker ps -a | grep "$(USER_PREFIX)-xlayer" | cut -d' ' -f1 | xargs -r docker stop; \
	docker ps -a | grep "$(USER_PREFIX)-xlayer" | cut -d' ' -f1 | xargs -r docker rm -f; \
	docker network rm $(USER_PREFIX)-erigon-lrp 2>/dev/null || true

# ----------- vmtouch -----------

vmtouch:
	docker exec -it $(DOCKER_MAINNET_REPLAY_CONTAINER) sh -c "vmtouch -t /home/erigon/data/chaindata/mdbx.dat"

vmtouch-evict:
	docker exec -it $(DOCKER_MAINNET_REPLAY_CONTAINER) sh -c "vmtouch -e /home/erigon/data/chaindata/mdbx.dat"

vmtouch-check:
	docker exec -it $(DOCKER_MAINNET_REPLAY_CONTAINER) sh -c "vmtouch /home/erigon/data/chaindata/mdbx.dat"

.PHONY: lrp-mainnet-replay-vmtouch
lrp-mainnet-replay-vmtouch: lrp-config-apply lrp-build-docker
	@EXTERNAL_DS=$$(awk '/^useExternalDatastream:/ {print $$2}' lrp.config.yaml); \
	if [ "$$EXTERNAL_DS" = "true" ]; then \
		$(RUN_DOCKER_MAINNET_REPLAY_WITH_EXTERNAL_DATASTREAM_AND_VMTOUCH); \
	else \
		$(RUN_DOCKER_MAINNET_REPLAY_WITH_VMTOUCH); \
	fi

# ---------- multi-process ----------

.PHONY: lrp-mainnet-l1-sync
lrp-mainnet-l1-sync:
	@$(RUN_DOCKER_MAINNET_L1_SYNC)

.PHONY: lrp-mainnet-parallel
lrp-mainnet-parallel: lrp-config-apply-parallel lrp-build-docker lrp-mainnet-l1-sync
	@PROCESS_COUNT=$$(awk '/^processCount:/ {print $$2}' lrp.config.yaml); \
	export COMPOSE_IGNORE_ORPHANS=true; \
	for i in $$(seq $$PROCESS_COUNT -1 1); do \
        mkdir -p mainnet/seq$$i; \
        if [ $$i -eq $$PROCESS_COUNT ]; then \
            cp -r mainnet/seq/* mainnet/seq$$i/; \
        else \
            cp -r mainnet/seq$$((i+1))/* mainnet/seq$$i/; \
        fi; \
        DOCKER_COMPOSE="docker compose -f docker-compose-$$i.yml -p $(USER_PREFIX)-xlayer-lrp"; \
        $$DOCKER_COMPOSE up -d $(DOCKER_MAINNET_UNWIND)-$$i; \
        docker wait $(DOCKER_MAINNET_UNWIND_CONTAINER)-$$i > /dev/null; \
        docker logs $(DOCKER_MAINNET_UNWIND_CONTAINER)-$$i > unwind_$$i.log 2>&1; \
    done; \
	for i in $$(seq 1 $$PROCESS_COUNT); do \
		DOCKER_COMPOSE="docker compose -f docker-compose-$$i.yml -p $(USER_PREFIX)-xlayer-lrp"; \
		$$DOCKER_COMPOSE --profile internal-ds up -d $(DOCKER_MAINNET_REPLAY)-$$i; \
	done; \

.PHONY: lrp-config-apply-parallel
lrp-config-apply-parallel:
	@PROCESS_COUNT=$$(awk '/^processCount:/ {print $$2}' lrp.config.yaml); \
	if [ -z "$$PROCESS_COUNT" ] || [ "$$PROCESS_COUNT" -lt 1 ]; then \
		echo "Error: Process count must be greater than 0"; \
		exit 1; \
	elif [ "$$PROCESS_COUNT" -eq 1 ]; then \
		echo "Error: Process count is 1, please use 'make lrp-mainnet' for single process mode"; \
		exit 1; \
	elif [ "$$PROCESS_COUNT" -gt 10 ]; then \
		echo "Warning: Process count ($$PROCESS_COUNT) is larger than 10"; \
	fi; \
	EXTERNAL_DS=$$(awk '/^useExternalDatastream:/ {print $$2}' lrp.config.yaml); \
	if [ "$$EXTERNAL_DS" = "true" ]; then \
		echo "Error: Multi-process LRP does not support external datastream mode"; \
		exit 1; \
	fi; \
	FROM_BATCH_NO=$$(awk '/^fromBatchNumber:/ {print $$2}' lrp.config.yaml); \
	TO_BATCH_NO=$$(awk '/^toBatchNumber:/ {print $$2}' lrp.config.yaml); \
	BATCH_RANGE=$$((TO_BATCH_NO - FROM_BATCH_NO + 1)); \
	SEGMENT_SIZE=$$((BATCH_RANGE / PROCESS_COUNT)); \
	REMAINDER=$$((BATCH_RANGE % PROCESS_COUNT)); \
	CURRENT_BATCH=$$TO_BATCH_NO; \
	for i in $$(seq $$PROCESS_COUNT -1 1); do \
		END_BATCH=$$CURRENT_BATCH; \
		if [ $$i -eq $$PROCESS_COUNT ]; then \
			START_BATCH=$$((CURRENT_BATCH - SEGMENT_SIZE - REMAINDER + 1)); \
		else \
			START_BATCH=$$((CURRENT_BATCH - SEGMENT_SIZE + 1)); \
		fi; \
		UNWIND_BATCH=$$((START_BATCH - 1)); \
		cp -f docker-compose.yml docker-compose-$$i.yml; \
		if [ "$(shell uname)" = "Darwin" ]; then \
			sed -i '' "s/xlayer-mainnet-unwind/xlayer-mainnet-unwind-$$i/g" docker-compose-$$i.yml; \
			sed -i '' "s/xlayer-mainnet-replay/xlayer-mainnet-replay-$$i/g" docker-compose-$$i.yml; \
			sed -i '' "s/--unwind-batch-no=[0-9]*/--unwind-batch-no=$$UNWIND_BATCH/" docker-compose-$$i.yml; \
			sed -i '' "s|./mainnet/seq:|./mainnet/seq$$i:|" docker-compose-$$i.yml; \
			sed -i '' "s/--zkevm.sequencer-replay-halt-on-batch-number=[0-9]*/--zkevm.sequencer-replay-halt-on-batch-number=$$END_BATCH/" docker-compose-$$i.yml; \
			sed -i '' "s/\$${PPROF_PORT}/$$(($(PPROF_PORT) + $$i))/" docker-compose-$$i.yml; \
			sed -i '' "s/\$${RPC_PORT}/$$(($(RPC_PORT) + $$i))/" docker-compose-$$i.yml; \
			sed -i '' "s/\$${DS_PORT}/$$(($(DS_PORT) + $$i))/" docker-compose-$$i.yml; \
			sed -i '' "s/\$${METRICS_PORT}/$$(($(METRICS_PORT) + $$i))/" docker-compose-$$i.yml; \
		else \
			sed -i "s/xlayer-mainnet-unwind/xlayer-mainnet-unwind-$$i/g" docker-compose-$$i.yml; \
			sed -i "s/xlayer-mainnet-replay/xlayer-mainnet-replay-$$i/g" docker-compose-$$i.yml; \
			sed -i "s/--unwind-batch-no=[0-9]*/--unwind-batch-no=$$UNWIND_BATCH/" docker-compose-$$i.yml; \
			sed -i "s|./mainnet/seq:|./mainnet/seq$$i:|" docker-compose-$$i.yml; \
			sed -i "s/--zkevm.sequencer-replay-halt-on-batch-number=[0-9]*/--zkevm.sequencer-replay-halt-on-batch-number=$$END_BATCH/" docker-compose-$$i.yml; \
			sed -i "s/\$${PPROF_PORT}/$$(($(PPROF_PORT) + $$i))/" docker-compose-$$i.yml; \
			sed -i "s/\$${RPC_PORT}/$$(($(RPC_PORT) + $$i))/" docker-compose-$$i.yml; \
			sed -i "s/\$${DS_PORT}/$$(($(DS_PORT) + $$i))/" docker-compose-$$i.yml; \
			sed -i "s/\$${METRICS_PORT}/$$(($(METRICS_PORT) + $$i))/" docker-compose-$$i.yml; \
		fi; \
		CURRENT_BATCH=$$((START_BATCH - 1)); \
	done

.PHONY: lrp-stop-parallel
lrp-stop-parallel: lrp-stop
	@PROCESS_COUNT=$$(awk '/^processCount:/ {print $$2}' lrp.config.yaml); \
	for i in $$(seq 1 $$PROCESS_COUNT); do \
		rm -f docker-compose-$$i.yml; \
	done

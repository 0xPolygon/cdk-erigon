#!/bin/bash

# This script is used to run CI tasks in Docker containers

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

if [ $# -lt 1 ]; then
    echo "Usage: $0 <event>"
    echo "Valid events: pull_request, push, release."
    exit 1
fi
EVENT=$1
if [ "$EVENT" != "pull_request" ] && [ "$EVENT" != "push" ] && [ "$EVENT" != "release" ]; then
    echo "Error: Invalid event $EVENT. Valid events are: pull_request, push, release."
    exit 1
fi

# ** Parse workflow file
WORKFLOWFILE="./ci/workflows/workflows.yml"
if [ ! -f "$WORKFLOWFILE" ]; then
    echo "Error: $WORKFLOWFILE not found!"
    exit 1
fi
NT=$(yq '.tasks | length' $WORKFLOWFILE)
if [ $NT -eq 0 ]; then
    echo "Error: No tasks found in $WORKFLOWFILE!"
    exit 1
fi

declare -A tasks_base
declare -A tasks_compose
declare -A tasks_kurtosis

echo -e "Fetching tasks for event ${RED}$EVENT${NC} from ${GREEN}$WORKFLOWFILE${NC} ..."
FT=0
for ((i=0; i<$NT; i++)); do
    TASK_EVENTS=$(yq ".tasks[$i].events" $WORKFLOWFILE | tr -d ' \-' | tr '\n' ' ')
    if ! [[ "$TASK_EVENTS" =~ "$EVENT" ]]; then
        echo "Skipping task $i: $TASK_EVENTS"
        continue
    fi

    TASK_NAME=$(yq ".tasks[$i].name" $WORKFLOWFILE | tr -d '"')
    TASK_TYPE=$(yq ".tasks[$i].type" $WORKFLOWFILE | tr -d '"')
    TASK_CMD=$(yq ".tasks[$i].command" $WORKFLOWFILE | tr -d '"')

    if [ "$TASK_TYPE" == "base" ]; then
        tasks_base["$TASK_NAME"]="$TASK_CMD"
        FT=$((FT + 1))
    elif [ "$TASK_TYPE" == "compose" ]; then
        tasks_compose["$TASK_NAME"]="$TASK_CMD"
        FT=$((FT + 1))
    elif [ "$TASK_TYPE" == "kurtosis" ]; then
        tasks_kurtosis["$TASK_NAME"]="$TASK_CMD"
        FT=$((FT + 1))
    else
        echo "Error: Unknown task type $TASK_TYPE for task $TASK_NAME!"
        exit 1
    fi
done
echo "Fetched $FT tasks."

declare -A task_pid
declare -A task_status

# Logs folder
TSTAMP=$(date +%Y%m%d%H%M%S%N)
LOGSDIR="logs-ci-$TSTAMP"
mkdir -p $LOGSDIR

# Get git tag (commit hash)
GIT_ROOT=`git rev-parse --show-toplevel`
cd $GIT_ROOT
GIT_TAG=`git rev-parse HEAD`
echo -e "Running workflows for git commit: ${GREEN}$GIT_TAG${NC}"

# Push Docker images to local registry
cd $GIT_ROOT
echo "Pushing images to local Docker registry ..."
./ci/utils/docker-cache-push.sh > $LOGSDIR/docker-cache-push.log 2>&1
DOCKER_REGISTRY_IP_PORT=$(cat $LOGSDIR/docker-cache-push.log | grep "Docker registry IP" | cut -d ' ' -f 4)
echo "Docker registry IP: $DOCKER_REGISTRY_IP_PORT"

# Build base images and push to local registry
cd $GIT_ROOT
docker build -f Dockerfile.ci -t xlayer-erigon-ci:latest --build-arg GIT_TAG=$GIT_TAG .
CDK_IMAGE_TAG="cdk-erigon:local"
docker build -t $CDK_IMAGE_TAG --file Dockerfile .
docker tag $CDK_IMAGE_TAG localhost:5000/$CDK_IMAGE_TAG
docker push localhost:5000/$CDK_IMAGE_TAG
docker rmi localhost:5000/$CDK_IMAGE_TAG

# *** Run non-dind tasks
# Base Docker command
BASE_CMD="docker run xlayer-erigon-ci:latest"
for task in "${!tasks_base[@]}"; do
    echo "Running task: $task"
    CMD="${BASE_CMD} sh -c \"${tasks_base[$task]}\""
    echo "Command: $CMD"
    eval $CMD > $LOGSDIR/logs-$task.log 2>&1 &
    task_pid[$task]=$!
done

# *** Run DinD (Docker-in-Docker) Docker Compose tasks
# DinD Docker command
BASE_NAME="base-xlayer-erigon-ci"
docker run -d --name $BASE_NAME --privileged xlayer-erigon-ci:latest sh -c "./ci/utils/docker-setup-start.sh $DOCKER_REGISTRY_IP_PORT"
sleep 5
docker exec $BASE_NAME sh -c "cd ./ci/utils && ./docker-cache-pull.sh $DOCKER_REGISTRY_IP_PORT" > $LOGSDIR/docker-cache-pull.log 2>&1
for task in "${!tasks_compose[@]}"; do
    echo "Running task: $task"
    CMD="docker exec $BASE_NAME sh -c \"${tasks_compose[$task]}\""
    echo "Command: $CMD"
    eval $CMD > $LOGSDIR/logs-$task.log 2>&1
    if [ $? -ne 0 ]; then
        echo -e "${NC}Task $task ${RED}failed${NC}."
        task_status[$task]="failed"
    else
        echo -e "${NC}Task $task ${GREEN}succeeded${NC}."
        task_status[$task]="succeeded"
    fi
done

# *** Run DinD (Docker-in-Docker) Kurtosis tasks
# DinD Docker command
BASE_CMD="--privileged xlayer-erigon-ci:latest sh -c \"./.github/scripts/configure_kurtosis_cdk.sh && ./.github/scripts/setup_kurtosis_cdk.sh $DOCKER_REGISTRY_IP_PORT"
for task in "${!tasks_kurtosis[@]}"; do
    echo "Running task: $task"
    LOGSSUBDIR="$LOGSDIR/$task"
    mkdir -p $LOGSSUBDIR
    CMD="docker run -v ./$LOGSSUBDIR:/logs ${BASE_CMD} && ${tasks_kurtosis[$task]}\""
    echo "Command: $CMD"
    eval $CMD > $LOGSDIR/logs-$task.log 2>&1 &
    task_pid[$task]=$!
done

# Wait for all tasks to finish
for task in "${!task_pid[@]}"; do
    wait ${task_pid[$task]}
    if [ $? -ne 0 ]; then
        echo -e "${NC}Task $task ${RED}failed${NC}."
        task_status[$task]="failed"
    else
        echo -e "${NC}Task $task ${GREEN}succeeded${NC}."
        task_status[$task]="succeeded"
    fi
done

# Summary
echo ""
for task in "${!tasks_status[@]}"; do
    if [ "${task_status[$task]}" == "failed" ]; then
        echo -e "${NC}$task ${RED}failed${NC}"
    else
        echo -e "${NC}$task ${GREEN}succeeded${NC}"
    fi
done

echo "All tasks completed. Logs are in ${GREEN}$LOGSDIR${NC}."
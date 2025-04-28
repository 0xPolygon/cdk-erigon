#!/bin/bash

if [ $# -lt 1 ]; then
    echo "Usage: $0 <docker_registry_ip>"
    exit 1
fi
DOCKER_REGISTRY_IP_PORT=$1

cd /

# The code below is taken from: https://github.com/moby/moby/blob/v26.0.1/hack/dind#L59
# It is used to avoid the error: "docker: Error response from daemon: failed to create task for container: failed to create shim task: OCI runtime create failed: runc create failed: unable to start container process: unable to apply cgroup configuration: cannot enter cgroupv2 "/sys/fs/cgroup/docker" with domain controllers -- it is in threaded mode: unknown."
# cgroup v2: enable nesting
if [ -f /sys/fs/cgroup/cgroup.controllers ]; then
	# move the processes from the root group to the /init group,
	# otherwise writing subtree_control fails with EBUSY.
	# An error during moving non-existent process (i.e., "cat") is ignored.
	mkdir -p /sys/fs/cgroup/init
	xargs -rn1 < /sys/fs/cgroup/cgroup.procs > /sys/fs/cgroup/init/cgroup.procs || :
	# enable controllers
	sed -e 's/ / +/g' -e 's/^/+/' < /sys/fs/cgroup/cgroup.controllers \
		> /sys/fs/cgroup/cgroup.subtree_control
fi

# Start Docker daemon
echo "{ \"insecure-registries\":[\"$DOCKER_REGISTRY_IP_PORT\"] }" > /etc/docker/daemon.json
dockerd > /dockerd.log 2>&1
# dockerd > /dockerd.log 2>&1 &
# sleep 5
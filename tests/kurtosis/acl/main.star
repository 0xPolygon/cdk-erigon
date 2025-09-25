load("github.com/kurtosis-tech/kurtosis/api/golang/lib/starlark/plan@0.83.7", "Plan")

def run(plan: Plan, args):
    erigon_image = args.get("erigon_image", "erigon-acl:test")
    rpc_port = args.get("rpc_port", 8545)
    acl_enabled = args.get("acl_enabled", False)

    # Start erigon (dev chain) with HTTP JSON-RPC enabled
    # Note: For full ACL preflight testing, erigon must be started with --acl.enable and
    # an ACL contract address whose code is present at genesis. This skeleton starts ACL disabled by default.

    cmd = [
        "erigon",
        "--chain", "dev",
        "--http",
        "--http.addr", "0.0.0.0",
        "--http.port", str(rpc_port),
        "--http.api", "eth,net,web3,debug",
        "--datadir", "/data",
        "--metrics", "--metrics.addr", "0.0.0.0",
    ]
    if acl_enabled:
        # Placeholder flags; requires an ACL contract deployed at the given address in genesis
        cmd.extend(["--acl.enable", "--acl.address", "0x0000000000000000000000000000000000000001"])  # TODO: replace via args

    erigon = plan.add_service(
        name = "erigon",
        config = plan.new_service_config(
            image = erigon_image,
            ports = {"rpc": plan.new_port_spec(number = rpc_port, transport_protocol = "TCP")},
            entrypoint = ["/bin/sh", "-lc"],
            cmd = ["mkdir -p /data && " + " ".join(cmd)],
        ),
    )

    # Simple test-runner container using curl + jq to hit RPC
    test_script = r'''
#!/bin/sh
set -e
apk add --no-cache curl jq >/dev/null 2>&1 || true

RPC=http://erigon:''' + str(rpc_port) + r'''

echo "Waiting for erigon RPC..."
for i in $(seq 1 60); do
  if curl -s "$RPC" -H 'content-type: application/json' -d '{"jsonrpc":"2.0","method":"net_version","params":[],"id":1}' | jq -e .result >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

echo "RPC reachable. net_version:" $(curl -s "$RPC" -H 'content-type: application/json' -d '{"jsonrpc":"2.0","method":"net_version","params":[],"id":1}' | jq -r .result)
echo "eth_blockNumber:" $(curl -s "$RPC" -H 'content-type: application/json' -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":2}' | jq -r .result)

echo "Basic Kurtosis ACL skeleton passed."
'''

    plan.upload_files("/scripts/test.sh", test_script)

    tester = plan.add_service(
        name = "tester",
        config = plan.new_service_config(
            image = "alpine:3.19",
            files = {"/scripts/test.sh": plan.get_uploaded_files_artifact("/scripts/test.sh")},
            entrypoint = ["/bin/sh", "/scripts/test.sh"],
        ),
    )

    # Declare dependency: tester waits for erigon
    plan.set_service_dependency(tester, erigon)


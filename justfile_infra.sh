# Starts the local infra used in testing.
#
# This will start the host VM and create SSH tunnels to the guest VMs.
# The environment will be torn down automatically when this command is exited.

#!/usr/bin/env bash
set -euo pipefail

host_pid=""
give_tunnel_pid=""
take_tunnel_pid=""

cleanup() {
    echo "[infra] cleaning up..."
    if [ -n "${take_tunnel_pid-}" ]; then
        kill "$take_tunnel_pid" || true
        echo "[infra] killed 'take' tunnel."
    fi
    if [ -n "${give_tunnel_pid-}" ]; then
        kill "$give_tunnel_pid" || true
        echo "[infra] killed 'give' tunnel."
    fi
    if [ -n "${host_pid-}" ]; then
        kill "$host_pid" || true
        echo "[infra] killed host vm."
    fi
}

trap cleanup EXIT

echo "[infra] starting host vm..."
nix run .#dev &
host_pid=$!
echo "[infra] host vm started with pid: $host_pid"

while ! nc -z localhost 2222 >/dev/null 2>&1; do
    sleep 1
done
echo "[infra] host vm is ready."

echo "[infra] starting ssh tunnels..."

ssh -o StrictHostKeyChecking=no -L 9000:give:22 root@localhost -p 2222 -N &
give_tunnel_pid=$!
echo "[infra] tunnel to 'give' vm started with pid: $give_tunnel_pid"

while ! nc -z localhost 9000 >/dev/null 2>&1; do
    sleep 1
done
echo "[infra] tunnel to 'give' vm is ready."

ssh -o StrictHostKeyChecking=no -L 9100:take:22 root@localhost -p 2222 -N &
take_tunnel_pid=$!
echo "[infra] tunnel to 'take' vm started with pid: $take_tunnel_pid"

while ! nc -z localhost 9100 >/dev/null 2>&1; do
    sleep 1
done
echo "[infra] tunnel to 'take' vm is ready."

echo "[infra] infrastructure is ready. press ctrl+c to exit and tear down."

wait "$host_pid"

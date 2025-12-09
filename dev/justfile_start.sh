#!/usr/bin/env bash
# Starts the local development environment used in testing.
#
# This will start the host VM and create SSH tunnels to the guest VMs.
# The environment will be torn down automatically when this command is exited.

set -euo pipefail

host_pid=""
give_tunnel_pid=""
take_tunnel_pid=""

cleanup() {
    echo "[dev] cleaning up..."
    if [ -n "${take_tunnel_pid-}" ]; then
        kill "$take_tunnel_pid" || true
        echo "[dev] killed 'take' tunnel."
    fi
    if [ -n "${give_tunnel_pid-}" ]; then
        kill "$give_tunnel_pid" || true
        echo "[dev] killed 'give' tunnel."
    fi
    if [ -n "${host_pid-}" ]; then
        kill "$host_pid" || true
        echo "[dev] killed host vm."
    fi
}

trap cleanup EXIT

echo "[dev] starting host vm..."
nix run .#dev >/dev/null 2>&1 &
host_pid=$!
echo "[dev] host vm started with pid: $host_pid"

while ! nc -z localhost 2222; do
    sleep 1
done
echo "[dev] host vm is ready."

echo "[dev] starting ssh tunnels..."

ssh -o StrictHostKeyChecking=no -L 9000:give:22 root@localhost -p 2222 -N >/dev/null 2>&1 &
give_tunnel_pid=$!
echo "[dev] tunnel to 'give' vm started with pid: $give_tunnel_pid"

while ! nc -z localhost 9000; do
    sleep 1
done
echo "[dev] tunnel to 'give' vm is ready."

ssh -o StrictHostKeyChecking=no -L 9100:take:22 root@localhost -p 2222 -N >/dev/null 2>&1 &
take_tunnel_pid=$!
echo "[dev] tunnel to 'take' vm started with pid: $take_tunnel_pid"

while ! nc -z localhost 9100; do
    sleep 1
done
echo "[dev] tunnel to 'take' vm is ready."

echo "[dev] infrastructure is ready. press ctrl+c to exit and tear down."

wait "$host_pid"

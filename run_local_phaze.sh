#!/bin/bash
# Phaze Local Test Runner
# This starts a local Nexus server and then launches the Phaze client.

# 1. Kill any existing instances
pkill -f "nexus_server"
pkill -f "phaze-native"

echo "--- Starting Phaze Nexus (Local) ---"
cd nexus_server
go build -o phaze-nexus main.go
DB_PATH=./nexus.db PORT=8080 ./phaze-nexus &
SERVER_PID=$!

echo "Waiting for server to spin up..."
sleep 2

echo "--- Launching Phaze Client ---"
cd ../native_client
# Ensure dependencies are met
go build -o phaze-native .

echo "Ready! Starting client in Local mode..."
./phaze-native &

# Cleanup on exit
trap "kill $SERVER_PID" EXIT
wait

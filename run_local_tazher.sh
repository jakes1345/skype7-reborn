#!/bin/bash
# Tazher Local Test Runner
# This starts a local Nexus server and then launches the Tazher client.

# 1. Kill any existing instances
pkill -f "nexus_server"
pkill -f "tazher-native"

echo "--- Starting Tazher Nexus (Local) ---"
cd nexus_server
go build -o tazher-nexus main.go
DB_PATH=./nexus.db PORT=8080 ./tazher-nexus &
SERVER_PID=$!

echo "Waiting for server to spin up..."
sleep 2

echo "--- Launching Tazher Client ---"
cd ../native_client
# Ensure dependencies are met
go build -o tazher-native .

echo "Ready! Starting client in Local mode..."
./tazher-native &

# Cleanup on exit
trap "kill $SERVER_PID" EXIT
wait

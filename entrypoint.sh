#!/bin/sh
set -e

# Entrypoint script for Arcturus Docker container

echo "==> Starting Arcturus services"
echo "=================================="

# Start control-plane
echo "Starting control-plane..."
cd /app/control-plane
./control-plane > log/control-plane.log 2>&1 &
CONTROL_PLANE_PID=$!
echo "control-plane started with PID: $CONTROL_PLANE_PID"

# Start data-plane
echo "Starting data-plane..."
cd /app/data-plane
./data-plane > log/data-plane.log 2>&1 &
DATA_PLANE_PID=$!
echo "data-plane started with PID: $DATA_PLANE_PID"

# Start data-proxy
echo "Starting data-proxy..."
cd /app/data-proxy
./data-proxy > log/data-proxy.log 2>&1 &
DATA_PROXY_PID=$!
echo "data-proxy started with PID: $DATA_PROXY_PID"

# Wait for any process to exit
echo "\nServices started. Waiting for processes..."
wait -n $CONTROL_PLANE_PID $DATA_PLANE_PID $DATA_PROXY_PID

# If any process exits, kill all others
echo "\nOne of the services exited. Stopping all services..."
kill -TERM $CONTROL_PLANE_PID $DATA_PLANE_PID $DATA_PROXY_PID 2>/dev/null || true

# Wait for all processes to exit
wait $CONTROL_PLANE_PID $DATA_PLANE_PID $DATA_PROXY_PID 2>/dev/null || true

echo "All services stopped."

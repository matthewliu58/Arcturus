#!/usr/bin/env bash
set -e

# Script to build and start Arcturus services

echo "==> Arcturus Service Manager"
echo "=================================="

# ===== Configuration =====
CONTROL_PLANE_DIR="control-plane"
DATA_PLANE_DIR="data-plane"
DATA_PROXY_DIR="data-proxy"

CONTROL_PLANE_BIN="control-plane"
DATA_PLANE_BIN="data-plane"
DATA_PROXY_BIN="data-proxy"

# ===== Helper functions =====

echo_step() {
    echo "\n==> $1"
}

check_process() {
    local process_name=$1
    pgrep -f "$process_name" > /dev/null
    return $?
}

kill_process() {
    local process_name=$1
    local process_ids=$(pgrep -f "$process_name")
    if [ -n "$process_ids" ]; then
        echo "Killing $process_name processes: $process_ids"
        sudo kill -9 $process_ids 2>/dev/null || true
        sleep 2
    fi
}

build_service() {
    local dir=$1
    local bin_name=$2
    local service_name=$3
    
    echo_step "Building $service_name"
    cd "$dir"
    go build -o "$bin_name" .
    if [ $? -ne 0 ]; then
        echo "Error: Failed to build $service_name"
        exit 1
    fi
    echo "$service_name built successfully"
    cd ..
}

start_service() {
    local dir=$1
    local bin_name=$2
    local service_name=$3
    
    echo_step "Starting $service_name"
    cd "$dir"
    nohup ./"$bin_name" > "$service_name.log" 2>&1 &
    local pid=$!
    echo "$service_name started with PID: $pid"
    echo $pid > "$service_name.pid"
    cd ..
    sleep 2
}

check_service_status() {
    local service_name=$1
    local dir=$2
    
    if [ -f "$dir/$service_name.pid" ]; then
        local pid=$(cat "$dir/$service_name.pid")
        if ps -p $pid > /dev/null 2>&1; then
            echo "$service_name is running with PID: $pid"
            return 0
        else
            echo "$service_name PID file exists but process is not running"
            return 1
        fi
    else
        echo "$service_name PID file not found"
        return 1
    fi
}

# ===== Main script =====

# Check if we're in the correct directory
if [ ! -d "$CONTROL_PLANE_DIR" ] || [ ! -d "$DATA_PLANE_DIR" ] || [ ! -d "$DATA_PROXY_DIR" ]; then
    echo "Error: This script must be run from the Arcturus project root directory"
    exit 1
fi

# Kill existing processes
echo_step "Checking for existing processes"
kill_process "$CONTROL_PLANE_BIN"
kill_process "$DATA_PLANE_BIN"
kill_process "$DATA_PROXY_BIN"

# Build services
build_service "$CONTROL_PLANE_DIR" "$CONTROL_PLANE_BIN" "Control Plane"
build_service "$DATA_PLANE_DIR" "$DATA_PLANE_BIN" "Data Plane"
build_service "$DATA_PROXY_DIR" "$DATA_PROXY_BIN" "Data Proxy"

# Start services
start_service "$CONTROL_PLANE_DIR" "$CONTROL_PLANE_BIN" "control-plane"
start_service "$DATA_PLANE_DIR" "$DATA_PLANE_BIN" "data-plane"
start_service "$DATA_PROXY_DIR" "$DATA_PROXY_BIN" "data-proxy"

# Check service status
echo_step "Checking service status"
check_service_status "control-plane" "$CONTROL_PLANE_DIR"
check_service_status "data-plane" "$DATA_PLANE_DIR"
check_service_status "data-proxy" "$DATA_PROXY_DIR"

# ===== Summary =====
echo "\n=================================="
echo "Arcturus Service Manager Complete!"
echo "=================================="
echo "Services built and started:"
echo "1. Control Plane"
echo "2. Data Plane"
echo "3. Data Proxy"
echo "\nTo check service status, run: ./check-services.sh"
echo "To stop services, run: ./stop-services.sh"
echo "=================================="

#!/usr/bin/env bash
set -e

# Script to build and run Arcturus Docker container

echo "==> Arcturus Docker Builder"
echo "========================="

# ===== Configuration =====
IMAGE_NAME="arcturus"
CONTAINER_NAME="arcturus-container"

# ===== Helper functions =====
echo_step() {
    echo "\n==> $1"
}

# ===== Main script =====

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo "Error: Docker is not installed"
    exit 1
fi

# Stop and remove existing container if it exists
echo_step "Checking for existing container"
if docker ps -a | grep -q "$CONTAINER_NAME"; then
    echo "Stopping and removing existing container..."
    docker stop "$CONTAINER_NAME" > /dev/null 2>&1 || true
    docker rm "$CONTAINER_NAME" > /dev/null 2>&1 || true
fi

# Build Docker image
echo_step "Building Docker image"
docker build -t "$IMAGE_NAME" .

# Run Docker container
echo_step "Running Docker container"
docker run -d \
    --name "$CONTAINER_NAME" \
    -p 7081:7081 \
    -p 7082:7082 \
    -p 7083:7083 \
    -p 8000-9000:8000-9000 \
    -p 4433:4433 \
    --restart unless-stopped \
    "$IMAGE_NAME"

# Check container status
echo_step "Checking container status"
sleep 2
docker ps -f name="$CONTAINER_NAME"

# ===== Summary =====
echo "\n========================="
echo "Arcturus Docker Build Complete!"
echo "========================="
echo "Container name: $CONTAINER_NAME"
echo "Image name: $IMAGE_NAME"
echo "Ports mapped:"
echo "- 7081:7081 (Control Plane API)"
echo "- 7082:7082 (Data Plane API)"
echo "- 7083:7083 (Data Proxy API)"
echo "- 8000-9000:8000-9000 (User access ports)"
echo "- 4433:4433 (QUIC Tunnel)"
echo "\nManagement commands:"
echo "  docker logs $CONTAINER_NAME      # View container logs"
echo "  docker exec -it $CONTAINER_NAME sh  # Enter container"
echo "  docker stop $CONTAINER_NAME      # Stop container"
echo "  docker start $CONTAINER_NAME     # Start container"
echo "  docker rm $CONTAINER_NAME        # Remove container"
echo "========================="

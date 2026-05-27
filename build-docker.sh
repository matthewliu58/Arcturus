#!/usr/bin/env bash
set -e

# Script to build and run SkyAccel Docker container

echo "==> SkyAccel Docker Builder"
echo "========================="

# ===== Configuration =====
IMAGE_NAME="SkyAccel"
CONTAINER_NAME="SkyAccel-container"

# ===== Helper functions =====
echo_step() {
    echo "\n==> $1"
}

# ===== Main script =====

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo "Docker is not installed. Installing Docker..."
    
    # Detect OS type
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        OS=$ID
    else
        echo "Error: Cannot detect OS type"
        exit 1
    fi
    
    # Install Docker based on OS type
    if [ "$OS" = "ubuntu" ] || [ "$OS" = "debian" ]; then
        # Ubuntu/Debian installation
        echo "Installing Docker on Ubuntu/Debian..."
        sudo apt update && sudo apt upgrade -y
        sudo apt install -y apt-transport-https ca-certificates curl gnupg-agent software-properties-common
        curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -
        sudo add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable"
        sudo apt update && sudo apt install -y docker-ce docker-ce-cli containerd.io
        sudo systemctl start docker
        sudo systemctl enable docker
    elif [ "$OS" = "centos" ] || [ "$OS" = "rhel" ]; then
        # CentOS/RHEL installation
        echo "Installing Docker on CentOS/RHEL..."
        sudo yum install -y yum-utils device-mapper-persistent-data lvm2
        sudo yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
        sudo yum install -y docker-ce docker-ce-cli containerd.io
        sudo systemctl start docker
        sudo systemctl enable docker
    else
        echo "Error: Unsupported OS. Please install Docker manually."
        exit 1
    fi
    
    # Verify Docker installation
    if ! command -v docker &> /dev/null; then
        echo "Error: Docker installation failed"
        exit 1
    fi
    
    echo "Docker installed successfully!"
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
echo "SkyAccel Docker Build Complete!"
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

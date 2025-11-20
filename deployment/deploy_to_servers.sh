#!/bin/bash

# Multi-Server Deployment Script - Linux Version
# Purpose: Execute Arcturus deployment on multiple server types (Forwarding, Traefik, Scheduling)

set -e

# Server Configuration by Type
FORWARDING_SERVERS=(
     "80.240.28.138"
     "149.248.8.34"
     "67.219.99.230"
     "45.77.90.250"
     "144.202.36.22"
     "155.138.146.39"
     "45.32.24.199"
)

TRAEFIK_SERVERS=(
    "80.240.28.138"
    "149.248.8.34"
    "67.219.99.230"
)

SCHEDULING_SERVERS=(
    "139.84.236.251"
)

# Credentials
USERNAME="root"
PASSWORD="Arcturus@Test2024"
REPO_URL="https://github.com/matthewliu58/Arcturus.git"
WORK_DIR="/root"
REPO_NAME="Arcturus"

# Log File
LOG_FILE="deployment_$(date +%Y%m%d_%H%M%S).log"

# Color Output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1" | tee -a "$LOG_FILE"
}

error() {
    echo -e "${RED}[$(date +'%Y-%m-%d %H:%M:%S')] ERROR: $1${NC}" | tee -a "$LOG_FILE"
}

warning() {
    echo -e "${YELLOW}[$(date +'%Y-%m-%d %H:%M:%S')] WARNING: $1${NC}" | tee -a "$LOG_FILE"
}

info() {
    echo -e "${BLUE}[$(date +'%Y-%m-%d %H:%M:%S')] INFO: $1${NC}" | tee -a "$LOG_FILE"
}

# Execute Remote Commands
execute_remote_command() {
    local server=$1
    local commands=$2
    
    log "Connecting to server: $server"
    
    # Use sshpass and ssh to execute commands
    sshpass -p "$PASSWORD" ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$USERNAME@$server" << EOF
set -e
$commands
EOF
    
    if [ $? -eq 0 ]; then
        log "✓ Server $server deployment succeeded"
        return 0
    else
        error "✗ Server $server deployment failed"
        return 1
    fi
}

# Generate Forwarding Deploy Commands
generate_forwarding_commands() {
    cat << 'COMMANDS'
cd /root

# Delete existing Arcturus folder
if [ -d "Arcturus" ]; then
    echo "Deleting existing Arcturus folder..."
    rm -rf Arcturus
fi

# Clone Repository (quiet mode)
echo "Cloning Arcturus repository..."
git clone -q --progress https://github.com/matthewliu58/Arcturus.git 2>&1 | grep -E "Receiving objects:|done"

# Enter deployment directory
cd Arcturus/deployment

# Execute deployment script
echo "Starting Forwarding deployment script..."
sudo bash deploy_forwarding.sh

# Verify services
echo "Verifying services..."
sudo systemctl status arcturus-forwarding | grep running
COMMANDS
}

# Generate Traefik Deploy Commands
generate_traefik_commands() {
    cat << 'COMMANDS'
cd /root

# Delete existing Arcturus folder
if [ -d "Arcturus" ]; then
    echo "Deleting existing Arcturus folder..."
    rm -rf Arcturus
fi

# Clone Repository (quiet mode)
echo "Cloning Arcturus repository..."
git clone -q --progress https://github.com/matthewliu58/Arcturus.git 2>&1 | grep -E "Receiving objects:|done"

# Enter deployment directory
cd Arcturus/deployment

# Step 1: Deploy Traefik
echo "Step 1: Deploying Traefik..."
sudo bash deploy_traefik.sh

# Step 2: Deploy Forwarding (entry nodes need to run both)
echo "Step 2: Deploying Forwarding..."
sudo bash deploy_forwarding.sh

# Verify services
echo "Verifying services..."
sudo systemctl status traefik | grep running
sudo systemctl status traefik-client-probe | grep running
sudo systemctl status arcturus-forwarding | grep running
COMMANDS
}

# Generate Scheduling Deploy Commands
generate_scheduling_commands() {
    cat << 'COMMANDS'
cd /root

# Delete existing Arcturus folder
if [ -d "Arcturus" ]; then
    echo "Deleting existing Arcturus folder..."
    rm -rf Arcturus
fi

# Clone Repository (quiet mode)
echo "Cloning Arcturus repository..."
git clone -q --progress https://github.com/matthewliu58/Arcturus.git 2>&1 | grep -E "Receiving objects:|done"

# Enter deployment directory
cd Arcturus/deployment

# Deploy Scheduling
echo "Deploying Scheduling..."
sudo bash deploy_scheduling.sh

# Verify services
echo "Verifying services..."
sudo systemctl status arcturus-scheduling | grep running
COMMANDS
}

# Deploy servers by type
deploy_servers() {
    local type=$1
    shift
    local servers=("$@")
    
    local success_count=0
    local fail_count=0
    local failed_servers=()
    
    info "========================================="
    info "Deploying $type servers"
    info "Number of $type servers: ${#servers[@]}"
    info "========================================="
    
    for server in "${servers[@]}"; do
        log "========================================="
        
        case $type in
            "Forwarding")
                deploy_commands=$(generate_forwarding_commands)
                ;;
            "Traefik")
                deploy_commands=$(generate_traefik_commands)
                ;;
            "Scheduling")
                deploy_commands=$(generate_scheduling_commands)
                ;;
            *)
                error "Unknown server type: $type"
                continue
                ;;
        esac
        
        if execute_remote_command "$server" "$deploy_commands"; then
            ((success_count++))
        else
            ((fail_count++))
            failed_servers+=("$server")
        fi
        
        # Wait 1 second to avoid frequent connections
        sleep 1
    done
    
    # Output Summary for this type
    log "========================================="
    log "$type deployment completed!"
    log "Successful: $success_count"
    log "Failed: $fail_count"
    
    if [ $fail_count -gt 0 ]; then
        warning "Failed $type servers: ${failed_servers[*]}"
    fi
    
    log "========================================="
}

# Main Function
main() {
    log "========================================="
    log "Starting multi-server deployment"
    log "Total servers: $((${#FORWARDING_SERVERS[@]} + ${#TRAEFIK_SERVERS[@]} + ${#SCHEDULING_SERVERS[@]}))"
    log "  - Forwarding servers: ${#FORWARDING_SERVERS[@]}"
    log "  - Traefik servers: ${#TRAEFIK_SERVERS[@]}"
    log "  - Scheduling servers: ${#SCHEDULING_SERVERS[@]}"
    log "Log file: $LOG_FILE"
    log "========================================="
    
    # Check dependencies
    if ! command -v sshpass &> /dev/null; then
        error "sshpass command not found. Please install: sudo apt-get install sshpass"
        exit 1
    fi
    
    # Deploy each server type
    # Use || true to prevent set -e from stopping execution if one type fails
    deploy_servers "Scheduling" "${SCHEDULING_SERVERS[@]}" || true
    deploy_servers "Traefik" "${TRAEFIK_SERVERS[@]}" || true
    deploy_servers "Forwarding" "${FORWARDING_SERVERS[@]}" || true
    
    # Final Summary
    log "========================================="
    log "All deployments completed!"
    log "Log file saved to: $LOG_FILE"
    log "========================================="
}

# Run Main Function
main

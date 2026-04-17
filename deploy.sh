#!/bin/bash

CLUSTER_INFO_FILE="cluster-info.txt"
TARGET_PASSWORD="root@12345"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}=== Arcturus Deployment Script ===${NC}"

if [ ! -f "$CLUSTER_INFO_FILE" ]; then
    echo -e "${RED}Error: Cluster information file not found${NC}"
    exit 1
fi

read_cluster_info() {
    local node_name="$1"
    local key="$2"
    grep -A 15 "^${node_name}:" "$CLUSTER_INFO_FILE" | grep "${key}:" | awk '{print $2}'
}

get_all_master_private_ips() {
    grep -A 15 "role: master" "$CLUSTER_INFO_FILE" | grep "private_ip:" | awk '{print $2}'
}

deploy_to_target() {
    local node_name="$1"
    local public_ip=$(read_cluster_info "$node_name" "ip")
    local private_ip=$(read_cluster_info "$node_name" "private_ip")
    local provider=$(read_cluster_info "$node_name" "provider")
    local continent=$(read_cluster_info "$node_name" "continent")
    local country=$(read_cluster_info "$node_name" "country")
    local city=$(read_cluster_info "$node_name" "city")
    local role=$(read_cluster_info "$node_name" "role")
    local server_private_ip=$(read_cluster_info "$node_name" "server")

    echo -e "${GREEN}Deploying to $node_name | Public: $public_ip | Private: $private_ip${NC}"

    local temp_dir=$(mktemp -d)
    cp -r ./* "$temp_dir"/
    cp "$CLUSTER_INFO_FILE" "$temp_dir"/

    for comp in control-plane data-plane data-proxy; do
        local cfg="$temp_dir/$comp/config.yaml"
        sed -i "s|public: \"[^\"]*\"|public: \"$public_ip\"|" "$cfg"
        sed -i "s|private: \"[^\"]*\"|private: \"$private_ip\"|" "$cfg"
        sed -i "s|provider: \"[^\"]*\"|provider: \"$provider\"|" "$cfg"
        sed -i "s|continent: \"[^\"]*\"|continent: \"$continent\"|" "$cfg"
        sed -i "s|country: \"[^\"]*\"|country: \"$country\"|" "$cfg"
        sed -i "s|city: \"[^\"]*\"|city: \"$city\"|" "$cfg"
    done

    local cfg="$temp_dir/control-plane/config.yaml"
    local master_private_ips=($(get_all_master_private_ips))

    if [ "$role" = "master" ]; then
        sed -i "s|server_ip: \"[^\"]*\"|server_ip: \"$private_ip\"|" "$cfg"
        server_list="  - \"$private_ip\"\n"
        for ip in "${master_private_ips[@]}"; do
            if [ "$ip" != "$private_ip" ]; then
                server_list+="  - \"$ip\"\n"
            fi
        done
    else
        sed -i "s|server_ip: \"[^\"]*\"|server_ip: \"\"|" "$cfg"
        server_list="  - \"$server_private_ip\"\n"
        for ip in "${master_private_ips[@]}"; do
            if [ "$ip" != "$server_private_ip" ]; then
                server_list+="  - \"$ip\"\n"
            fi
        done
    fi

    sed -i "/^server_list:/,/^[^ ]/c\\server_list:\n$server_list" "$cfg"

    tar -czf "$temp_dir/arcturus.tar.gz" -C "$temp_dir" .
    sshpass -p "$TARGET_PASSWORD" scp "$temp_dir/arcturus.tar.gz" root@$public_ip:/root/
    sshpass -p "$TARGET_PASSWORD" ssh root@$public_ip "mkdir -p /root/arcturus && tar -xzf /root/arcturus.tar.gz -C /root/arcturus && cd /root/arcturus && bash setup-systemd.sh"

    rm -rf "$temp_dir"
    echo -e "${GREEN} $node_name deployed successfully!${NC}"
}

deploy_all() {
    local nodes=$(grep -E "^[a-zA-Z0-9_-]+:" "$CLUSTER_INFO_FILE" | cut -d':' -f1)
    for node in $nodes; do
        deploy_to_target "$node"
    done
}

show_help() {
    echo -e "${YELLOW}Usage:$NC"
    echo "  $0 --deploy-all"
    echo "  $0 --deploy <node-name>"
    echo "  $0 --help"
}

if [ $# -eq 0 ]; then
    show_help
    exit 0
fi

while [ $# -gt 0 ]; do
    case "$1" in
        --deploy-all) deploy_all; shift ;;
        --deploy) deploy_to_target "$2"; shift 2 ;;
        --help) show_help; exit 0 ;;
        *) echo -e "${RED}Unknown option$NC"; exit 1 ;;
    esac
done
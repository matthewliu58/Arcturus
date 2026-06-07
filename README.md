# SkyAccel

**Version**: v1.0.0  
**Status**: Production-ready  
**License**: MIT

Modern cloud services increasingly rely on latency-sensitive workloads (LSWs) characterized by bursty arrivals, CPU-intensive execution, and stringent latency requirements. Existing commercial cloud acceleration services (CCASs) are tightly coupled to vendor-specific infrastructures, resulting in limited deployment flexibility and poor portability across heterogeneous multi-cloud environments. 

We present SkyAccel, a cross-cloud traffic acceleration system for LSWs. We identify an inherent asymmetry in end-to-end traffic control. The edge domain focuses on execution stability under dynamic CPU contention, whereas the core domain focuses on routing optimization across heterogeneous inter-cloud paths. 

Motivated by this observation, SkyAccel adopts an asymmetry-aware control decomposition (ACD) that separates execution stability control at the edge from routing optimization in the core. 

SkyAccel dynamically steers requests and schedules execution across heterogeneous virtual machines (VMs) spanning multiple cloud providers without relying on vendor-specific networking infrastructure. 

Evaluation shows that SkyAccel reduces 90th-percentile latency by 24--60%, maintains stable performance on resource-constrained 2-core instances under workloads of up to 200K connections per minute, and lowers traffic delivery cost by 3×--30× compared to state-of-the-art CCASs.

SkyAccel is a high-performance network acceleration project focused on optimizing network transmission performance and stability. It adopts a layered architecture design, combining heuristic algorithms and Lyapunov optimization to implement network routing, providing users with faster and more reliable network experience.

## System Architecture

The SkyAccel project consists of three main components:

1. **Control Plane** (`control-plane/`) — Route computing, resource management, and network synchronization. Exposes REST API on port `7081`.
2. **Data Plane** (`data-plane/`) — Local data collection (CPU, latency), analysis, and reporting to etcd.
3. **Data Proxy** (`data-proxy/`) — Data forwarding, tunnel management, and user access. Handles incoming user TCP/UDP connections.

```
User Traffic ──► Data Proxy (:8081) ──► Edge Node ──► Origin Server
                      │                        │
                      ▼                        │
              Control Plane (:7081) ◄─────────┘
                      │              Query routing path
                      ▼
                 etcd Cluster ◄── Data Plane reports telemetry
```

**Architecture Flow Explanation:**

**User Request Path:**
1. User connects to Data Proxy on port 8081
2. Data Proxy queries Control Plane for optimal routing decisions
3. Control Plane returns the best Edge Node and path to Origin Server
4. Data Proxy forwards traffic through Edge Node to Origin Server

**Telemetry Data Flow:**
1. Data Plane nodes continuously probe network quality and CPU metrics
2. Telemetry data is reported to etcd cluster
3. Control Plane reads real-time data from etcd for routing optimization

## Core Features

- **Intelligent Routing Optimization**: Uses heuristic algorithms (carousel-greed) and Lyapunov optimization for segmented routing decisions
- **Real-time Network Monitoring**: Collects network status data and analyzes network quality in real-time
- **QUIC Tunnel**: Establishes secure and efficient transmission tunnels using the QUIC protocol
- **Packet Merging**: Optimizes data packet transmission and reduces network overhead
- **Multi-protocol Support**: Compatible with multiple network protocols including TCP, UDP, and all protocols based on them, providing acceleration for mainstream protocols like HTTP/HTTPS, FTP, SMTP, DNS, RTP, QUIC, and streaming protocols such as HLS, DASH, RTMP, and WebRTC
- **Automatic Fault Detection and Recovery**: Monitors network status in real-time and automatically switches to optimal paths

---

## Quick Start

### Prerequisites

- Go 1.21+
- Linux server (Ubuntu 20.04+ recommended)
- At least 2 nodes with public IPs

### 1. Define Your Cluster Topology

Edit `cluster-info.txt` to describe all nodes in your cluster:

```yaml
# cluster-info.txt

# Node 1 - Master (runs etcd + control-plane)
node1:
  ip: 202.182.96.100
  provider: Vultr
  continent: Asia
  country: JP
  city: Tokyo
  private_ip: 192.168.1.100
  role: master
  server: ""

# Node 2 - Master (runs etcd + control-plane, for HA)
node2:
  ip: 202.182.96.101
  provider: DigitalOcean
  continent: Asia
  country: SG
  city: Singapore
  private_ip: 192.168.1.101
  role: master
  server: ""

# Node 3 - Slave (data-plane + data-proxy only)
node3:
  ip: 202.182.96.102
  provider: AWS
  continent: Asia
  country: HK
  city: Hong Kong
  private_ip: 192.168.1.102
  role: slave
  server: 192.168.1.100   # <-- points to a master's private IP
```

| Field | Description |
|-------|-------------|
| `ip` | Public IP of the node |
| `provider` | Cloud provider (Vultr, AWS, GCP, etc.) |
| `continent` / `country` / `city` | Geographic location for routing |
| `private_ip` | Internal IP for etcd cluster communication |
| `role` | `master` (runs etcd + control-plane) or `slave` (data-plane + data-proxy only) |
| `server` | For slaves: private IP of a master node to connect to |

### 2. Configure Components

Each component has its own `config.yaml`. The `deploy.sh` script auto-fills IP and location fields; you only need to tweak the rest.

#### control-plane/config.yaml

```yaml
port: "7081"                    # Control plane API port

ip_lib: "http://127.0.0.1:7082" # IP geolocation service (internal)

server_list:                    # All master node private IPs (for etcd cluster)
  - "192.168.1.100"
server_ip: "192.168.1.100"      # This node's private IP (masters only, empty for slaves)
data_dir: "/root/etcd"          # etcd data directory

node:                           # Auto-filled by deploy.sh
  provider: "Vultr"
  continent: "Asia"
  country: "JP"
  city: "Tokyo"
  ip:
    private: "0.0.0.0"
    public: "202.182.96.100"
```

#### data-proxy/config.yaml (Port Mapping)

This is where you define **which ports users connect to** and **how traffic is forwarded**:

```yaml
port: "7083"
control_host: "http://127.0.0.1:7081"  # Control plane URL (for routing queries)

# test_routing: defines per-port forwarding targets
# port  = the Data Proxy listening port users connect to
# path  = comma-separated list of "origin_ip:origin_port" (and optional "edge_ip:edge_port")
#
# Format: "origin1_ip,origin2_ip,edge1_ip:edge2_port,origin3_ip:origin_port"
#   - If only IP given → uses the test_routing port as destination port
#   - If "IP:Port" given → uses the specified destination port
test_routing:
  - port: 8081
    path: "104.238.177.110,149.28.88.62,34.20.157.38:8082"
  - port: 8082
    path: "104.238.177.110,149.28.88.62,47.251.90.204:8082"

# listeners: open ports on this node for user traffic
listeners:
  - proto: "tcp"
    port: 8081
    batch_num: 10
  - proto: "tcp"
    port: 8002
    batch_num: 10
  # Uncomment to enable additional protocols/ports:
  # - proto: "udp"
  #   port: 8002
  # - proto: "tcp"
  #   port: 443      # HTTPS acceleration
  # - proto: "udp"
  #   port: 443      # HTTP3/QUIC

# Rate limiting
rate_limit:
  qps: 1000
  burst: 2000
  clean_interval: 5

# Packet aggregation
aggregator:
  buffer_size: 5120
  batch_timeout_ms: 50

node:                           # Auto-filled by deploy.sh
  provider: "Vultr"
  continent: "Asia"
  country: "SG"
  city: "Singapore"
  ip:
    private: "0.0.0.0"
    public: "104.238.177.110"
```

**Key fields explained:**

| Field | Description |
|-------|-------------|
| `listeners[].port` | Port that accepts user connections on this node |
| `listeners[].proto` | Protocol: `tcp` or `udp` |
| `listeners[].batch_num` | Packets per batch for aggregation |
| `test_routing[].port` | The user-facing port; must match a `listeners` port |
| `test_routing[].path` | Target origin servers (comma-separated `IP` or `IP:Port`) |

### 3. Configure Origin Server Probing

Place `probe-targets.json` next to the control-plane binary (same directory as the executable):

```json
[
  {
    "server_port": 8081,
    "provider": "Vultr",
    "ip": "141.164.43.8",
    "port": 8081,
    "region": "Asia",
    "id": "Seoul"
  },
  {
    "server_port": 8082,
    "provider": "Vultr",
    "ip": "76.223.1.167",
    "port": 80,
    "region": "Asia",
    "id": "Singapore"
  }
]
```

| Field | Description |
|-------|-------------|
| `server_port` | Maps to `test_routing[].port` in data-proxy config — links this origin to a user-facing port |
| `ip` / `port` | The actual origin server address to forward traffic to |
| `region` / `id` | Geographic info for probing and routing decisions |

Each entry defines an origin server (e.g., your web server). The Data Plane nodes will probe these origins and report latency back to the Control Plane. When a user request arrives at `POST /api/v1/routing/last` with `Dest.Port`, the system looks up the corresponding origin via `server_port`.

### 4. Deploy

```bash
# Deploy to all nodes defined in cluster-info.txt
bash deploy.sh --deploy-all

# Deploy to a specific node
bash deploy.sh --deploy node1

# Docker alternative (single node)
bash build-docker.sh
```

The `deploy.sh` script:
1. Auto-fills IP, provider, and location fields in all `config.yaml` files
2. Packages the project into a tarball
3. SCPs it to each remote node
4. Runs `setup-systemd.sh` to build Go binaries and register systemd services

### 5. Verify Services

```bash
# On each node:
sudo systemctl status SkyAccel-control-plane
sudo systemctl status SkyAccel-data-plane
sudo systemctl status SkyAccel-data-proxy

# View logs
sudo journalctl -u SkyAccel-control-plane -f
tail -f control-plane/log/app.log
```

---

## User-Facing API

Users (or your client application) query the Control Plane for routing decisions before sending traffic.

### Middle-Mile Routing: `POST /api/v1/routing/middle`

Computes the optimal path between two SkyAccel edge nodes.

```bash
curl -X POST "http://<control-plane-ip>:7081/api/v1/routing/middle?ip=<user-ip>&algorithm=shortest" \
  -H "Content-Type: application/json" \
  -d '{
    "Source": {"Continent": "Asia", "Country": "CN", "City": "Shanghai"},
    "Dest":   {"Port": 8081}
  }'
```

**Request body:**

| Field | Type | Description |
|-------|------|-------------|
| `Source.Continent` | string | User's continent |
| `Source.Country` | string | User's country code |
| `Source.City` | string | User's city |
| `Dest.Port` | int | Target port (matches `server_port` in `probe-targets.json`) |

**Query parameters:**

| Param | Values | Default | Description |
|-------|--------|---------|-------------|
| `ip` | any string | — | Client IP for logging |
| `algorithm` | `shortest`, `carousel-greed`, `lyapunov` | `shortest` | Routing algorithm |

**Response:**

```json
{
  "Code": 200,
  "Msg": "Successfully obtained path",
  "Data": [
    {"Hops": ["149.28.88.62"], "Rtt": 15.2, "RawRTT": 15.2}
  ]
}
```

### Last-Mile Routing: `POST /api/v1/routing/last`

Selects the best edge node to forward traffic to the origin server (CPU + latency aware).

```bash
curl -X POST "http://<control-plane-ip>:7081/api/v1/routing/last?ip=<user-ip>&algorithm=joint_cpu_latency&cpu_weight=0.5&latency_weight=0.5" \
  -H "Content-Type: application/json" \
  -d '{
    "Source": {"Continent": "Asia", "Country": "CN", "City": "Shanghai"},
    "Dest":   {"Port": 8081}
  }'
```

**Query parameters:**

| Param | Values | Default | Description |
|-------|--------|---------|-------------|
| `algorithm` | `p2c`, `ewma`, `lyapunov`, `joint_cpu_latency` | `lyapunov` | Last-mile algorithm |
| `cpu_weight` | 0.0–1.0 | 0.5 | CPU weight for `joint_cpu_latency` |
| `latency_weight` | 0.0–1.0 | 0.5 | Latency weight for `joint_cpu_latency` |

**Algorithms:**

| Algorithm | Description |
|-----------|-------------|
| `p2c` | Power-of-Two-Choices: inverse-CPU proportional distribution across all nodes |
| `ewma` | Exponentially Weighted Moving Average: smooths CPU/latency spikes, per-node scoring without batch normalization |
| `lyapunov` | Lyapunov drift-plus-penalty optimization for joint CPU-latency decision |
| `joint_cpu_latency` | Weighted combination of CPU usage and network latency |

**Response:**

```json
{
  "Code": 200,
  "Msg": "Successfully obtained path",
  "Data": [
    {"Hops": ["104.238.177.110"], "Rtt": 0.4256, "RawRTT": 30.0}
  ]
}
```

- `Rtt` — probability/weight for this path (sums to 1.0 across all paths)
- `RawRTT` — raw combined score (lower = better)

## Port Reference

| Port | Component | Protocol | Description |
|------|-----------|----------|-------------|
| 7081 | Control Plane | TCP (HTTP) | REST API for routing, health, probe tasks |
| 7082 | Data Plane | TCP (HTTP) | IP geolocation service |
| 7083 | Data Proxy | TCP (HTTP) | Data Proxy internal API |
| 8081–8099 | Data Proxy | TCP/UDP | User-facing acceleration ports (configurable in `listeners`) |
| 4433 | Data Proxy | UDP | QUIC tunnel for inter-node communication |
| 2379–2380 | etcd | TCP | etcd cluster communication (internal) |

---

## Directory Structure

```
SkyAccel/
├── control-plane/         # Route computing & API server
│   ├── api/               # REST API handlers (routing, probing)
│   ├── aggregator/        # Telemetry aggregation
│   ├── routing/           # Routing algorithms (graph, edge-domain)
│   ├── sync/              # etcd client & embedded server
│   ├── config.yaml        # Control plane configuration
│   ├── probe-targets.json # Origin server probing targets
│   └── main.go
├── data-plane/            # Telemetry collection & reporting
│   ├── collector/         # CPU, latency collectors
│   ├── report-info/       # Reporting to etcd
│   └── config.yaml
├── data-proxy/            # User-facing proxy & forwarding
│   ├── config.yaml        # Port mapping & routing config
│   └── ...
├── testing/               # Benchmark clients & server
├── cluster-info.txt       # Cluster topology definition
├── deploy.sh              # Multi-node deployment script
├── setup-systemd.sh       # Systemd service registration
├── build-docker.sh        # Docker build & run
└── Dockerfile
```

---

## Management Commands

```bash
# Systemd (on each node)
sudo systemctl start SkyAccel-control-plane
sudo systemctl stop SkyAccel-data-proxy
sudo systemctl restart SkyAccel-data-plane
sudo journalctl -u SkyAccel-control-plane -f

# Docker
docker logs SkyAccel-container -f
docker exec -it SkyAccel-container sh
docker restart SkyAccel-container

# Manual build & run
cd control-plane && go build -o control-plane . && ./control-plane
cd data-plane   && go build -o data-plane .   && ./data-plane
cd data-proxy   && go build -o data-proxy .   && ./data-proxy
```

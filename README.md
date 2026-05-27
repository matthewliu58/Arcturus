# SkyAccel

SkyAccel is a high-performance network acceleration project focused on optimizing network transmission performance and stability. It adopts a layered architecture design, combining heuristic algorithms and Lyapunov optimization to implement network routing, providing users with faster and more reliable network experience.

## System Architecture

The SkyAccel project consists of three main components:

1. **Control Plane**: Responsible for route computing, resource management, and network synchronization
2. **Data Plane**: Responsible for local data collection, analysis, and reporting
3. **Data Proxy**: Responsible for data forwarding, tunnel management, and user access

## Core Features

- **Intelligent Routing Optimization**: Uses heuristic algorithms (carousel-greed) and Lyapunov optimization for segmented routing decisions
- **Real-time Network Monitoring**: Collects network status data and analyzes network quality in real-time
- **QUIC Tunnel**: Establishes secure and efficient transmission tunnels using the QUIC protocol
- **Packet Merging**: Optimizes data packet transmission and reduces network overhead
- **Multi-protocol Support**: Compatible with multiple network protocols including TCP, UDP, and all protocols based on them, providing acceleration for mainstream protocols like HTTP/HTTPS, FTP, SMTP, DNS, RTP, QUIC, and streaming protocols such as HLS, DASH, RTMP, and WebRTC
- **Automatic Fault Detection and Recovery**: Monitors network status in real-time and automatically switches to optimal paths

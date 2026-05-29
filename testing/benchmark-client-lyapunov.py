import socket
import time
import threading
import random
import requests
import json
from datetime import datetime

CONTROL_PLANE_URL = "http://localhost:7081/api/v1/routing/last?ip=1.1.1.1"
SERVER_PORT = 8081
CONCURRENCY = 400
TOTAL_RUN_SECONDS = 100
MIN_SLEEP = 0.2
MAX_SLEEP = 0.5
SOCK_TIMEOUT = 10

LOSS_LOG = "packet_loss.log"
stop_event = threading.Event()
FIXED_SEND_SIZE = 512

# Global routing table, updated every second
routing_servers = []      # List of server IPs
routing_probabilities = []  # List of selection probabilities
routing_lock = threading.RLock()  # Use RLock for read-write lock

def write_loss_log(content):
    try:
        ts = datetime.now().strftime("%Y-%m-%d %H:%M:%S.%f")[:-3]
        with open(LOSS_LOG, "a", encoding="utf-8") as f:
            f.write(f"[{ts}] {content}\n")
    except Exception:
        pass

def fetch_routing_table():
    """Fetch routing table from control-plane every second"""
    while not stop_event.is_set():
        try:
            # TODO: adjust request body as needed
            payload = {
                "source": {
                    "ip": "157.230.41.239",
                    "continent": "Asia",
                    "country": "HongKong",
                    "city": "HongKong"
                }
            }
            resp = requests.post(CONTROL_PLANE_URL, json=payload, timeout=5)
            if resp.status_code == 200:
                data = resp.json()
                if data.get("code") == 200 and "data" in data:
                    routing_info = data["data"]
                    if "routing" in routing_info:
                        routing_table = routing_info["routing"]
                        
                        # Pre-calculate servers and probabilities
                        rtts = []
                        servers = []
                        for route in routing_table:
                            rtt = route.get("rtt", 0)
                            if rtt <= 0:
                                rtt = 1  # minimum value
                            rtts.append(rtt)
                            
                            # Hops is an array, take the first one
                            hops = route.get("hops", [])
                            if hops:
                                servers.append(hops[0])
                            else:
                                servers.append(None)
                        
                        # Normalize RTT to probabilities
                        if rtts and servers:
                            total_rtt = sum(rtts)
                            probabilities = [rtt / total_rtt for rtt in rtts]
                            
                            with routing_lock:
                                global routing_servers, routing_probabilities
                                routing_servers = servers
                                routing_probabilities = probabilities
                            
                            # Print detailed routing table
                            ts = datetime.now().strftime("%Y-%m-%d %H:%M:%S.%f")[:-3]
                            print(f"[{ts}] [ROUTING] Updated {len(routing_servers)} routes:")
                            for i, (server, weight, prob) in enumerate(zip(servers, rtts, probabilities)):
                                print(f"[{ts}]   Route {i+1}: IP={server}, Weight={weight}, Probability={prob*100:.2f}%")
        except Exception as e:
            print(f"[ROUTING] Fetch failed: {e}")
        
        time.sleep(1)

def select_server_by_rtt():
    """Select server IP based on pre-calculated probabilities"""
    with routing_lock:
        if not routing_servers or not routing_probabilities:
            return None
        
        # Random selection based on pre-calculated probabilities
        rand_val = random.uniform(0, 1)
        cumulative = 0
        for i, prob in enumerate(routing_probabilities):
            cumulative += prob
            if rand_val <= cumulative:
                return routing_servers[i]
        
        return routing_servers[-1]

def read_server_response(sock):
    buf = b""
    try:
        while True:
            chunk = sock.recv(1024)  # Optimized: read 1024 bytes at a time instead of 1
            if not chunk:
                break
            if b"\n" in chunk:
                buf += chunk.split(b"\n")[0]
                break
            buf += chunk
    except socket.timeout:
        return None
    return buf.decode().strip()

def client_worker():
    # Random initial delay to disperse connections
    init_delay = random.uniform(0, 3)
    time.sleep(init_delay)
    req_count = 0

    while not stop_event.is_set():
        # Select server based on RTT weight
        server_ip = select_server_by_rtt()
        if not server_ip:
            print(f"[{threading.current_thread().name}] No available server, waiting...")
            time.sleep(1)
            continue
        
        sock = None
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(SOCK_TIMEOUT)
            sock.connect((server_ip, SERVER_PORT))
            req_count += 1

            send_dt = datetime.now()
            time_fmt = send_dt.strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "+08:00"
            thread_name = threading.current_thread().name
            show_msg = f"[{thread_name}][{req_count:06d}] {time_fmt}"
            original_bytes = show_msg.encode()

            # Pad with null bytes to fixed size, append newline at the end
            pad_len = FIXED_SEND_SIZE - len(original_bytes) - 1
            if pad_len < 0:
                pad_len = 0
            send_data = original_bytes + b"\x00" * pad_len + b"\n"

            sock.sendall(send_data)
            resp = read_server_response(sock)

            recv_dt = datetime.now()
            cost_ms = (recv_dt - send_dt).total_seconds() * 1000

            if resp is None:
                err_info = f"{thread_name} timeout, no response received {show_msg}"
                write_loss_log(err_info)
                print(f"{err_info}")
            else:
                if random.random() < 0.02:
                    print(f"[{thread_name}] Server:{server_ip} Latency:{cost_ms:.2f}ms")

        except Exception as e:
            err_info = f"{threading.current_thread().name} exception:{str(e)}"
            write_loss_log(err_info)
        finally:
            # Close socket safely
            if sock:
                try:
                    sock.close()
                except Exception:
                    pass

        # Random sleep interval between requests
        sleep_sec = random.uniform(MIN_SLEEP, MAX_SLEEP)
        time.sleep(sleep_sec)

if __name__ == "__main__":
    print(f"Concurrency:{CONCURRENCY} Fixed 512-byte packets, null padding + \\n")
    print(f"Control-plane URL: {CONTROL_PLANE_URL}")
    
    # Start routing table fetcher thread
    routing_thread = threading.Thread(target=fetch_routing_table, name="ROUTING-FETCHER", daemon=True)
    routing_thread.start()
    
    # Wait for first routing table
    time.sleep(2)
    
    # Start client workers
    for idx in range(CONCURRENCY):
        t = threading.Thread(target=client_worker, name=f"CLIENT-{idx+1}", daemon=True)
        t.start()

    time.sleep(TOTAL_RUN_SECONDS)
    stop_event.set()
    print("\nPressure test finished")

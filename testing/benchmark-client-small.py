import socket
import time
import threading
import random
from datetime import datetime

SERVER_IP = "104.238.177.110"
SERVER_PORT = 8081
CONCURRENCY = 40
TOTAL_RUN_SECONDS = 120
# Random sleep interval between two connections to avoid reconnection peaks
MIN_SLEEP = 0.2
MAX_SLEEP = 0.5
# Socket timeout
SOCK_TIMEOUT = 10
# Payload size in bytes (0 = no padding, use original message size)
PAYLOAD_SIZE = 1024

LOSS_LOG = "packet_loss.log"
stop_event = threading.Event()

def write_loss_log(content):
    try:
        ts = datetime.now().strftime("%Y-%m-%d %H:%M:%S.%f")[:-3]
        with open(LOSS_LOG, "a", encoding="utf-8") as f:
            f.write(f"[{ts}] {content}\n")
    except Exception:
        pass

# Read a single line response from server by newline delimiter
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
    # Random delay at startup to disperse initial connections
    init_delay = random.uniform(0, 3)
    time.sleep(init_delay)
    req_count = 0

    while not stop_event.is_set():
        sock = None
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(SOCK_TIMEOUT)
            sock.connect((SERVER_IP, SERVER_PORT))
            req_count += 1

            # Build and send message
            send_dt = datetime.now()
            time_fmt = send_dt.strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "+08:00"
            thread_name = threading.current_thread().name  # Get thread name
            send_msg = f"[{thread_name}][{req_count:06d}] {time_fmt}"
            
            # Pad message to target size if PAYLOAD_SIZE > 0
            if PAYLOAD_SIZE > 0:
                current_len = len(send_msg)
                if current_len < PAYLOAD_SIZE - 1:  # Reserve 1 byte for \n
                    send_msg += " " * (PAYLOAD_SIZE - 1 - current_len)
            send_msg += "\n"
            
            sock.sendall(send_msg.encode())

            # Block and wait for server response
            resp = read_server_response(sock)
            recv_dt = datetime.now()
            cost_ms = (recv_dt - send_dt).total_seconds() * 1000

            if resp is None:
                err_info = f"{threading.current_thread().name} timeout, no response received {send_msg.strip()}"
                write_loss_log(err_info)
                print(f"{err_info}")
            else:
                if random.random() < 0.02:
                    print(f"[{threading.current_thread().name}] SendLen:{len(send_msg)} RecvLen:{len(resp)} Send:{send_msg.strip()} Recv:{resp} Latency:{cost_ms:.2f}ms")
        except Exception as e:
            err_info = f"{threading.current_thread().name} exception:{str(e)}"
            write_loss_log(err_info)
        finally:
            # Close connection immediately after response/exception
            if sock:
                try:
                    sock.close()
                except Exception:
                    pass

        # Random sleep to make each thread run at different pace
        sleep_sec = random.uniform(MIN_SLEEP, MAX_SLEEP)
        time.sleep(sleep_sec)

if __name__ == "__main__":
    payload_info = f"payload size: {PAYLOAD_SIZE}B" if PAYLOAD_SIZE > 0 else "variable payload size"
    print(f"Starting concurrency:{CONCURRENCY}, {payload_info}, single send & single receive, disconnect after response, stable pressure test")
    for idx in range(CONCURRENCY):
        t = threading.Thread(target=client_worker, name=f"CLIENT-{idx+1}", daemon=True)
        t.start()

    time.sleep(TOTAL_RUN_SECONDS)
    stop_event.set()
    print("\nPressure test finished, check log: cat packet_loss.log")
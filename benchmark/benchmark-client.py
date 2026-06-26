import socket
import time
import threading
import random
from datetime import datetime

SERVER_IP = "141.164.51.166"
SERVER_PORT = 8081
CONCURRENCY = 200
TOTAL_RUN_SECONDS = 20
MIN_SLEEP = 0.2
MAX_SLEEP = 0.5
SOCK_TIMEOUT = 10

LOSS_LOG = "packet_loss.log"
stop_event = threading.Event()
FIXED_SEND_SIZE = 512

def write_loss_log(content):
    try:
        ts = datetime.now().strftime("%Y-%m-%d %H:%M:%S.%f")[:-3]
        with open(LOSS_LOG, "a", encoding="utf-8") as f:
            f.write(f"[{ts}] {content}\n")
    except Exception:
        pass

# Read single line response from server by newline delimiter
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
        sock = None
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(SOCK_TIMEOUT)
            sock.connect((SERVER_IP, SERVER_PORT))
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
                    print(f"[{thread_name}] Sent:{show_msg} Latency:{cost_ms:.2f}ms")

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
    for idx in range(CONCURRENCY):
        t = threading.Thread(target=client_worker, name=f"CLIENT-{idx+1}", daemon=True)
        t.start()

    time.sleep(TOTAL_RUN_SECONDS)
    stop_event.set()
    print("\nPressure test finished")
import socket
import time
import threading
import random
from datetime import datetime

SERVER_IP = "47.83.232.28"
SERVER_PORT = 8081
CONCURRENCY = 600
TOTAL_RUN_SECONDS = 120
# Random sleep between connections to avoid connection spikes
MIN_SLEEP = 0.2
MAX_SLEEP = 0.5
# Socket timeout in seconds
SOCK_TIMEOUT = 10

LOSS_LOG = "packet_loss.log"
stop_event = threading.Event()

def write_loss_log(content):
    try:
        ts = datetime.now().strftime("%Y-%m-%d %H:%M:%S.%f")[:-3]
        with open(LOSS_LOG, "a", encoding="utf-8") as f:
            f.write(f"[{ts}] {content}\n")
    except Exception:
        pass

# Read server response line by newline
def read_server_response(sock):
    buf = b""
    while True:
        try:
            chunk = sock.recv(1)
        except socket.timeout:
            return None
        if not chunk or chunk == b"\n":
            break
        buf += chunk
    return buf.decode().strip()

def client_worker():
    # Random delay at startup to spread initial connections
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

            # Assemble message with thread name
            #send_dt = datetime.now()
            #time_fmt = send_dt.strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "+08:00"
            #send_msg = f"[{req_count:06d}] {time_fmt}\n"
            #sock.sendall(send_msg.encode())

            # Assemble message with thread name
            send_dt = datetime.now()
            time_fmt = send_dt.strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "+08:00"
            thread_name = threading.current_thread().name  # Get thread name
            send_msg = f"[{thread_name}][{req_count:06d}] {time_fmt}\n"  # Include in message
            sock.sendall(send_msg.encode())

            # Blocking wait for server response
            resp = read_server_response(sock)
            recv_dt = datetime.now()
            cost_ms = (recv_dt - send_dt).total_seconds() * 1000

            if resp is None:
                err_info = f"{threading.current_thread().name} timeout, no response {send_msg.strip()}"
                write_loss_log(err_info)
                print(f"[WARN] {err_info}")
            else:
                if random.random() < 0.02:
                    print(f"[{threading.current_thread().name}] send:{send_msg.strip()} recv:{resp} latency:{cost_ms:.2f}ms")

        except Exception as e:
            err_info = f"{threading.current_thread().name} error:{str(e)}"
            write_loss_log(err_info)
        finally:
            # Close connection after response/error
            if sock:
                try:
                    sock.close()
                except Exception:
                    pass

        # Random sleep to avoid synchronized reconnects
        sleep_sec = random.uniform(MIN_SLEEP, MAX_SLEEP)
        time.sleep(sleep_sec)

if __name__ == "__main__":
    print(f"Starting {CONCURRENCY} concurrent clients, send-receive-close pattern, steady benchmark")
    for idx in range(CONCURRENCY):
        t = threading.Thread(target=client_worker, name=f"CLIENT-{idx+1}", daemon=True)
        t.start()

    time.sleep(TOTAL_RUN_SECONDS)
    stop_event.set()
    print("\nBenchmark completed, check log: cat packet_loss.log")
import socket
import time
import threading
import random
from datetime import datetime

SERVER_IP = "47.83.232.28"
SERVER_PORT = 8081
CONCURRENCY = 50
RUN_DURATION = 5
SEND_INTERVAL = 0.1
TOTAL_RUN_SECONDS = 50

START_DELAY_MAX = 3

LOSS_LOG = "packet_loss.log"

def write_loss_log(content):
    try:
        ts = datetime.now().strftime("%Y-%m-%d %H:%M:%S.%f")[:-3]
        with open(LOSS_LOG, "a", encoding="utf-8") as f:
            f.write(f"[{ts}] {content}\n")
    except:
        pass

def read_line(sock):
    try:
        buf = b""
        while True:
            ch = sock.recv(1)
            if not ch or ch == b"\n":
                break
            buf += ch
        return buf.decode().strip()
    except:
        return None

stop_event = threading.Event()

def client_task():
    delay = random.uniform(0, START_DELAY_MAX)
    time.sleep(delay)

    round_num = 0
    while not stop_event.is_set():
        sock = None
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(2)
            sock.connect((SERVER_IP, SERVER_PORT))
            start_time = time.time()

            while time.time() - start_time < RUN_DURATION and not stop_event.is_set():
                try:
                    round_num += 1
                    send_ts = datetime.now()
                    time_str = send_ts.strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "+08:00"
                    msg = f"[{round_num:06d}] {time_str}\n"
                    sock.sendall(msg.encode())

                    recv_str = read_line(sock)
                    if recv_str is None:
                        log_msg = f"{threading.current_thread().name} packet loss | {msg.strip()}"
                        write_loss_log(log_msg)
                        print(f"!!!{log_msg}")
                        break

                    recv_ts = datetime.now()
                    gap = (recv_ts - send_ts).total_seconds() * 1000
                    print(f"[{threading.current_thread().name}] Send→{msg.strip()} Recv←{recv_str} Time={gap:.2f}ms")
                    time.sleep(SEND_INTERVAL)

                except Exception as e:
                    write_loss_log(f"{threading.current_thread().name} error: {str(e)}")
                    break
        except:
            pass
        finally:
            if sock:
                try:
                    sock.close()
                except:
                    pass
            time.sleep(0.2)

print(f"Starting {CONCURRENCY} concurrent clients")
for i in range(CONCURRENCY):
    threading.Thread(target=client_task, name=f"CLIENT-{i+1}", daemon=True).start()

time.sleep(TOTAL_RUN_SECONDS)
stop_event.set()
print("\nBenchmark finished! Check packet loss: cat packet_loss.log")
import socket
import time
import threading
import random
from datetime import datetime

SERVER_IP = "47.83.232.28"
SERVER_PORT = 8081
CONCURRENCY = 600
TOTAL_RUN_SECONDS = 120
# 两轮连接之间随机休眠区间，错开重连高峰
MIN_SLEEP = 0.2
MAX_SLEEP = 0.5
# 套接字超时
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

# 按换行读取服务端单行回复
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
    # 启动阶段随机延迟，分散初始连接
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

            # 组装发送报文
            #send_dt = datetime.now()
            #time_fmt = send_dt.strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "+08:00"
            #send_msg = f"[{req_count:06d}] {time_fmt}\n"
            #sock.sendall(send_msg.encode())

            # 组装发送报文 → 我加了线程名
            send_dt = datetime.now()
            time_fmt = send_dt.strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "+08:00"
            thread_name = threading.current_thread().name  # 🔥 拿到线程号
            send_msg = f"[{thread_name}][{req_count:06d}] {time_fmt}\n"  # 🔥 塞进消息里
            sock.sendall(send_msg.encode())

            # 阻塞等待服务端回复，必须收到再往下走
            resp = read_server_response(sock)
            recv_dt = datetime.now()
            cost_ms = (recv_dt - send_dt).total_seconds() * 1000

            if resp is None:
                err_info = f"{threading.current_thread().name} 超时未收到回复 {send_msg.strip()}"
                write_loss_log(err_info)
                print(f"⚠️ {err_info}")
            else:
                if random.random() < 0.02:
                    print(f"[{threading.current_thread().name}] 发:{send_msg.strip()} 收:{resp} 时延:{cost_ms:.2f}ms")

        except Exception as e:
            err_info = f"{threading.current_thread().name} 异常:{str(e)}"
            write_loss_log(err_info)
        finally:
            # 收到回复/异常后立刻关闭连接
            if sock:
                try:
                    sock.close()
                except Exception:
                    pass

        # 随机休眠，每个线程节奏完全不同，不会集体断连重连
        sleep_sec = random.uniform(MIN_SLEEP, MAX_SLEEP)
        time.sleep(sleep_sec)

if __name__ == "__main__":
    print(f"🚀 启动并发:{CONCURRENCY}，单发单收，收完回复立即断连，平稳压测")
    for idx in range(CONCURRENCY):
        t = threading.Thread(target=client_worker, name=f"CLIENT-{idx+1}", daemon=True)
        t.start()

    time.sleep(TOTAL_RUN_SECONDS)
    stop_event.set()
    print("\n✅ 压测结束，日志查看：cat packet_loss.log")
import socket
import threading

server = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
server.bind(("0.0.0.0", 8082))
server.listen(10)

# 🔥 已改成英文后缀
APPEND_STR = "-server-response"

print("✅ Long connection + Split by \\n + Auto disconnect on timeout")

def handle(conn, addr):
    print("Client connected:", addr)
    buffer = b""

    conn.settimeout(10)

    while True:
        try:
            data = conn.recv(1024)
            if not data:
                print("Client disconnected normally:", addr)
                break

            buffer += data

            while b'\n' in buffer:
                line, buffer = buffer.split(b'\n', 1)
                if line:
                    print("Received message:", line.decode("utf-8", "ignore"))

                    # 拼接英文后缀
                    response = line + APPEND_STR.encode("utf-8")

                    conn.sendall(response + b'\n')

        except socket.timeout:
            print("Timeout disconnect (no data from client):", addr)
            break

        except Exception as e:
            print("Error disconnect:", e)
            break

    conn.close()
    print("Connection closed\n")

while True:
    conn, addr = server.accept()
    threading.Thread(target=handle, args=(conn, addr), daemon=True).start()
import socket
import threading

# Create TCP socket
server = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
# Allow reuse of local address/port
server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
# Bind to all interfaces on port 8082
server.bind(("0.0.0.0", 8081))
# Listen for incoming connections (backlog=100)
server.listen(100)

print("Server started: 0.0.0.0:8081 | Auto-filter \\x00 padding | Echo valid data + \\n")

def handle(conn, addr):
    """Handle client connection in a separate thread"""
    print("Client connected:", addr)
    buffer = b""
    # Set 10-second timeout for socket operations
    conn.settimeout(10)

    while True:
        try:
            # Receive up to 1024 bytes from client
            data = conn.recv(1024)
            if not data:
                # Empty data means client closed the connection
                print("Client disconnected:", addr)
                break

            # Append received data to buffer
            buffer += data

            # Split packets by newline delimiter
            while b'\n' in buffer:
                line, buffer = buffer.split(b'\n', 1)
                if line:
                    # ====================== Core Logic ======================
                    # Remove all null bytes (\\x00) used for padding
                    clean_line = line.replace(b'\x00', b'')
                    # ======================================================

                    print("Received:", clean_line.decode("utf-8", "ignore"))

                    # Send cleaned data back to client with newline
                    conn.sendall(clean_line + b'\n')

        except socket.timeout:
            # Connection closed due to timeout
            print("Timeout disconnect:", addr)
            break
        except Exception as e:
            # Handle other exceptions
            print("Error:", e, addr)
            break

    # Close client connection
    conn.close()

# Main loop: accept incoming connections
while True:
    conn, addr = server.accept()
    # Start a new daemon thread for each client
    threading.Thread(target=handle, args=(conn, addr), daemon=True).start()
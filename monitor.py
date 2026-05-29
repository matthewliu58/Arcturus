import subprocess
import time
from datetime import datetime

# Process name to monitor
PROCESS_NAME = "data-proxy"
# Log file name
LOG_FILE = "proxy_monitor.log"

def get_top_snapshot():
    try:
        output = subprocess.check_output(
            ["top", "-b", "-n", "1"],
            encoding="utf-8",
            timeout=3
        )
        return output
    except:
        return ""

def monitor():
    print(f"Monitoring {PROCESS_NAME} every second, logging to: {LOG_FILE}\n")
    
    # Open file in append mode
    with open(LOG_FILE, "a", encoding="utf-8") as f:
        while True:
            ts = time.strftime("%Y-%m-%d %H:%M:%S")
            data = get_top_snapshot()
            
            print(f"[{ts}] ------------------------------")
            f.write(f"[{ts}] ------------------------------\n")
            
            for line in data.splitlines():
                if PROCESS_NAME in line:
                    line_clean = line.strip()
                    # Print to console
                    print(line_clean)
                    # Write to file
                    f.write(line_clean + "\n")
            
            # Flush buffer to write immediately
            f.flush()
            time.sleep(1)

if __name__ == "__main__":
    monitor()

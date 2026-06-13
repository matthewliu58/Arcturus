import subprocess
import os
import glob

def main():
    # Change to parent directory (core-domain)
    script_dir = os.path.dirname(os.path.abspath(__file__))
    parent_dir = os.path.dirname(script_dir)
    os.chdir(parent_dir)
    
    results_file = "janos-us-ca_livenet_test_results.txt"
    
    # Clear/create results file
    with open(results_file, "w") as f:
        f.write("=" * 70 + "\n")
        f.write("LiveStyle Algorithm - 20 Random Test Runs\n")
        f.write("=" * 70 + "\n\n")
    
    avg_rtts = []
    har_means = []
    
    # Find all 20 log files
    log_files = sorted(glob.glob("janos-us-ca_livenet_test_random_*_*.log"))
    print(f"Found {len(log_files)} log files")
    
    for i, log_file in enumerate(log_files[:20], 1):
        print(f"\n{'='*60}")
        print(f"Processing Run {i}/20: {log_file}")
        print(f"{'='*60}")
        
        # Run evaluation
        result = subprocess.run(f"py evaluation/run_livenet-evaluation.py {log_file}", shell=True, capture_output=True, text=True)
        eval_output = result.stdout
        print(eval_output)
        
        # Save to results file
        with open(results_file, "a") as f:
            f.write(f"--- Run {i} ---\n")
            f.write(eval_output)
            f.write("\n")
        
        # Parse results for summary
        for line in eval_output.split('\n'):
            if 'Mean RTT' in line:
                try:
                    avg_rtts.append(float(line.split(':')[1].strip().split()[0]))
                except:
                    pass
            if 'Overall HAR mean' in line:
                try:
                    har_means.append(float(line.split(':')[1].strip()))
                except:
                    pass
    
    # Write summary
    with open(results_file, "a") as f:
        f.write("=" * 70 + "\n")
        f.write("Summary of 20 Runs\n")
        f.write("=" * 70 + "\n")
        
        if avg_rtts:
            f.write(f"\nAverage RTT across all runs: {sum(avg_rtts)/len(avg_rtts):.2f} ms\n")
            f.write(f"Min RTT: {min(avg_rtts):.2f} ms\n")
            f.write(f"Max RTT: {max(avg_rtts):.2f} ms\n")
        
        if har_means:
            f.write(f"\nAverage HAR across all runs: {sum(har_means)/len(har_means):.3f}\n")
            f.write(f"Min HAR: {min(har_means):.3f}\n")
            f.write(f"Max HAR: {max(har_means):.3f}\n")
    
    print(f"\nResults saved to {results_file}")
    print(f"Processed {len(avg_rtts)} runs")

if __name__ == "__main__":
    main()
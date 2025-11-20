import numpy as np
import time
import itertools
import matplotlib.pyplot as plt
import pandas as pd
import os
import json
from typing import Dict, List, Tuple, Any

# Simulate system metrics collection functions
def get_requests_per_second():
    """Simulate requests per second (RPS)"""
    base_rps = 5000
    return base_rps * (1 + 0.2 * np.random.randn())

def get_cpu_utilization():
    """Simulate CPU utilization"""
    return 0.3 + 0.6 * np.random.random()

def get_requests_processed_per_unit_time():
    """Simulate requests processed per unit time (RQPT)"""
    base_rqpt = 4500  # Slightly lower than RPS to simulate processing delay
    return base_rqpt * (1 + 0.15 * np.random.randn())

def get_average_request_time(current_params=None):
    """
    Simulate average request processing time (ART)
    Takes current parameters into account
    """
    base_time = 50.0  # Base processing time (ms)

    if current_params is not None:
        # Impact of packet_merge_timeout - linear increase
        tp_impact = current_params['packet_merge_timeout'] / 5.0

        # Impact of multiplexing_sessions - quadratic curve, optimal in the middle
        optimal_sp = 5
        sp_diff = abs(current_params['multiplexing_sessions'] - optimal_sp) / optimal_sp
        sp_impact = 0.5 * sp_diff**2

        # Impact of concurrency_levels - quadratic curve, optimal in the middle
        optimal_cp = 125
        cp_diff = abs(current_params['concurrency_levels'] - optimal_cp) / optimal_cp
        cp_impact = 0.5 * cp_diff**2

        param_impact = (tp_impact + sp_impact + cp_impact) / 3.0
        return base_time * (1 + param_impact) * (1 + 0.1 * np.random.randn())
    else:
        return base_time * (1 + 0.1 * np.random.randn())

def apply_system_parameters(params):
    """Simulate applying parameters to the system"""
    print(f"Applying new parameters: {params}")
    return True

class ActionSpace:
    def __init__(self):
        # Define parameter search ranges according to the document
        self.actions = {
            'multiplexing_sessions': list(range(1, 11)),  # Sp ∈ [1, 10], step size 1
            'concurrency_levels': list(range(50, 201, 10)),  # Cp ∈ [50, 200], step size 10
            'packet_merge_timeout': list(range(1, 6))  # Tp ∈ [1, 5]ms, step size 1
        }

    def get_actions(self):
        """Return all possible action combinations"""
        return list(itertools.product(
            self.actions['multiplexing_sessions'],
            self.actions['concurrency_levels'],
            self.actions['packet_merge_timeout']
        ))

    def apply_action(self, action_idx):
        """Convert action index to actual parameters"""
        action = self.get_actions()[action_idx]
        return {
            'multiplexing_sessions': action[0],
            'concurrency_levels': action[1],
            'packet_merge_timeout': action[2]
        }

class LinUCBModel:
    def __init__(self, action_count, feature_dim=4, alpha=0.5):
        self.action_count = action_count
        self.feature_dim = feature_dim
        self.alpha = alpha
        self.last_action = None

        # Initialize model parameters for each action
        self.A = [np.identity(feature_dim) for _ in range(action_count)]
        self.b = [np.zeros(feature_dim) for _ in range(action_count)]
        self.theta = [np.zeros(feature_dim) for _ in range(action_count)]

    def select_action(self, features):
        """Compute UCB values and select the optimal action based on current features"""
        max_ucb = -float('inf')
        best_action = 0

        for a in range(self.action_count):
            # Compute expected reward estimate for action a
            theta_a = np.linalg.inv(self.A[a]).dot(self.b[a])

            # Compute uncertainty of the feature vector in the direction of action a
            uncertainty = self.alpha * np.sqrt(
                features.dot(np.linalg.inv(self.A[a])).dot(features)
            )

            # UCB value = expected reward + exploration bonus
            ucb = features.dot(theta_a) + uncertainty

            if ucb > max_ucb:
                max_ucb = ucb
                best_action = a

        self.last_action = best_action
        return best_action

    def update(self, features, reward):
        """Update model parameters based on observed reward"""
        if self.last_action is None:
            return

        A_a = self.A[self.last_action]
        b_a = self.b[self.last_action]

        # Update A matrix and b vector
        A_a += np.outer(features, features)
        b_a += reward * features

        # Update theta
        self.theta[self.last_action] = np.linalg.inv(A_a).dot(b_a)

class ParameterOptimizer:
    def __init__(self, initial_params, feature_dim=4, alpha=0.5,
                 min_adjustment_interval=300, simulation_mode=False):
        self.current_params = initial_params
        self.simulation_mode = simulation_mode

        # Create action space
        self.action_space = ActionSpace()
        self.actions = self.action_space.get_actions()

        # Initialize LinUCB model
        self.model = LinUCBModel(
            action_count=len(self.actions),
            feature_dim=feature_dim,
            alpha=alpha
        )

        # Performance history
        self.performance_history = []

        # Last adjustment time
        self.last_adjustment_time = time.time()
        self.min_adjustment_interval = min_adjustment_interval

        # Weight settings (initial values of 0.5 as per document)
        self.w_rqpt = 0.5
        self.w_art = 0.5

        # Track historical min/max metrics for normalization
        self.metrics_history = {
            'rqpt': [],
            'art': []
        }

        # Store current parameters (for simulation mode)
        self._current_system_params = initial_params

    def extract_features(self, metrics):
        """Construct feature vector from system metrics (4 features)"""
        return np.array([
            metrics['cpu_utilization'],  # CPU utilization
            metrics['rps'] / 10000.0,    # Normalized RPS
            metrics['rqpt'] / 10000.0,   # Normalized RQPT
            metrics['art'] / 100.0       # Normalized ART
        ])

    def collect_system_metrics(self):
        """Collect current system state metrics"""
        if self.simulation_mode:
            metrics = {
                'cpu_utilization': get_cpu_utilization(),
                'rps': get_requests_per_second(),
                'rqpt': get_requests_processed_per_unit_time(),
                'art': get_average_request_time(self._current_system_params)
            }
        else:
            # Actual system implementation
            metrics = {
                'cpu_utilization': get_cpu_utilization(),
                'rps': get_requests_per_second(),
                'rqpt': get_requests_processed_per_unit_time(),
                'art': get_average_request_time()
            }

        # Update historical metrics
        self.metrics_history['rqpt'].append(metrics['rqpt'])
        self.metrics_history['art'].append(metrics['art'])

        return metrics

    def calculate_reward(self, metrics):
        """Calculate weighted reward value"""
        if len(self.metrics_history['rqpt']) < 2 or len(self.metrics_history['art']) < 2:
            # Insufficient historical data, use simple calculation
            return metrics['rqpt'] / metrics['art']

        # Normalize RQPT
        rqpt_min = min(self.metrics_history['rqpt'])
        rqpt_max = max(self.metrics_history['rqpt'])

        if rqpt_max > rqpt_min:
            rqpt_norm = (metrics['rqpt'] - rqpt_min) / (rqpt_max - rqpt_min)
        else:
            rqpt_norm = 0.5

        # Normalize ART
        art_min = min(self.metrics_history['art'])
        art_max = max(self.metrics_history['art'])

        if art_max > art_min:
            art_norm = (metrics['art'] - art_min) / (art_max - art_min)
        else:
            art_norm = 0.5

        # Calculate reward (formula specified in document)
        reward = self.w_rqpt * rqpt_norm + self.w_art * (1 - art_norm)

        return reward

    def optimize_parameters(self):
        """Execute parameter optimization decision loop"""
        # Check if minimum adjustment interval has been reached
        current_time = time.time()
        if current_time - self.last_adjustment_time < self.min_adjustment_interval:
            return self.current_params

        # Collect current system metrics
        metrics = self.collect_system_metrics()
        features = self.extract_features(metrics)

        # Select optimal action
        action_idx = self.model.select_action(features)

        # Apply parameter adjustment
        new_params = self.action_space.apply_action(action_idx)

        # Record pre-adjustment performance metrics
        self.pre_adjustment_metrics = metrics

        # Update current parameters
        self.current_params = new_params
        self.last_adjustment_time = current_time

        # Update system parameters in simulation mode
        if self.simulation_mode:
            self._current_system_params = new_params

        return new_params

    def collect_feedback_and_update(self, wait_time=5):
        """Collect feedback after parameter adjustment and update model"""
        if self.simulation_mode:
            time.sleep(wait_time)
        else:
            time.sleep(300)  # Wait 5 minutes in actual system

        # Collect post-adjustment performance metrics
        post_metrics = self.collect_system_metrics()

        # Calculate reward
        reward = self.calculate_reward(post_metrics)

        # Get pre-adjustment feature vector
        features = self.extract_features(self.pre_adjustment_metrics)

        # Update model
        self.model.update(features, reward)

        # Record performance history
        self.performance_history.append({
            'params': self.current_params.copy(),
            'metrics': post_metrics,
            'reward': reward,
            'timestamp': time.time()
        })

        return post_metrics, reward

    def save_history(self, filename='optimization_history.json'):
        """Save optimization history to file"""
        serializable_history = []
        for record in self.performance_history:
            serializable_record = {
                'params': record['params'],
                'metrics': {
                    'cpu_utilization': float(record['metrics']['cpu_utilization']),
                    'rps': float(record['metrics']['rps']),
                    'rqpt': float(record['metrics']['rqpt']),
                    'art': float(record['metrics']['art'])
                },
                'reward': float(record['reward']),
                'timestamp': record['timestamp']
            }
            serializable_history.append(serializable_record)

        with open(filename, 'w') as f:
            json.dump(serializable_history, f, indent=2)

        print(f"Optimization history saved to {filename}")

    def plot_performance_history(self, save_path=None):
        """Plot performance history charts"""
        if not self.performance_history:
            print("Insufficient historical data for plotting")
            return

        # Extract data
        timestamps = [h['timestamp'] for h in self.performance_history]
        relative_times = [(t - timestamps[0])/60 for t in timestamps]
        rewards = [h['reward'] for h in self.performance_history]
        art_values = [h['metrics']['art'] for h in self.performance_history]
        rqpt_values = [h['metrics']['rqpt'] for h in self.performance_history]

        # Create charts
        fig, (ax1, ax2, ax3) = plt.subplots(3, 1, figsize=(12, 10), sharex=True)

        # Plot rewards
        ax1.plot(relative_times, rewards, 'b-', marker='o')
        ax1.set_ylabel('Reward Value')
        ax1.set_title('LinUCB Optimization Performance History')
        ax1.grid(True)

        # Plot ART
        ax2.plot(relative_times, art_values, 'r-', marker='x')
        ax2.set_ylabel('Average Request Time (ART) (ms)')
        ax2.grid(True)

        # Plot RQPT
        ax3.plot(relative_times, rqpt_values, 'g-', marker='s')
        ax3.set_xlabel('Time (minutes)')
        ax3.set_ylabel('Requests Processed Per Unit Time (RQPT)')
        ax3.grid(True)

        plt.tight_layout()

        if save_path:
            plt.savefig(save_path)
            print(f"Performance history chart saved to {save_path}")
        else:
            plt.show()

def initialize_system(simulation_mode=False):
    """Initialize parameter optimization system"""
    # Set initial parameters (middle values according to document)
    initial_params = {
        'multiplexing_sessions': 5,    # Sp
        'concurrency_levels': 125,     # Cp
        'packet_merge_timeout': 3      # Tp
    }

    # Create optimizer
    optimizer = ParameterOptimizer(
        initial_params=initial_params,
        feature_dim=4,  # 4 features
        alpha=0.5,
        min_adjustment_interval=60 if simulation_mode else 300,
        simulation_mode=simulation_mode
    )

    # Collect initial system metrics
    metrics = optimizer.collect_system_metrics()
    optimizer.performance_history.append({
        'params': initial_params.copy(),
        'metrics': metrics,
        'reward': optimizer.calculate_reward(metrics),
        'timestamp': time.time()
    })

    return optimizer

def main_optimization_loop(optimizer, iterations=100, simulation_mode=False):
    """Main parameter optimization loop"""
    if simulation_mode:
        for i in range(iterations):
            try:
                print(f"\nIteration {i+1}/{iterations} ...")

                # Parameter adjustment decision
                new_params = optimizer.optimize_parameters()

                # Apply new parameters
                apply_system_parameters(new_params)

                # Collect feedback and update model
                post_metrics, reward = optimizer.collect_feedback_and_update(wait_time=1)

                print(f"Post-adjustment metrics: RQPT={post_metrics['rqpt']:.2f}, ART={post_metrics['art']:.2f}ms")
                print(f"Reward value: {reward:.4f}")

                # Periodically save historical data
                if (i+1) % 10 == 0:
                    optimizer.save_history()

            except Exception as e:
                print(f"Error during optimization process: {e}")
                time.sleep(5)
    else:
        # Actual system operation logic
        run_time = 86400  # One day
        start_time = time.time()

        while time.time() - start_time < run_time:
            try:
                # Parameter adjustment decision
                new_params = optimizer.optimize_parameters()

                # Apply new parameters
                apply_system_parameters(new_params)

                # Collect feedback and update model
                optimizer.collect_feedback_and_update()

                # Wait for next decision cycle
                time.sleep(60)

                # Periodically save historical data
                if int((time.time() - start_time) / 3600) % 2 == 0:
                    optimizer.save_history()

            except Exception as e:
                print(f"Error during optimization process: {e}")
                time.sleep(300)

def analyze_performance(optimizer):
    """Analyze optimization results and generate report"""
    if len(optimizer.performance_history) < 2:
        print("Insufficient historical data for analysis")
        return

    # Calculate key metrics
    first_record = optimizer.performance_history[0]
    last_record = optimizer.performance_history[-1]
    best_record = max(optimizer.performance_history, key=lambda h: h['reward'])

    # Performance report
    print("\n====== Performance Optimization Report ======")
    print(f"Total iterations: {len(optimizer.performance_history)}")

    print("\nInitial performance:")
    print(f"  RQPT: {first_record['metrics']['rqpt']:.2f}")
    print(f"  ART: {first_record['metrics']['art']:.2f} ms")
    print(f"  Parameters: {first_record['params']}")

    print("\nFinal performance:")
    print(f"  RQPT: {last_record['metrics']['rqpt']:.2f}")
    print(f"  ART: {last_record['metrics']['art']:.2f} ms")
    print(f"  Parameters: {last_record['params']}")

    print("\nBest performance:")
    print(f"  RQPT: {best_record['metrics']['rqpt']:.2f}")
    print(f"  ART: {best_record['metrics']['art']:.2f} ms")
    print(f"  Parameters: {best_record['params']}")

    # Performance improvement
    rqpt_improvement = ((best_record['metrics']['rqpt'] - first_record['metrics']['rqpt']) /
                       first_record['metrics']['rqpt']) * 100
    art_improvement = ((first_record['metrics']['art'] - best_record['metrics']['art']) /
                      first_record['metrics']['art']) * 100

    print("\nPerformance improvement:")
    print(f"  RQPT: +{rqpt_improvement:.2f}%")
    print(f"  ART: -{art_improvement:.2f}%")

    # Plot performance history chart
    optimizer.plot_performance_history(save_path="performance_history.png")

if __name__ == "__main__":
    # Set random seed
    np.random.seed(42)

    # Start simulation mode
    simulation_mode = True
    print("Starting LinUCB parameter optimization system (simulation mode)...")

    # Initialize system
    optimizer = initialize_system(simulation_mode=simulation_mode)

    # Run optimization loop
    print("Starting parameter optimization...")
    main_optimization_loop(optimizer, iterations=50, simulation_mode=simulation_mode)

    # Analyze optimization results
    analyze_performance(optimizer)

    # Save optimization history
    optimizer.save_history()

    print("\nOptimization complete!")
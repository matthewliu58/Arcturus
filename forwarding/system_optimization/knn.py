import pandas as pd
import numpy as np
from enum import Enum
from sklearn.neighbors import NearestNeighbors
import re
import matplotlib.pyplot as plt
import matplotlib.font_manager as fm

plt.rcParams['font.sans-serif'] = ['SimHei', 'Microsoft YaHei', 'SimSun']  # Chinese fonts setting

# Fix the issue where negative sign '-' appears as a box
plt.rcParams['axes.unicode_minus'] = False

# Define outlier type enumeration
class OutlierType(Enum):
    SMALL = "small_outlier"
    LARGE = "large_outlier"


# Outlier class
class Outlier:
    def __init__(self, index, value, outlier_type, score):
        self.index = index
        self.value = value
        self.type = outlier_type
        self.score = score


# KNN outlier detector class
class AdaptiveKnnOutlierDetector:
    def __init__(self, k=5, sensitivity=1.5):
        """
        Initialize a 1-D outlier detector

        Parameters:
        k - number of neighbors used for neighbor calculations
        sensitivity - sensitivity coefficient, larger values detect fewer outliers
        """
        self.k = k
        self.sensitivity = sensitivity

    def detect_outliers(self, data):
        """
        Detect outliers in one-dimensional data

        Parameters:
        data - one-dimensional array

        Returns:
        list of detected outliers containing index, value, type and score
        """
        if len(data) <= self.k:
            return []  # not enough data for meaningful analysis

        # convert input data to numpy array
        data_np = np.array(data, dtype=float)
        n = len(data_np)

        # create (index, value) pairs to preserve original indices
        indexed_data = [(i, val) for i, val in enumerate(data_np)]

        # sort by value while keeping original indices
        indexed_data.sort(key=lambda x: x[1])

        # extract sorted indices and values
        indices = [item[0] for item in indexed_data]
        sorted_data = [item[1] for item in indexed_data]

        # compute basic statistics
        mean = np.mean(sorted_data)
        std_dev = np.std(sorted_data)

        # use KNN to find k-neighbor distances for each point
        X = np.array(sorted_data).reshape(-1, 1)
        nbrs = NearestNeighbors(n_neighbors=min(self.k + 1, len(X))).fit(X)
        distances, _ = nbrs.kneighbors(X)

        # remove the first distance (self-distance = 0)
        k_distances = distances[:, 1:].mean(axis=1)

        # compute threshold for k-distances using median and MAD
        median_dist = np.median(k_distances)
        mad = np.median(np.abs(k_distances - median_dist))

        # ensure MAD is not zero
        if mad < 0.0001:
            mad = max(0.0001, std_dev * 0.6745)  # 0.6745 scales MAD to match std for normal dist

        # compute gaps used for density-based cluster separation
        gaps = [sorted_data[i + 1] - sorted_data[i] for i in range(n - 1)]

        # compute median and MAD of gaps
        gap_median = np.median(gaps)
        gap_mad = np.median(np.abs(np.array(gaps) - gap_median))

        # ensure gap MAD is not zero
        if gap_mad < 0.0001:
            gap_mad = max(0.0001, np.std(gaps) * 0.6745)

        # identify significant gaps to split data into clusters
        sig_gap_threshold = gap_median + 2.5 * gap_mad / 0.6745
        sig_gap_positions = [i for i, gap in enumerate(gaps) if gap > sig_gap_threshold]

        # split data into clusters
        clusters = []
        if sig_gap_positions:
            start_idx = 0
            for gap_pos in sig_gap_positions:
                cluster = list(range(start_idx, gap_pos + 1))
                if cluster:
                    clusters.append(cluster)
                start_idx = gap_pos + 1

            # add last cluster
            if start_idx < n:
                last_cluster = list(range(start_idx, n))
                if last_cluster:
                    clusters.append(last_cluster)
        else:
            # if no significant gaps, treat all data as one cluster
            clusters = [list(range(n))]

        # choose main cluster based on cluster size and closeness to global mean
        main_cluster_idx = 0
        main_cluster_score = -1.0

        for i, cluster in enumerate(clusters):
            if not cluster:
                continue

            # extract cluster values
            cluster_values = [sorted_data[idx] for idx in cluster]
            cluster_mean = np.mean(cluster_values)

            # size score
            size_score = len(cluster) / n

            # position score (closeness to overall mean)
            position_score = 1.0 - min(1.0, abs((cluster_mean - mean) / mean) if mean != 0 else 0)

            # total score: 75% size, 25% position
            total_score = size_score * 0.75 + position_score * 0.25

            if total_score > main_cluster_score:
                main_cluster_score = total_score
                main_cluster_idx = i

        # ensure valid main cluster
        if main_cluster_idx >= len(clusters) or not clusters[main_cluster_idx]:
            return []

        # indices of main cluster within sorted_data
        main_cluster_indices = set(clusters[main_cluster_idx])

        # main cluster statistics
        main_cluster_values = [sorted_data[idx] for idx in main_cluster_indices]
        main_cluster_mean = np.mean(main_cluster_values)
        main_cluster_std = np.std(main_cluster_values)

        # compute quartiles for the main cluster
        main_cluster_sorted = sorted(main_cluster_values)
        q1_pos = int(len(main_cluster_sorted) * 0.25)
        q3_pos = int(len(main_cluster_sorted) * 0.75)
        q1 = main_cluster_sorted[q1_pos]
        q3 = main_cluster_sorted[q3_pos]
        iqr = q3 - q1

        # adaptive thresholds combining multiple methods
        # std method
        small_threshold_std = main_cluster_mean - self.sensitivity * 2.0 * main_cluster_std
        large_threshold_std = main_cluster_mean + self.sensitivity * 2.0 * main_cluster_std

        # IQR method
        small_threshold_iqr = q1 - self.sensitivity * 1.5 * iqr
        large_threshold_iqr = q3 + self.sensitivity * 1.5 * iqr

        # final thresholds - take the more permissive thresholds of the two methods
        small_threshold = min(small_threshold_std, small_threshold_iqr)
        large_threshold = max(large_threshold_std, large_threshold_iqr)

        # detect outliers
        outliers = []
        for i, value in enumerate(data_np):
            # small-value outliers
            if value < small_threshold:
                deviation = (small_threshold - value) / main_cluster_std
                score = max(1.0, deviation ** 1.2)
                outliers.append(Outlier(i, value, OutlierType.SMALL, score))
            # large-value outliers
            elif value > large_threshold:
                deviation = (value - large_threshold) / main_cluster_std

                # if value far exceeds main cluster range, boost score
                if value > max(main_cluster_values) + (max(main_cluster_values) - min(main_cluster_values)) * 0.5:
                    deviation *= 1.8

                score = max(1.0, deviation ** 1.3)
                outliers.append(Outlier(i, value, OutlierType.LARGE, score))

        # sort outliers by type and score (small before large, then by descending score)
        outliers.sort(key=lambda x: (-1 if x.type == OutlierType.SMALL else 1, -x.score))

        return outliers

    def visualize(self, data, outliers=None, title="1-D Outlier Detection", xlabel="Index", ylabel="Value", filename=None):
        """Visualize data and outliers"""
        if outliers is None:
            outliers = self.detect_outliers(data)

        # proportion of normal data points
        normal_ratio = (len(data) - len(outliers)) / len(data) if len(data) > 0 else 0

        # append normal ratio info to title
        title = f"{title} (Normal data ratio: {normal_ratio:.4f})"

        plt.figure(figsize=(12, 6))

        # plot all data points
        plt.scatter(range(len(data)), data, c='blue', alpha=0.6, label='Normal points')

        # mark outliers
        outlier_indices = [o.index for o in outliers]
        outlier_values = [data[i] for i in outlier_indices]
        outlier_types = [o.type for o in outliers]

        small_indices = [idx for idx, otype in zip(outlier_indices, outlier_types) if otype == OutlierType.SMALL]
        large_indices = [idx for idx, otype in zip(outlier_indices, outlier_types) if otype == OutlierType.LARGE]

        small_values = [data[i] for i in small_indices]
        large_values = [data[i] for i in large_indices]

        if small_indices:
            plt.scatter(small_indices, small_values, c='green', s=100, alpha=0.8, marker='o', label='Small outliers')

        if large_indices:
            plt.scatter(large_indices, large_values, c='red', s=100, alpha=0.8, marker='o', label='Large outliers')

        plt.title(title)
        plt.xlabel(xlabel)
        plt.ylabel(ylabel)
        plt.legend()
        plt.grid(True, alpha=0.3)

        if filename:
            plt.savefig(filename)
        else:
            plt.show()

        return plt


def calculate_overall_normal_ratio(results):
    """
    Calculate overall normal data point ratio across all IP pairs

    Parameters:
    results - dict containing analysis results for each IP pair

    Returns:
    overall ratio, total normal points, total data points, and per-IP normal point details
    """
    total_normal_points = 0
    total_points = 0

    # list to store normal points per IP for display
    normal_points_by_ip = []

    for ip_pair, ip_results in results.items():
        if 'all_sizes' in ip_results:
            # total data points for this IP pair
            data_points = len(ip_results['all_sizes']['total_time_ms'])
            # number of outliers
            outliers = len(ip_results['all_sizes']['outliers'])
            # normal points count
            normal_points = data_points - outliers

            # aggregate totals
            total_normal_points += normal_points
            total_points += data_points

            # save per-ip info
            normal_points_by_ip.append((ip_pair, normal_points, data_points))

    overall_ratio = total_normal_points / total_points if total_points > 0 else 0

    return overall_ratio, total_normal_points, total_points, normal_points_by_ip

def process_network_latency_by_lines(file_path, start_line=10442, end_line=12865):
    """
    Process network latency data by line range and perform KNN outlier detection

    Parameters:
    file_path - path to the input file
    start_line - starting line number (1-based)
    end_line - ending line number

    Returns:
    processing results
    """
    try:
        print(f"Extracting lines {start_line} to {end_line} from file {file_path}...")

        # read specified line range
        data_lines = []
        line_counter = 0

        with open(file_path, 'r', encoding='utf-8') as f:
            # skip preceding lines
            for _ in range(start_line - 1):
                next(f, None)
                line_counter += 1

            # read desired line range
            for line in f:
                line_counter += 1
                if line_counter > end_line:
                    break

                line = line.strip()
                if line:  # ignore empty lines
                    data_lines.append(line)

        print(f"Successfully extracted {len(data_lines)} lines")

        # check first line to infer format
        if not data_lines:
            print("No data extracted, please check the line range")
            return None

        print(f"Sample line:\n{data_lines[0]}")

        # try to determine delimiter
        first_line = data_lines[0]
        delimiter = None

        if ',' in first_line:
            delimiter = ','
            print("Detected CSV format (comma-separated)")
        else:
            # assume space-separated
            delimiter = None  # will use split() later
            print("Detected space-separated format")

        # parse data lines
        parsed_data = []
        for line in data_lines:
            if delimiter == ',':
                values = line.split(',')
            else:
                values = line.split()

            # ensure enough columns
            if len(values) < 10:  # CSV expected to have 10 columns
                print(f"Warning: line '{line}' has insufficient columns")
                continue

            # extract fields with robust parsing
            try:
                timestamp = values[0]
                src_ip = values[1]
                dst_ip = values[2]

                # extract round info
                round_info = values[3]
                if '/' in round_info:
                    current_round = int(round_info.split('/')[0])
                else:
                    round_match = re.search(r'\d+', round_info)
                    current_round = int(round_match.group()) if round_match else 0

                # data size
                data_size = values[4]

                # status
                status = values[5]

                # upload/download speeds (may be empty)
                upload_speed = int(values[6]) if values[6] and values[6].isdigit() else 0
                download_speed = int(values[7]) if values[7] and values[7].isdigit() else 0

                # TCP handshake and total time (may be empty)
                tcp_time = float(values[8]) if values[8] else 0.0
                total_time = float(values[9]) if values[9] else 0.0

                data_row = {
                    'timestamp': timestamp,
                    'src_ip': src_ip,
                    'dst_ip': dst_ip,
                    'round_info': round_info,
                    'current_round': current_round,
                    'data_size': data_size,
                    'status': status,
                    'upload_speed': upload_speed,
                    'download_speed': download_speed,
                    'tcp_time': tcp_time,
                    'total_time': total_time,
                    'total_time_ms': total_time * 1000  # convert to milliseconds
                }

                parsed_data.append(data_row)

            except (ValueError, IndexError) as e:
                print(f"Warning: error parsing line '{line}': {e}")
                continue

        # create DataFrame
        df = pd.DataFrame(parsed_data)

        if df.empty:
            print("No data parsed successfully")
            return None

        print(f"Successfully parsed {len(df)} rows")
        print(f"Data preview:\n{df.head()}")

        # filter valid rows (status == SUCCESS and total_time > 0)
        valid_df = df[(df['status'] == 'SUCCESS') & (df['total_time'] > 0)]
        print(f"Valid rows count: {len(valid_df)}")

        # count distinct IP pairs
        ip_pairs = valid_df.groupby(['src_ip', 'dst_ip']).size().reset_index(name='count')
        print(f"Found {len(ip_pairs)} distinct IP pairs in the data:")
        for idx, row in ip_pairs.iterrows():
            print(f"  {row['src_ip']} -> {row['dst_ip']}: {row['count']} records")

        # analyze per IP pair
        results = {}

        for (src_ip, dst_ip), group_data in valid_df.groupby(['src_ip', 'dst_ip']):
            ip_pair_key = f"{src_ip} -> {dst_ip}"
            print(f"\nProcessing IP pair: {ip_pair_key}")

            # further group by data size
            for size, size_group in group_data.groupby('data_size'):
                print(f"  Analyzing size {size} with {len(size_group)} records")

                total_time_ms = size_group['total_time_ms'].values

                # choose k
                k_value = min(5, len(total_time_ms))
                if k_value < 2:
                    print(f"    Not enough points for KNN analysis, skipping")
                    continue

                detector = AdaptiveKnnOutlierDetector(k=k_value, sensitivity=1.5)
                outliers = detector.detect_outliers(total_time_ms)

                # compute normal data ratio
                normal_ratio = (len(total_time_ms) - len(outliers)) / len(total_time_ms) if len(total_time_ms) > 0 else 0

                # store results
                if ip_pair_key not in results:
                    results[ip_pair_key] = {}

                size_key = f"size_{size}"
                results[ip_pair_key][size_key] = {
                    'data': size_group,
                    'total_time_ms': total_time_ms,
                    'outliers': outliers,
                    'normal_data_ratio': normal_ratio
                }

                # print outlier info
                print(f"    Outlier analysis:")
                print(f"    Normal data ratio: {normal_ratio:.4f} ({len(total_time_ms) - len(outliers)}/{len(total_time_ms)})")
                if outliers:
                    for o in outliers:
                        round_num = size_group.iloc[o.index]['current_round']
                        print(f"      Round: {round_num}, Total time: {o.value:.2f}ms, Type: {o.type.value}, Score: {o.score:.2f}")
                else:
                    print("      No outliers detected")

                # visualize and save
                title = f'IP Pair {ip_pair_key} Size {size} Total Time (ms) Outlier Detection'
                filename = f"{src_ip}_to_{dst_ip}_size_{size}_lines_{start_line}_{end_line}_outliers.png"
                detector.visualize(total_time_ms, outliers, title=title, xlabel="Data Index", ylabel="Total Time (ms)", filename=filename)
                print(f"    Saved visualization: {filename}")

            # overall analysis for the IP pair
            print(f"  Overall analysis for IP pair {ip_pair_key} ({len(group_data)} records)")

            total_time_ms = group_data['total_time_ms'].values

            k_value = min(5, len(total_time_ms))
            if k_value < 2:
                print(f"    Not enough points for KNN analysis, skipping")
                continue

            detector = AdaptiveKnnOutlierDetector(k=k_value, sensitivity=1.5)
            outliers = detector.detect_outliers(total_time_ms)

            normal_ratio = (len(total_time_ms) - len(outliers)) / len(total_time_ms) if len(total_time_ms) > 0 else 0

            if ip_pair_key not in results:
                results[ip_pair_key] = {}

            results[ip_pair_key]['all_sizes'] = {
                'data': group_data,
                'total_time_ms': total_time_ms,
                'outliers': outliers,
                'normal_data_ratio': normal_ratio
            }

            print(f"    Overall outlier analysis:")
            print(f"    Normal data ratio: {normal_ratio:.4f} ({len(total_time_ms) - len(outliers)}/{len(total_time_ms)})")
            if outliers:
                for o in outliers:
                    round_num = group_data.iloc[o.index]['current_round']
                    data_size = group_data.iloc[o.index]['data_size']
                    print(f"      Round: {round_num}, Size: {data_size}, Total time: {o.value:.2f}ms, Type: {o.type.value}, Score: {o.score:.2f}")
            else:
                print("      No outliers detected")

            title = f'IP Pair {ip_pair_key} All Sizes Total Time (ms) Outlier Detection'
            filename = f"{src_ip}_to_{dst_ip}_all_sizes_lines_{start_line}_{end_line}_outliers.png"
            detector.visualize(total_time_ms, outliers, title=title, xlabel="Data Index", ylabel="Total Time (ms)", filename=filename)
            print(f"    Saved visualization: {filename}")

        # summary report
        print("\nOutlier detection summary:")
        for ip_pair, ip_results in results.items():
            print(f"\nIP pair: {ip_pair}")

            if 'all_sizes' in ip_results:
                all_outliers = len(ip_results['all_sizes']['outliers'])
                all_data_points = len(ip_results['all_sizes']['total_time_ms'])
                normal_ratio = ip_results['all_sizes']['normal_data_ratio']

                print(f"  All sizes: detected {all_outliers} outliers, normal data ratio: {normal_ratio:.4f} ({all_data_points - all_outliers}/{all_data_points})")

            for size_key, size_results in ip_results.items():
                if size_key != 'all_sizes':
                    outliers = size_results['outliers']
                    data_points = len(size_results['total_time_ms'])
                    normal_ratio = size_results['normal_data_ratio']

                    size = size_key.replace('size_', '')
                    print(f"  Size {size}: detected {len(outliers)} outliers, normal data ratio: {normal_ratio:.4f} ({data_points - len(outliers)}/{data_points})")

            if 'all_sizes' in ip_results:
                total_normal_ratio = ip_results['all_sizes']['normal_data_ratio']
                print(f"  Overall normal data ratio for this IP pair: {total_normal_ratio:.4f}")

        # sort IP pairs by normal ratio
        print("\nIP pairs sorted by normal data ratio:")
        ip_pair_ratios = []
        for ip_pair, ip_results in results.items():
            if 'all_sizes' in ip_results:
                ratio = ip_results['all_sizes']['normal_data_ratio']
                total_points = len(ip_results['all_sizes']['total_time_ms'])
                normal_points = total_points - len(ip_results['all_sizes']['outliers'])
                ip_pair_ratios.append((ip_pair, ratio, normal_points, total_points))

        ip_pair_ratios.sort(key=lambda x: x[1], reverse=True)

        for i, (ip_pair, ratio, normal_points, total_points) in enumerate(ip_pair_ratios, 1):
            print(f"{i}. {ip_pair}: Normal data ratio = {ratio:.4f} ({normal_points}/{total_points})")

        # compute overall normal ratio
        overall_ratio, total_normal, total_data, normal_points_detail = calculate_overall_normal_ratio(results)

        # print detailed overall calculation
        print("\nCalculating overall normal data ratio:")
        print("Sum normal points across IP pairs divided by total data points:")

        print("Normal points details:")
        normal_points_list = []
        for ip_pair, normal, total in normal_points_detail:
            normal_points_list.append(str(normal))
            print(f"  {ip_pair}: {normal}/{total}")

        normal_points_str = " + ".join(normal_points_list)
        print(f"Total normal points: {normal_points_str} = {total_normal}")
        print(f"Total data points: {total_data // len(normal_points_detail)} × {len(normal_points_detail)} = {total_data}")
        print(f"Overall normal data ratio: {total_normal} ÷ {total_data} = {overall_ratio:.4f}")

        print("\nAnalysis complete! All results saved.")
        return results

    except Exception as e:
        print(f"Error processing data: {e}")
        import traceback
        traceback.print_exc()
        return None


def process_raw_text_data_by_lines(text_data, start_line=1, end_line=None):
    """
    Process raw text data directly

    Parameters:
    text_data - raw text content
    start_line - starting line number (default 1)
    end_line - ending line number (default None, meaning all lines)

    Returns:
    processing results
    """
    try:
        # split text into lines
        lines = text_data.strip().split('\n')

        # if end_line is None, process to end
        if end_line is None:
            end_line = len(lines)

        # validate line range
        if start_line < 1 or start_line > len(lines) or end_line < start_line or end_line > len(lines):
            print(f"Error: invalid line range. Text has {len(lines)} lines but range {start_line}-{end_line} was given")
            return None

        # select lines
        selected_lines = lines[start_line - 1:end_line]

        # join selected lines into temporary text
        selected_text = '\n'.join(selected_lines)

        # write to temporary file
        temp_file = "temp_network_data.txt"
        with open(temp_file, 'w', encoding='utf-8') as f:
            f.write(selected_text)

        print(f"Saved {len(selected_lines)} extracted lines to temporary file and processing...")
        results = process_network_latency_by_lines(temp_file, 1, len(selected_lines))

        # try to remove temp file
        import os
        try:
            os.remove(temp_file)
        except:
            pass

        return results

    except Exception as e:
        print(f"Error processing raw text data: {e}")
        import traceback
        traceback.print_exc()
        return None


# main function
def main():
    print("Starting processing of network latency data by line range...")

    # file and line range to use
    file_path = "network_latency_34.90.77.82.csv"
    start_line = 10442
    end_line = 12865

    results = process_network_latency_by_lines(file_path, start_line, end_line)

    if results:
        print("\nProcessing completed! All results saved.")
    else:
        print("\nProcessing failed. Please check file path and line range.")


if __name__ == "__main__":
    main()
#!/usr/bin/env python3
"""
Plot average response times from workload_stats JSON files.
Converts from nanoseconds to milliseconds for display.
"""

import json
import re
import glob
import os
from pathlib import Path
import matplotlib.pyplot as plt

def extract_x_value_and_postfix(filename):
    """Extract x-axis value (number) and postfix (series name) from filename.
    
    Example: 'workload_stats_10_CP2.json' -> (10, 'CP2')
    Example: 'workload_stats_500_NoTr.json' -> (500, 'NoTr')
    """
    match = re.search(r'workload_stats_(\d+)_(.+)\.json', filename)
    if match:
        x_value = int(match.group(1))
        postfix = match.group(2)
        return x_value, postfix
    return None, None

def load_workload_stats(filepath):
    """Load JSON file and extract average latency (in nanoseconds)"""
    with open(filepath, 'r') as f:
        data = json.load(f)
        # average_latency_ms is actually in nanoseconds despite the name
        return data.get('average_latency_ms', 0)

def main():
    # Find all workload_stats JSON files
    workload_dir = Path(__file__).parent
    files = glob.glob(str(workload_dir / 'workload_stats_*.json'))
    
    # Organize data by postfix (series name): {postfix: {x_value: latency_ms}}
    series_data = {}
    
    for filepath in files:
        filename = os.path.basename(filepath)
        x_value, postfix = extract_x_value_and_postfix(filename)
        
        if x_value is None or postfix is None:
            print(f"Skipping {filename}: could not parse x-value or postfix")
            continue
        
        # Load average latency (in nanoseconds)
        latency_ns = load_workload_stats(filepath)
        # Convert to milliseconds
        latency_ms = latency_ns / 1_000_000
        
        # Store in the appropriate series
        if postfix not in series_data:
            series_data[postfix] = {}
        series_data[postfix][x_value] = latency_ms
    
    if not series_data:
        print("No valid data files found!")
        return
    
    # Create the plot
    plt.figure(figsize=(10, 6))
    
    # Define markers and colors for different series
    markers = ['o', 's', '^', 'v', 'D', 'p', '*', 'h']
    colors = plt.cm.tab10(range(len(series_data)))
    
    # Plot each series
    for idx, (postfix, data) in enumerate(sorted(series_data.items())):
        sorted_data = sorted(data.items())
        if sorted_data:
            x_values, y_values = zip(*sorted_data)
            marker = markers[idx % len(markers)]
            color = colors[idx]
            plt.plot(x_values, y_values, marker=marker, label=postfix, 
                    linewidth=2, markersize=8, color=color)
    
    plt.xlabel('X-Axis Value', fontsize=12)
    plt.ylabel('Average Response Time (ms)', fontsize=12)
    plt.title('Average Response Time vs X-Axis Value', fontsize=14, fontweight='bold')
    plt.grid(True, alpha=0.3)
    plt.legend(fontsize=11)
    
    # Set x-axis to show all x-values
    all_x_values = set()
    for data in series_data.values():
        all_x_values.update(data.keys())
    if all_x_values:
        plt.xticks(sorted(all_x_values))
    
    plt.tight_layout()
    
    # Save the plot
    output_file = workload_dir / 'response_times_plot.png'
    plt.savefig(output_file, dpi=300, bbox_inches='tight')
    print(f"Plot saved to: {output_file}")
    
    # Also display the plot
    plt.show()
    
    # Print summary table
    print("\nSummary of Average Response Times (ms):")
    print("=" * 80)
    
    # Build header with all postfixes
    all_x_values = sorted(all_x_values)
    postfixes = sorted(series_data.keys())
    header = f"{'X-Value':<12}"
    for postfix in postfixes:
        header += f"{postfix:<15}"
    print(header)
    print("-" * 80)
    
    # Print data rows
    for x_val in all_x_values:
        row = f"{x_val:<12}"
        for postfix in postfixes:
            latency = series_data[postfix].get(x_val, None)
            latency_str = f"{latency:.2f}" if latency is not None else "N/A"
            row += f"{latency_str:<15}"
        print(row)

if __name__ == '__main__':
    main()


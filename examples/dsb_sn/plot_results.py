#!/usr/bin/env python3
import os
import re
import sys
import matplotlib.pyplot as plt
from pathlib import Path

def parse_aggregate_out(filepath, metric_type):
    """Parses an aggregate.out file and returns a dict of load_level -> latency value.
    
    Args:
        filepath: Path to aggregate.out file
        metric_type: Either 'mean', 'p99', or 'max'
    
    Returns:
        Dictionary mapping load_level -> latency value
    """
    results = {}
    with open(filepath, 'r') as f:
        content = f.read()
    
    if metric_type == 'mean':
        # Match pattern: "50:\n  Mean: 9.913ms, ..."
        pattern = r'(\d+):\s*\n\s*Mean:\s+([\d.]+)ms'
    elif metric_type == 'p99':
        # Match pattern: "50:\n  Mean: 9.913ms, 99%: 14.28ms, ..."
        pattern = r'(\d+):\s*\n\s*Mean:\s+[\d.]+ms,\s+99%:\s+([\d.]+)ms'
    elif metric_type == 'max':
        # Match pattern: "50:\n  Mean: 9.913ms, 99%: 14.28ms, Max: 23.184ms"
        pattern = r'(\d+):\s*\n\s*Mean:\s+[\d.]+ms,\s+99%:\s+[\d.]+ms,\s+Max:\s+([\d.]+)ms'
    else:
        raise ValueError(f"Invalid metric_type: {metric_type}. Must be 'mean', 'p99', or 'max'")
    
    matches = re.findall(pattern, content)
    
    for load_level_str, latency_str in matches:
        load_level = int(load_level_str)
        latency = float(latency_str)
        results[load_level] = latency
    
    return results

def get_name_from_subdir(subdir_path):
    """Finds the name_ file in a subdirectory and extracts the name."""
    name_files = list(subdir_path.glob('name_*'))
    if name_files:
        # Extract name from filename (remove "name_" prefix)
        name_file = name_files[0]
        name = name_file.name.replace('name_', '').replace('_', ' ')
        return name
    # Fallback to directory name if no name_ file found
    return subdir_path.name

def plot_results(results_dir, metric_type, output_filename=None, min_data_point=None, max_data_point=None):
    """Plots latency results from all subdirectories.
    
    Args:
        results_dir: Path to results directory
        metric_type: Either 'mean', 'p99', or 'max'
        output_filename: Optional custom output filename (default: based on metric_type)
        min_data_point: Optional minimum load level to include (inclusive)
        max_data_point: Optional maximum load level to include (inclusive)
    """
    results_path = Path(results_dir)
    
    if not results_path.is_dir():
        print(f"Error: '{results_dir}' is not a directory.", file=sys.stderr)
        sys.exit(1)
    
    if metric_type not in ['mean', 'p99', 'max']:
        print(f"Error: metric_type must be 'mean', 'p99', or 'max', got '{metric_type}'", file=sys.stderr)
        sys.exit(1)
    
    # Find all subdirectories with aggregate.out files
    data_to_plot = {}
    
    for subdir in results_path.iterdir():
        if not subdir.is_dir():
            continue
        
        aggregate_file = subdir / 'aggregate.out'
        if not aggregate_file.exists():
            continue
        
        # Parse aggregate.out
        latency_results = parse_aggregate_out(aggregate_file, metric_type)
        if not latency_results:
            print(f"Warning: No data found in {aggregate_file}", file=sys.stderr)
            continue
        
        # Get name from name_ file
        name = get_name_from_subdir(subdir)
        data_to_plot[name] = latency_results
    
    if not data_to_plot:
        print(f"Error: No aggregate.out files found in subdirectories of '{results_dir}'", file=sys.stderr)
        sys.exit(1)
    
    # Create the plot
    plt.figure(figsize=(10, 6))
    
    for name, results in sorted(data_to_plot.items()):
        load_levels = sorted(results.keys())
        
        # Filter by min and max data points if specified
        if min_data_point is not None:
            load_levels = [level for level in load_levels if level >= min_data_point]
        if max_data_point is not None:
            load_levels = [level for level in load_levels if level <= max_data_point]
        
        if not load_levels:
            print(f"Warning: No data points in range for {name} after filtering", file=sys.stderr)
            continue
        
        latencies = [results[level] for level in load_levels]
        plt.plot(load_levels, latencies, marker='o', label=name, linewidth=2)
    
    plt.xlabel('Load Level (concurrent connections)', fontsize=12)
    
    if metric_type == 'mean':
        ylabel = 'Mean Latency (ms)'
        title = 'Mean Latency Comparison Across Configurations'
        default_filename = 'mean_latency_comparison.png'
    elif metric_type == 'p99':
        ylabel = '99th Percentile Latency (ms)'
        title = '99th Percentile Latency Comparison Across Configurations'
        default_filename = 'p99_latency_comparison.png'
    else:  # max
        ylabel = 'Max Latency (ms)'
        title = 'Max Latency Comparison Across Configurations'
        default_filename = 'max_latency_comparison.png'
    
    plt.ylabel(ylabel, fontsize=12)
    plt.title(title, fontsize=14, fontweight='bold')
    plt.legend(loc='best', fontsize=10)
    plt.grid(True, alpha=0.3)
    plt.tight_layout()
    
    # Save the plot
    if output_filename:
        output_file = results_path / output_filename
    else:
        output_file = results_path / default_filename
    
    plt.savefig(output_file, dpi=300, bbox_inches='tight')
    print(f"Plot saved to {output_file}")
    
    # Also show the plot
    plt.show()

if __name__ == "__main__":
    if len(sys.argv) < 3 or len(sys.argv) > 6:
        print("Usage: python3 plot_results.py <results_directory> <type> [output_filename] [min_data_point] [max_data_point]", file=sys.stderr)
        print("  type: 'mean', 'p99', or 'max'", file=sys.stderr)
        print("  output_filename: optional custom output filename (default: based on type)", file=sys.stderr)
        print("  min_data_point: optional minimum load level to include (inclusive)", file=sys.stderr)
        print("  max_data_point: optional maximum load level to include (inclusive)", file=sys.stderr)
        sys.exit(1)
    
    results_dir = sys.argv[1]
    metric_type = sys.argv[2]
    
    # Parse optional arguments
    output_filename = None
    min_data_point = None
    max_data_point = None
    
    # Check each optional argument position
    if len(sys.argv) >= 4:
        if sys.argv[3].isdigit():
            # sys.argv[3] is a number, so it's min_data_point
            min_data_point = int(sys.argv[3])
        else:
            # sys.argv[3] is a string, so it's output_filename
            output_filename = sys.argv[3]
    
    if len(sys.argv) >= 5:
        if output_filename:
            # We already have output_filename, so sys.argv[4] is min_data_point
            min_data_point = int(sys.argv[4])
        else:
            # No output_filename, so sys.argv[4] is max_data_point (sys.argv[3] was min_data_point)
            max_data_point = int(sys.argv[4])
    
    if len(sys.argv) >= 6:
        # sys.argv[5] is always max_data_point
        max_data_point = int(sys.argv[5])
    
    plot_results(results_dir, metric_type, output_filename, min_data_point, max_data_point)


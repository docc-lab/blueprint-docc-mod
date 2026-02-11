#!/usr/bin/env python3
import os
import re
import sys
import matplotlib.pyplot as plt
from pathlib import Path

def parse_aggregate_out(filepath, metric_type):
    """Parses an aggregate.out file and returns a dict of load_level -> latency value (in ms).
    Accepts values in ms or s and normalizes to milliseconds.
    
    Args:
        filepath: Path to aggregate.out file
        metric_type: Either 'mean', 'p99', or 'max'
    
    Returns:
        Dictionary mapping load_level -> latency value in ms
    """
    results = {}
    with open(filepath, 'r') as f:
        content = f.read()
    
    # Unit can be ms or s; capture value and unit, normalize to ms
    unit_suffix = r'([\d.]+)(ms|s)'
    if metric_type == 'mean':
        # Match pattern: "50:\n  Mean: 9.913ms, ..." or "9.913s"
        pattern = r'(\d+):\s*\n\s*Mean:\s+' + unit_suffix
    elif metric_type == 'p99':
        pattern = r'(\d+):\s*\n\s*Mean:\s+[\d.]+(?:ms|s),\s+99%:\s+' + unit_suffix
    elif metric_type == 'max':
        pattern = r'(\d+):\s*\n\s*Mean:\s+[\d.]+(?:ms|s),\s+99%:\s+[\d.]+(?:ms|s),\s+Max:\s+' + unit_suffix
    else:
        raise ValueError(f"Invalid metric_type: {metric_type}. Must be 'mean', 'p99', or 'max'")
    
    matches = re.findall(pattern, content)
    
    for load_level_str, latency_str, unit in matches:
        load_level = int(load_level_str)
        latency = float(latency_str)
        # Convert seconds to milliseconds when value is postfixed with "s"
        if unit.lower() == 's':
            latency *= 1000.0
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

def plot_results(results_dir, metric_type, output_filename=None, min_data_point=None, max_data_point=None, targets=None, clamp_x=False):
    """Plots latency results from all subdirectories.
    
    Args:
        results_dir: Path to results directory
        metric_type: Either 'mean', 'p99', or 'max'
        output_filename: Optional custom output filename (default: based on metric_type)
        min_data_point: Optional minimum load level to include (inclusive)
        max_data_point: Optional maximum load level to include (inclusive)
        targets: Optional list of subdirectory names to include (if None, includes all)
        clamp_x: If True, set x-axis range to exactly the min/max of plotted data points
    """
    results_path = Path(results_dir)
    
    if not results_path.is_dir():
        print(f"Error: '{results_dir}' is not a directory.", file=sys.stderr)
        sys.exit(1)
    
    if metric_type not in ['mean', 'p99', 'max']:
        print(f"Error: metric_type must be 'mean', 'p99', or 'max', got '{metric_type}'", file=sys.stderr)
        sys.exit(1)
    
    # Normalize targets (subdirectory names to include), if provided
    target_set = set(targets) if targets is not None else None

    # Find all subdirectories with aggregate.out files
    data_to_plot = {}
    
    for subdir in results_path.iterdir():
        if not subdir.is_dir():
            continue

        # If targets list is provided, only include matching subdirectories
        if target_set is not None and subdir.name not in target_set:
            continue
        
        aggregate_file = subdir / 'aggregate.out'
        if not aggregate_file.exists():
            continue
        
        # Parse aggregate.out
        latency_results = parse_aggregate_out(aggregate_file, metric_type)
        if not latency_results:
            print(f"Warning: No data found in {aggregate_file}", file=sys.stderr)
            continue
        
        # Get human-readable name from name_ file (or fallback to directory name)
        name = get_name_from_subdir(subdir)
        # Key by subdirectory name so we can respect target ordering, store (label, results)
        data_to_plot[subdir.name] = (name, latency_results)
    
    if not data_to_plot:
        print(f"Error: No aggregate.out files found in subdirectories of '{results_dir}'", file=sys.stderr)
        sys.exit(1)
    
    # Create the plot
    plt.figure(figsize=(10, 6))
    
    # Determine plotting order:
    # - If targets were provided, follow their order (filtering out any that had no data)
    # - Otherwise, sort series by their display name alphabetically
    if targets is not None:
        ordered_keys = [t for t in targets if t in data_to_plot]
    else:
        ordered_keys = sorted(data_to_plot.keys(), key=lambda k: data_to_plot[k][0])

    plotted_load_levels = []
    for key in ordered_keys:
        name, results = data_to_plot[key]
        load_levels = sorted(results.keys())
        
        # Filter by min and max data points if specified
        if min_data_point is not None:
            load_levels = [level for level in load_levels if level >= min_data_point]
        if max_data_point is not None:
            load_levels = [level for level in load_levels if level <= max_data_point]
        
        if not load_levels:
            print(f"Warning: No data points in range for {name} after filtering", file=sys.stderr)
            continue
        
        plotted_load_levels.extend(load_levels)
        latencies = [results[level] for level in load_levels]
        plt.plot(load_levels, latencies, marker='o', label=name, linewidth=2)
    
    plt.xlabel('Load Level (requests per second)', fontsize=12)
    
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
    if clamp_x and plotted_load_levels:
        x_min, x_max = min(plotted_load_levels), max(plotted_load_levels)
        x_range = x_max - x_min
        pad = (x_range * 0.02) if x_range > 0 else 50  # 2% padding each side, or 50 if single point
        plt.xlim(x_min - pad, x_max + pad)
    else:
        plt.xlim(left=0)
    plt.ylim(bottom=0)
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
    if len(sys.argv) < 3:
        print("Usage: python3 plot_results.py <results_directory> <type> [output_filename] [min_data_point] [max_data_point] [--targets=dir1,dir2,...] [--clamp-x]", file=sys.stderr)
        print("  type: 'mean', 'p99', or 'max'", file=sys.stderr)
        print("  output_filename: optional custom output filename (default: based on type)", file=sys.stderr)
        print("  min_data_point: optional minimum load level to include (inclusive)", file=sys.stderr)
        print("  max_data_point: optional maximum load level to include (inclusive)", file=sys.stderr)
        print("  --targets=...: optional comma-separated list of subdirectory names to include", file=sys.stderr)
        print("  --clamp-x: set x-axis range to the min/max of plotted data points only", file=sys.stderr)
        sys.exit(1)
    
    results_dir = sys.argv[1]
    metric_type = sys.argv[2]
    
    # Parse optional arguments
    output_filename = None
    min_data_point = None
    max_data_point = None
    targets = None
    clamp_x = False

    # Separate flag-style args (e.g., --targets=..., --clamp-x) from positional optional args
    raw_optional_args = sys.argv[3:]
    positional_args = []
    for arg in raw_optional_args:
        if arg == "--clamp-x":
            clamp_x = True
        elif arg.startswith("--targets="):
            value = arg.split("=", 1)[1]
            if value:
                targets = [name for name in value.split(",") if name]
            else:
                targets = []
        else:
            positional_args.append(arg)

    # Now interpret positional_args the same way the script did before
    if len(positional_args) >= 1:
        if positional_args[0].isdigit():
            # First positional is a number: min_data_point
            min_data_point = int(positional_args[0])
        else:
            # First positional is a string: output_filename
            output_filename = positional_args[0]

    if len(positional_args) >= 2:
        if output_filename:
            # We already have output_filename, so second positional is min_data_point
            min_data_point = int(positional_args[1])
        else:
            # No output_filename, so second positional is max_data_point
            max_data_point = int(positional_args[1])

    if len(positional_args) >= 3:
        # Third positional is always max_data_point
        max_data_point = int(positional_args[2])
    
    plot_results(results_dir, metric_type, output_filename, min_data_point, max_data_point, targets, clamp_x)

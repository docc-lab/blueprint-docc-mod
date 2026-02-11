#!/usr/bin/env python3
"""
Aggregates results from multiple .run files in a directory.
Averages Mean, 99%, and Max metrics across all runs for each load level.
"""

import os
import re
import sys
from pathlib import Path
from collections import defaultdict


def parse_run_file(filepath):
    """Parse a .run file and extract metrics for each load level.
    Detects unit from the Latency line (ms or s) and normalizes all values to milliseconds.
    """
    results = {}
    
    with open(filepath, 'r') as f:
        content = f.read()
    
    # Split by load level sections (e.g., "50:", "100:", etc.)
    sections = re.split(r'^(\d+):$', content, flags=re.MULTILINE)
    
    # Process each section (sections[0] is content before first match, then pairs of (number, content))
    for i in range(1, len(sections), 2):
        if i + 1 >= len(sections):
            break
        
        load_level = int(sections[i])
        section_content = sections[i + 1]
        
        # Detect unit from Latency line (first value: ms or s); allow optional space before unit (e.g. "0.008 s")
        unit_match = re.search(r'Latency\s+[\d.]+\s*(ms|s)\s+', section_content, re.IGNORECASE)
        scale_to_ms = 1000.0 if (unit_match and unit_match.group(1).lower() == 's') else 1.0

        # Extract Mean from #[Mean = X, ...] (no unit in line; use section unit). If "s", convert to ms.
        mean_match = re.search(r'#\[Mean\s*=\s*([\d.]+)', section_content)
        mean = float(mean_match.group(1)) * scale_to_ms if mean_match else None

        # Extract 99% from Latency line (3rd value); accept ms or s (with optional space). If "s", convert to ms.
        latency_match = re.search(r'Latency\s+[\d.]+\s*(?:ms|s)\s+[\d.]+\s*(?:ms|s)\s+([\d.]+)\s*(ms|s)', section_content, re.IGNORECASE)
        if latency_match:
            p99 = float(latency_match.group(1)) * (1000.0 if latency_match.group(2).lower() == 's' else 1.0)
        else:
            p99 = None

        # Extract Max from #[Max = X, ...] (no unit in line; use section unit). If "s", convert to ms.
        max_match = re.search(r'#\[Max\s*=\s*([\d.]+)', section_content)
        max_val = float(max_match.group(1)) * scale_to_ms if max_match else None
        
        if mean is not None and p99 is not None and max_val is not None:
            results[load_level] = {
                'mean': mean,
                'p99': p99,
                'max': max_val
            }
    
    return results


def aggregate_results(directory):
    """Aggregate results from all .run files in the directory."""
    dir_path = Path(directory)
    
    # Find all .run files
    run_files = sorted(dir_path.glob('*.run'))
    
    if not run_files:
        print(f"No .run files found in {directory}", file=sys.stderr)
        return None
    
    print(f"Found {len(run_files)} .run files: {[f.name for f in run_files]}", file=sys.stderr)
    
    # Collect all results by load level
    all_results = defaultdict(list)
    
    for run_file in run_files:
        results = parse_run_file(run_file)
        for load_level, metrics in results.items():
            all_results[load_level].append(metrics)
    
    # Calculate averages for each load level
    aggregated = {}
    for load_level in sorted(all_results.keys()):
        metrics_list = all_results[load_level]
        aggregated[load_level] = {
            'mean': sum(m['mean'] for m in metrics_list) / len(metrics_list),
            'p99': sum(m['p99'] for m in metrics_list) / len(metrics_list),
            'max': sum(m['max'] for m in metrics_list) / len(metrics_list),
        }
    
    return aggregated


def format_output(aggregated):
    """Format aggregated results for output."""
    output_lines = []
    
    for load_level in sorted(aggregated.keys()):
        metrics = aggregated[load_level]
        output_lines.append(f"{load_level}:")
        output_lines.append(f"  Mean: {metrics['mean']:.3f}ms, 99%: {metrics['p99']:.2f}ms, Max: {metrics['max']:.3f}ms")
        output_lines.append("")
    
    return "\n".join(output_lines)


def main():
    if len(sys.argv) != 2:
        print(f"Usage: {sys.argv[0]} <directory>", file=sys.stderr)
        sys.exit(1)
    
    directory = sys.argv[1]
    
    if not os.path.isdir(directory):
        print(f"Error: {directory} is not a directory", file=sys.stderr)
        sys.exit(1)
    
    aggregated = aggregate_results(directory)
    
    if aggregated is None:
        sys.exit(1)
    
    # Write to aggregate.out in the same directory
    output_file = Path(directory) / "aggregate.out"
    output_content = format_output(aggregated)
    
    with open(output_file, 'w') as f:
        f.write(output_content)
    
    print(f"Results written to {output_file}", file=sys.stderr)
    print(output_content)


if __name__ == "__main__":
    main()


#!/usr/bin/env python3
"""
Visualize abstract workflows as trees.
"""

import json
import argparse
from pathlib import Path
from typing import List, Dict, Any


def visualize_workflow(workflow: Dict[str, Any], max_depth: int = None) -> str:
    """Convert workflow to a tree visualization string."""
    
    def format_span(span: Dict, depth: int = 0, prefix: str = "", max_depth_limit: int = None) -> List[str]:
        """Format a span and its children as a tree."""
        lines = []
        
        # Format this span
        span_info = f"{span['call_id']}: {span['service']}.{span['operation']}"
        if span.get('depends_on'):
            deps = ",".join(map(str, span['depends_on']))
            span_info += f" (depends: {deps})"
        
        lines.append(f"{prefix}{span_info}")
        
        # Format children
        if span.get('children') and (max_depth_limit is None or depth < max_depth_limit):
            for i, child in enumerate(span['children']):
                is_last = i == len(span['children']) - 1
                child_prefix = prefix + ("└─ " if is_last else "├─ ")
                child_lines = format_span(child, depth + 1, child_prefix, max_depth_limit)
                lines.extend(child_lines)
        
        return lines
    
    # Start with workflow info
    lines = [
        f"# Workflow: {workflow['workflow_id']}",
        f"Complexity Profile: {workflow['complexity_profile']['name']}",
        f"Target: height={workflow['complexity_profile']['height']}, length={workflow['complexity_profile']['length']}",
        "",
        "## Call Tree",
        ""
    ]
    
    # Format the root span and its children
    if workflow['calls']:
        root_span = workflow['calls'][0]
        tree_lines = format_span(root_span, max_depth_limit=max_depth)
        lines.extend(tree_lines)
    
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(description="Visualize abstract workflows")
    parser.add_argument("workflow_file", help="Path to workflow JSON file")
    parser.add_argument("--max-depth", type=int, help="Maximum depth to show")
    
    args = parser.parse_args()
    
    # Load workflow
    with open(args.workflow_file, 'r') as f:
        workflow = json.load(f)
    
    # Generate visualization
    visualization = visualize_workflow(workflow, max_depth=args.max_depth)
    print(visualization)


if __name__ == "__main__":
    main() 
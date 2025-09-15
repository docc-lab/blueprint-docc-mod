#!/usr/bin/env python3
"""
Workflow to Tree Converter

Converts workflow JSON files into tree structures for markdown display.
Shows call hierarchies, dependencies, and business purposes.
"""

import json
import sys
import os
from typing import Dict, List, Any


def load_workflow(json_file: str) -> Dict[str, Any]:
    """Load workflow from JSON file."""
    try:
        with open(json_file, 'r') as f:
            return json.load(f)
    except FileNotFoundError:
        print(f"Error: File '{json_file}' not found.")
        sys.exit(1)
    except json.JSONDecodeError as e:
        print(f"Error: Invalid JSON in '{json_file}': {e}")
        sys.exit(1)


def format_call(call: Dict[str, Any], level: int = 0, is_last: bool = False, parent_prefix: str = "") -> List[str]:
    """Format a single call into tree representation."""
    lines = []
    
    # Main call line
    call_id = call.get('call_id', '?')
    service = call.get('service', 'Unknown Service')
    operation = call.get('operation', 'unknown_operation')
    depends_on = call.get('depends_on', [])
    business_purpose = call.get('business_purpose', 'No description')
    
    # Calculate call depth (nesting level) - starting from 1
    call_depth = level + 1
    
    # Format dependency info
    deps_str = f" (depends on: {depends_on})" if depends_on else ""
    
    # Determine prefix for this level
    if level == 0:
        prefix = "└─" if is_last else "├─"
    else:
        prefix = "└─" if is_last else "├─"
    
    # Main call line with depth info
    lines.append(f"{parent_prefix}{prefix} [{call_id}] {service}.{operation} (depth: {call_depth}){deps_str}")
    
    # Business purpose removed from tree for cleaner display
    
    # Children
    children = call.get('children', [])
    for i, child in enumerate(children):
        child_is_last = i == len(children) - 1
        
        # Determine child prefix
        if level == 0:
            child_parent_prefix = "   " if is_last else "│  "
        else:
            child_parent_prefix = parent_prefix + ("   " if is_last else "│  ")
        
        child_lines = format_call(child, level + 1, child_is_last, child_parent_prefix)
        lines.extend(child_lines)
    
    return lines


def workflow_to_tree(workflow: Dict[str, Any]) -> str:
    """Convert workflow to tree structure."""
    lines = []
    
    # Header
    workflow_id = workflow.get('workflow_id', 'unknown')
    endpoint = workflow.get('endpoint', 'unknown')
    description = workflow.get('description', 'No description')
    business_purpose = workflow.get('business_purpose', 'No business purpose')
    
    lines.append(f"# Workflow: {workflow_id}")
    lines.append("")
    lines.append(f"**Endpoint:** `{endpoint}`")
    lines.append(f"**Description:** {description}")
    lines.append(f"**Business Purpose:** {business_purpose}")
    lines.append("")
    lines.append("## Call Tree")
    lines.append("")
    lines.append("```")
    
    # Tree structure
    calls = workflow.get('calls', [])
    if not calls:
        lines.append("*No calls defined*")
    else:
        for i, call in enumerate(calls):
            is_last = i == len(calls) - 1
            call_lines = format_call(call, 0, is_last, "")
            lines.extend(call_lines)
    
    lines.append("```")
    
    return "\n".join(lines)


def main():
    """Main function."""
    if len(sys.argv) != 2:
        print("Usage: python workflow_to_tree.py <workflow.json>")
        sys.exit(1)
    
    json_file = sys.argv[1]
    workflow = load_workflow(json_file)
    
    tree = workflow_to_tree(workflow)
    print(tree)


if __name__ == "__main__":
    main() 
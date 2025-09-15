#!/usr/bin/env python3
"""
Visualize mapped workflows with business service names and operations.
"""

import json
import argparse
from pathlib import Path
from typing import Dict, List, Any


def visualize_mapped_workflow(workflow: Dict[str, Any], max_depth: int = None) -> str:
    """Visualize a mapped workflow as a tree structure."""
    
    workflow_id = workflow.get("workflow_id", "unknown")
    business_context = workflow.get("business_context", "Unknown Business Context")
    endpoint = workflow.get("endpoint", "Unknown Endpoint")
    
    output = []
    output.append(f"# Mapped Workflow: {workflow_id}")
    output.append(f"Business Context: {business_context}")
    output.append(f"Endpoint: {endpoint}")
    output.append("")
    
    calls = workflow.get("calls", [])
    if not calls:
        output.append("No calls found in workflow.")
        return "\n".join(output)
    
    # Visualize the call tree
    output.append("## Business Service Call Tree")
    output.append("")
    
    def print_call_tree(calls: List[Dict], prefix: str = "", depth: int = 0):
        """Recursively print the call tree."""
        if max_depth and depth > max_depth:
            return
        
        for i, call in enumerate(calls):
            is_last = i == len(calls) - 1
            current_prefix = "└─ " if is_last else "├─ "
            
            # Format the call information
            call_id = call.get("call_id", "?")
            service = call.get("service", "UnknownService")
            operation = call.get("operation", "unknownOperation")
            depends_on = call.get("depends_on", [])
            business_purpose = call.get("business_purpose", "Unknown purpose")
            
            # Create the call line
            call_line = f"{prefix}{current_prefix}{call_id}: {service}.{operation}"
            if depends_on:
                call_line += f" (depends: {','.join(map(str, depends_on))})"
            
            output.append(call_line)
            
            # Add business purpose as comment
            if business_purpose and business_purpose != "Unknown purpose":
                purpose_line = f"{prefix}{'   ' if is_last else '│  '}   # {business_purpose}"
                output.append(purpose_line)
            
            # Recursively print children
            children = call.get("children", [])
            if children:
                child_prefix = prefix + ("    " if is_last else "│   ")
                print_call_tree(children, child_prefix, depth + 1)
    
    print_call_tree(calls)
    
    # Add summary statistics
    output.append("")
    output.append("## Summary")
    
    def count_services(calls: List[Dict]) -> Dict[str, int]:
        """Count service usage."""
        service_counts = {}
        for call in calls:
            service = call.get("service", "UnknownService")
            service_counts[service] = service_counts.get(service, 0) + 1
            if call.get("children"):
                child_counts = count_services(call["children"])
                for child_service, count in child_counts.items():
                    service_counts[child_service] = service_counts.get(child_service, 0) + count
        return service_counts
    
    service_counts = count_services(calls)
    output.append(f"Total unique services: {len(service_counts)}")
    output.append(f"Total calls: {sum(service_counts.values())}")
    
    # Show service breakdown
    output.append("")
    output.append("### Service Usage:")
    for service, count in sorted(service_counts.items()):
        output.append(f"  {service}: {count} calls")
    
    return "\n".join(output)


def main():
    parser = argparse.ArgumentParser(description="Visualize mapped workflows")
    parser.add_argument("workflow_file", help="Path to the mapped workflow JSON file")
    parser.add_argument("--max-depth", type=int, help="Maximum depth to display")
    
    args = parser.parse_args()
    
    workflow_file = Path(args.workflow_file)
    if not workflow_file.exists():
        print(f"Error: Workflow file {workflow_file} does not exist")
        return
    
    with open(workflow_file, 'r') as f:
        workflow = json.load(f)
    
    visualization = visualize_mapped_workflow(workflow, args.max_depth)
    print(visualization)


if __name__ == "__main__":
    main() 
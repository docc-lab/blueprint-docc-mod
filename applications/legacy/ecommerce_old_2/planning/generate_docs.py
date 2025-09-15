#!/usr/bin/env python3
"""
Workflow Documentation Generator

Generates markdown documentation files from workflow JSON files.
Flexible tool that can process workflows from any input directory and output to any specified directory.
"""

import os
import sys
import glob
from workflow_to_tree import load_workflow, workflow_to_tree


def generate_docs(input_dir: str = ".", output_dir: str = "docs"):
    """Generate markdown documentation for all workflow JSON files in input directory."""
    
    # Create output directory if it doesn't exist
    os.makedirs(output_dir, exist_ok=True)
    
    # Find all workflow JSON files
    pattern = os.path.join(input_dir, "*.json")
    json_files = glob.glob(pattern)
    
    # Filter for workflow files (containing workflow_id field)
    workflow_files = []
    for json_file in json_files:
        try:
            workflow = load_workflow(json_file)
            if 'workflow_id' in workflow:
                workflow_files.append(json_file)
        except Exception:
            # Skip files that aren't valid workflow JSON
            continue
    
    if not workflow_files:
        print(f"No workflow files found in: {input_dir}")
        return
    
    print(f"Found {len(workflow_files)} workflow files:")
    
    for json_file in workflow_files:
        # Generate output filename
        base_name = os.path.splitext(os.path.basename(json_file))[0]
        md_file = os.path.join(output_dir, f"{base_name}.md")
        
        print(f"  Processing: {json_file} -> {md_file}")
        
        try:
            # Load and convert workflow
            workflow = load_workflow(json_file)
            tree_content = workflow_to_tree(workflow)
            
            # Write to markdown file
            with open(md_file, 'w') as f:
                f.write(tree_content)
                f.write("\n")
            
            print(f"  ✓ Generated: {md_file}")
            
        except Exception as e:
            print(f"  ✗ Error processing {json_file}: {e}")
    
    # Generate index file
    generate_index(output_dir, workflow_files)


def generate_index(output_dir: str, workflow_files: list):
    """Generate an index markdown file listing all workflows."""
    
    index_file = os.path.join(output_dir, "README.md")
    
    lines = []
    lines.append("# Workflow Documentation")
    lines.append("")
    lines.append("This directory contains tree visualizations of workflow JSON files.")
    lines.append("")
    lines.append("## Available Workflows")
    lines.append("")
    
    for json_file in workflow_files:
        base_name = os.path.splitext(os.path.basename(json_file))[0]
        md_file = f"{base_name}.md"
        
        try:
            workflow = load_workflow(json_file)
            workflow_id = workflow.get('workflow_id', 'unknown')
            endpoint = workflow.get('endpoint', 'unknown')
            description = workflow.get('description', 'No description')
            
            lines.append(f"- **[{workflow_id}]({md_file})** - `{endpoint}`")
            lines.append(f"  - {description}")
            lines.append("")
            
        except Exception as e:
            lines.append(f"- **{base_name}** - Error loading metadata: {e}")
            lines.append("")
    
    lines.append("## Usage")
    lines.append("")
    lines.append("To regenerate these documents, run:")
    lines.append("```bash")
    lines.append("python generate_docs.py [input_dir] [output_dir]")
    lines.append("```")
    lines.append("")
    lines.append("## Workflow Format")
    lines.append("")
    lines.append("These workflows follow the JSON format defined in `workflow_format.md`.")
    lines.append("Each workflow shows the service call hierarchy and dependencies.")
    
    with open(index_file, 'w') as f:
        f.write("\n".join(lines))
    
    print(f"✓ Generated index: {index_file}")


def main():
    """Main function."""
    if len(sys.argv) != 3:
        print("Usage: python generate_docs.py <input_directory> <output_directory>")
        print("")
        print("Examples:")
        print("  python generate_docs.py . docs")
        print("  python generate_docs.py workflows/ documentation/")
        print("  python generate_docs.py examples/ docs/examples/")
        sys.exit(1)
    
    input_dir = sys.argv[1]
    output_dir = sys.argv[2]
    
    # Validate input directory exists
    if not os.path.exists(input_dir):
        print(f"Error: Input directory '{input_dir}' does not exist.")
        sys.exit(1)
    
    print(f"Processing workflows from: {input_dir}")
    print(f"Output directory: {output_dir}")
    print()
    
    generate_docs(input_dir, output_dir)


if __name__ == "__main__":
    main() 
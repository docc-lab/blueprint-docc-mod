#!/usr/bin/env python3
"""
Documentation generator for microservice architecture.
Reads services.json and service-graph.dot, produces markdown with service list and SVG diagram.
"""

import json
import subprocess
import sys
import os
from pathlib import Path

def load_services(services_file):
    """Load services from JSON file."""
    with open(services_file, 'r') as f:
        return json.load(f)

def generate_svg_from_dot(dot_file, svg_file):
    """Generate SVG from DOT file using graphviz."""
    try:
        result = subprocess.run(
            ['dot', '-Tsvg', dot_file, '-o', svg_file],
            capture_output=True,
            text=True,
            check=True
        )
        return True
    except subprocess.CalledProcessError as e:
        print(f"Error generating SVG: {e}")
        print(f"stdout: {e.stdout}")
        print(f"stderr: {e.stderr}")
        return False
    except FileNotFoundError:
        print("Error: 'dot' command not found. Please install Graphviz.")
        return False

def count_services(services_data):
    """Count total number of services across all domains."""
    total = 0
    for domain_data in services_data.values():
        if 'services' in domain_data:
            total += len(domain_data['services'])
    return total

def generate_markdown(services_data, svg_file, output_file):
    """Generate markdown documentation."""
    total_services = count_services(services_data)
    
    with open(output_file, 'w') as f:
        f.write("# Microservice Architecture Documentation\n\n")
        f.write(f"**Total Services:** {total_services}\n\n")
        
        # Service catalog
        f.write("## Service Catalog\n\n")
        
        for domain, domain_data in services_data.items():
            f.write(f"### {domain.replace('_', ' ').title()}\n")
            f.write(f"*{domain_data['description']}*\n\n")
            
            if 'services' in domain_data:
                for service in domain_data['services']:
                    f.write(f"- **{service}**\n")
                f.write("\n")
        
        # Service graph visualization
        f.write("## Service Dependency Graph\n\n")
        
        if os.path.exists(svg_file):
            # Link to SVG file
            svg_filename = os.path.basename(svg_file)
            f.write(f"![Service Graph]({svg_filename})\n\n")
        else:
            f.write(f"*SVG diagram not available: {svg_file} not found*\n\n")
        
        # Architecture notes
        f.write("## Architecture Notes\n\n")
        f.write("- **Frontend**: UI-only layer with no direct internal service calls\n")
        f.write("- **External APIs**: Dedicated root services owning public endpoints\n")
        f.write("- **Payment Flow**: Split into Authorization → Settlement for traceability\n")
        f.write("- **Scope**: Synchronous calls only; databases/caching/async patterns excluded\n")

def main():
    if len(sys.argv) != 4:
        print("Usage: python generate_docs.py <services.json> <service-graph.dot> <output.md>")
        sys.exit(1)
    
    services_file = sys.argv[1]
    dot_file = sys.argv[2]
    output_file = sys.argv[3]
    
    # Check input files exist
    if not os.path.exists(services_file):
        print(f"Error: {services_file} not found")
        sys.exit(1)
    
    if not os.path.exists(dot_file):
        print(f"Error: {dot_file} not found")
        sys.exit(1)
    
    # Generate SVG
    svg_file = Path(dot_file).with_suffix('.svg')
    print(f"Generating SVG diagram: {svg_file}")
    
    if not generate_svg_from_dot(dot_file, svg_file):
        print("Warning: Could not generate SVG diagram")
    
    # Load services
    print(f"Loading services from: {services_file}")
    services_data = load_services(services_file)
    
    # Generate documentation
    print(f"Generating documentation: {output_file}")
    generate_markdown(services_data, svg_file, output_file)
    
    print("Documentation generated successfully!")

if __name__ == "__main__":
    main() 
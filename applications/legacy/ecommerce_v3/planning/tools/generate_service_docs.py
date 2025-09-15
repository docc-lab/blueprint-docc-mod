#!/usr/bin/env python3
"""
Generate service documentation from service graph and dotfile.

This script creates a README with service graph visualization
and detailed service information.
"""

import json
import argparse
import subprocess
from pathlib import Path
from typing import Dict, List, Any


def load_service_graph(service_graph_path: Path) -> Dict[str, Any]:
    """Load the service graph JSON file."""
    with open(service_graph_path, 'r') as f:
        return json.load(f)


def load_dotfile(dotfile_path: Path) -> str:
    """Load the dotfile content."""
    with open(dotfile_path, 'r') as f:
        return f.read()


def generate_svg_from_dot(dotfile_path: Path, svg_path: Path) -> bool:
    """Generate SVG from dotfile using Graphviz."""
    try:
        result = subprocess.run(['dot', '-Tsvg', str(dotfile_path), '-o', str(svg_path)], 
                              capture_output=True, text=True)
        if result.returncode == 0:
            return True
        else:
            print(f"Warning: Failed to generate SVG: {result.stderr}")
            return False
    except FileNotFoundError:
        print("Warning: Graphviz 'dot' command not found. Install Graphviz to generate SVG diagrams.")
        return False


def generate_service_table(services: Dict[str, Any]) -> str:
    """Generate a markdown table of all services."""
    table = "| Service | Category | Description | Endpoints |\n"
    table += "|---------|----------|-------------|-----------|\n"
    
    for service_name, service_info in services.items():
        category = service_info.get("category", "unknown")
        description = service_info.get("description", "")
        endpoints = service_info.get("endpoints", [])
        
        # Truncate description if too long
        if len(description) > 50:
            description = description[:47] + "..."
        
        # Format endpoints
        endpoint_str = ", ".join(endpoints[:2])  # Show first 2 endpoints
        if len(endpoints) > 2:
            endpoint_str += f" (+{len(endpoints)-2} more)"
        
        table += f"| {service_name} | {category} | {description} | {endpoint_str} |\n"
    
    return table


def generate_category_summary(services: Dict[str, Any], categories: Dict[str, str]) -> str:
    """Generate a summary of services by category."""
    summary = "## Service Categories\n\n"
    
    for category_name, category_desc in categories.items():
        category_services = [name for name, info in services.items() 
                           if info.get("category") == category_name]
        
        if category_services:
            summary += f"### {category_desc}\n\n"
            summary += f"**Services:** {', '.join(category_services)}\n\n"
            summary += f"**Count:** {len(category_services)} services\n\n"
    
    return summary


def generate_workflow_templates_section(workflow_templates: Dict[str, Any]) -> str:
    """Generate documentation for workflow templates."""
    section = "## Workflow Templates\n\n"
    
    for template_name, template_info in workflow_templates.items():
        section += f"### {template_name.replace('_', ' ').title()}\n\n"
        section += f"**Description:** {template_info['description']}\n\n"
        section += f"**Root Service:** {template_info['root_service']}\n\n"
        section += f"**Required Services:** {', '.join(template_info['required_services'])}\n\n"
        section += f"**Optional Services:** {', '.join(template_info['optional_services'])}\n\n"
    
    return section


def generate_dependency_analysis(services: Dict[str, Any]) -> str:
    """Generate dependency analysis section."""
    section = "## Dependency Analysis\n\n"
    
    # Find services with most dependencies
    dependency_counts = {}
    for service_name, service_info in services.items():
        dependencies = service_info.get("dependencies", [])
        dependency_counts[service_name] = len(dependencies)
    
    # Sort by dependency count
    sorted_services = sorted(dependency_counts.items(), key=lambda x: x[1], reverse=True)
    
    section += "### Services by Dependency Count\n\n"
    section += "| Service | Dependencies |\n"
    section += "|---------|-------------|\n"
    
    for service_name, count in sorted_services[:10]:  # Top 10
        section += f"| {service_name} | {count} |\n"
    
    section += "\n### Most Dependent Services\n\n"
    most_dependent = [name for name, count in sorted_services if count > 0][:5]
    section += f"These services have the most dependencies: {', '.join(most_dependent)}\n\n"
    
    # Find leaf services (no dependencies)
    leaf_services = [name for name, count in sorted_services if count == 0]
    section += "### Leaf Services\n\n"
    section += f"These services have no dependencies: {', '.join(leaf_services)}\n\n"
    
    return section


def generate_readme(service_graph: Dict[str, Any], dotfile_path: Path, output_path: Path):
    """Generate the complete README file."""
    
    services = service_graph["services"]
    categories = service_graph["categories"]
    workflow_templates = service_graph["workflow_templates"]
    
    # Generate SVG from dotfile
    svg_path = Path("service_graph.svg")
    svg_generated = generate_svg_from_dot(dotfile_path, svg_path)
    
    # Create visualization section
    if svg_generated and svg_path.exists():
        visualization = f"""## Service Graph Visualization

![Service Graph]({svg_path.name})

The service graph shows the hierarchical relationships between services in our e-commerce microservice architecture."""
    else:
        # Fallback to showing the dotfile content
        dotfile_content = load_dotfile(dotfile_path)
        visualization = f"""## Service Graph Visualization

The service graph shows the relationships between services in our e-commerce microservice architecture.

```dot
{dotfile_content}
```

*Note: Install Graphviz to generate a visual diagram from the dotfile above.*"""
    
    readme_content = f"""# {service_graph['name']}

{service_graph['description']}

{visualization}

## Service Overview

The service graph contains **{len(services)} services** organized into **{len(categories)} categories**.

{generate_service_table(services)}

{generate_category_summary(services, categories)}

{generate_dependency_analysis(services)}

{generate_workflow_templates_section(workflow_templates)}

## Usage

This service graph can be used to:

1. **Generate realistic workflows** by traversing service dependencies
2. **Analyze service relationships** and identify coupling points
3. **Plan microservice architecture** with proper service boundaries
4. **Create distributed tracing scenarios** for research and testing

## Service Graph Structure

- **Frontend Services**: HTTP API gateway handling external requests
- **Core Services**: Main business logic services (OrderService, CustomerService, etc.)
- **Product Services**: Product catalog, inventory, and pricing services
- **Customer Services**: User management and profile services

## Workflow Generation

The service graph supports workflow generation through:

- **Template-based generation**: Use predefined workflow templates
- **Dependency traversal**: Follow service dependencies to create realistic call chains
- **Complexity profiles**: Control workflow depth and breadth
- **Business context**: Ensure workflows represent realistic business processes

"""
    
    with open(output_path, 'w') as f:
        f.write(readme_content)


def main():
    parser = argparse.ArgumentParser(description="Generate service documentation")
    parser.add_argument("--service-graph", default="service_graph.json", help="Path to service graph JSON file")
    parser.add_argument("--dotfile", default="service_graph.dot", help="Path to dotfile")
    parser.add_argument("--output", default="SERVICE_GRAPH.md", help="Output README file")
    
    args = parser.parse_args()
    
    # Load files
    service_graph_path = Path(args.service_graph)
    dotfile_path = Path(args.dotfile)
    output_path = Path(args.output)
    
    if not service_graph_path.exists():
        print(f"Error: Service graph file {service_graph_path} does not exist")
        return
    
    if not dotfile_path.exists():
        print(f"Error: Dotfile {dotfile_path} does not exist")
        return
    
    # Load data
    service_graph = load_service_graph(service_graph_path)
    
    # Generate README
    generate_readme(service_graph, dotfile_path, output_path)
    
    print(f"Generated service documentation: {output_path}")
    print(f"Services: {len(service_graph['services'])}")
    print(f"Categories: {len(service_graph['categories'])}")
    print(f"Workflow Templates: {len(service_graph['workflow_templates'])}")


if __name__ == "__main__":
    main() 
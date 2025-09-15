#!/usr/bin/env python3
"""
Graph depth analysis script for microservice architecture.
Calculates average in-degree at each depth level from a DOT file.
"""

import re
import sys
from collections import defaultdict, deque

def parse_dot_file(dot_file):
    """Parse DOT file and extract directed edges."""
    edges = []
    nodes = set()
    
    with open(dot_file, 'r') as f:
        content = f.read()
    
    # Find all edges in format "source" -> "target";
    edge_pattern = r'"([^"]+)"\s*->\s*"([^"]+)"'
    matches = re.findall(edge_pattern, content)
    
    for source, target in matches:
        edges.append((source, target))
        nodes.add(source)
        nodes.add(target)
    
    # Also find standalone nodes (no edges)
    standalone_pattern = r'"([^"]+)";'
    standalone_matches = re.findall(standalone_pattern, content)
    for node in standalone_matches:
        nodes.add(node)
    
    return list(nodes), edges

def build_graph(nodes, edges):
    """Build adjacency list and in-degree count."""
    graph = defaultdict(list)  # adjacency list
    in_degree = defaultdict(int)  # in-degree count
    
    # Initialize all nodes with 0 in-degree
    for node in nodes:
        in_degree[node] = 0
    
    # Build graph and count in-degrees
    for source, target in edges:
        graph[source].append(target)
        in_degree[target] += 1
    
    return graph, in_degree

def find_root_nodes(nodes, in_degree):
    """Find nodes with in-degree 0 (roots)."""
    return [node for node in nodes if in_degree[node] == 0]

def calculate_depths_longest_path(graph, roots):
    """Calculate maximum depth of each node (longest path from any root)."""
    depths = {}
    
    # Start BFS from all root nodes, but track maximum depth
    queue = deque()
    for root in roots:
        queue.append((root, 0))
        depths[root] = 0
    
    while queue:
        node, depth = queue.popleft()
        
        # Visit all neighbors
        for neighbor in graph[node]:
            # Update depth if we haven't seen this node OR found a longer path
            if neighbor not in depths or depths[neighbor] < depth + 1:
                depths[neighbor] = depth + 1
                queue.append((neighbor, depth + 1))
    
    return depths

def analyze_in_degree_by_depth(nodes, edges):
    """Main analysis function."""
    graph, in_degree = build_graph(nodes, edges)
    roots = find_root_nodes(nodes, in_degree)
    
    if not roots:
        print("Warning: No root nodes found (all nodes have incoming edges)")
        # If no roots, pick nodes with minimum in-degree as starting points
        min_in_degree = min(in_degree.values())
        roots = [node for node in nodes if in_degree[node] == min_in_degree]
        print(f"Using nodes with minimum in-degree ({min_in_degree}) as roots: {roots}")
    
    depths = calculate_depths_longest_path(graph, roots)
    
    # Group nodes by depth and calculate average in-degree
    depth_groups = defaultdict(list)
    for node in nodes:
        if node in depths:
            depth_groups[depths[node]].append(node)
    
    results = []
    for depth in sorted(depth_groups.keys()):
        nodes_at_depth = depth_groups[depth]
        in_degrees_at_depth = [in_degree[node] for node in nodes_at_depth]
        avg_in_degree = sum(in_degrees_at_depth) / len(in_degrees_at_depth)
        
        results.append({
            'depth': depth,
            'node_count': len(nodes_at_depth),
            'avg_in_degree': avg_in_degree,
            'nodes': nodes_at_depth
        })
    
    return results, roots

def main():
    if len(sys.argv) != 2:
        print("Usage: python analyze_graph_depth.py <service-graph.dot>")
        sys.exit(1)
    
    dot_file = sys.argv[1]
    
    try:
        nodes, edges = parse_dot_file(dot_file)
        print(f"Parsed {len(nodes)} nodes and {len(edges)} edges from {dot_file}")
        
        results, roots = analyze_in_degree_by_depth(nodes, edges)
        
        print(f"\nRoot nodes (in-degree 0): {roots}")
        print("\nAverage In-Degree by Depth Level:")
        print("=" * 50)
        print(f"{'Depth':<6} {'Nodes':<6} {'Avg In-Degree':<15} {'Sample Nodes'}")
        print("-" * 50)
        
        for result in results:
            sample_nodes = ', '.join(result['nodes'][:3])  # Show first 3 nodes
            if len(result['nodes']) > 3:
                sample_nodes += f" (+ {len(result['nodes']) - 3} more)"
            
            print(f"{result['depth']:<6} {result['node_count']:<6} {result['avg_in_degree']:<15.2f} {sample_nodes}")
        
        # Summary statistics
        print("\nSummary:")
        print(f"Max depth: {max(r['depth'] for r in results)}")
        print(f"Total nodes: {sum(r['node_count'] for r in results)}")
        overall_avg = sum(r['avg_in_degree'] * r['node_count'] for r in results) / sum(r['node_count'] for r in results)
        print(f"Overall average in-degree: {overall_avg:.2f}")
        
    except FileNotFoundError:
        print(f"Error: File {dot_file} not found")
        sys.exit(1)
    except Exception as e:
        print(f"Error: {e}")
        sys.exit(1)

if __name__ == "__main__":
    main() 
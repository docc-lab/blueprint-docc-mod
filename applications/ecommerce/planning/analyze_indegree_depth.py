#!/usr/bin/env python3
"""
In-degree depth analysis script for microservice architecture.
Groups nodes by in-degree and calculates average max-depth for each in-degree bucket.
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

def analyze_indegree_depth_buckets(nodes, edges):
    """Main analysis function - group by in-degree and calculate avg depth."""
    graph, in_degree = build_graph(nodes, edges)
    roots = find_root_nodes(nodes, in_degree)
    
    if not roots:
        print("Warning: No root nodes found (all nodes have incoming edges)")
        # If no roots, pick nodes with minimum in-degree as starting points
        min_in_degree = min(in_degree.values())
        roots = [node for node in nodes if in_degree[node] == min_in_degree]
        print(f"Using nodes with minimum in-degree ({min_in_degree}) as roots: {roots}")
    
    depths = calculate_depths_longest_path(graph, roots)
    
    # Group nodes by in-degree
    indegree_buckets = defaultdict(list)
    for node in nodes:
        if node in depths:  # Only include nodes reachable from roots
            indegree_buckets[in_degree[node]].append((node, depths[node]))
    
    # Calculate statistics for each in-degree bucket
    results = []
    for indegree in sorted(indegree_buckets.keys()):
        nodes_and_depths = indegree_buckets[indegree]
        node_count = len(nodes_and_depths)
        depths_list = [depth for node, depth in nodes_and_depths]
        avg_depth = sum(depths_list) / len(depths_list)
        max_depth = max(depths_list)
        min_depth = min(depths_list)
        
        # Sample nodes for display
        sample_nodes = [node for node, depth in nodes_and_depths[:3]]
        
        results.append({
            'in_degree': indegree,
            'node_count': node_count,
            'avg_depth': avg_depth,
            'max_depth': max_depth,
            'min_depth': min_depth,
            'sample_nodes': sample_nodes,
            'all_nodes': [node for node, depth in nodes_and_depths]
        })
    
    return results, roots

def main():
    if len(sys.argv) != 2:
        print("Usage: python analyze_indegree_depth.py <service-graph.dot>")
        sys.exit(1)
    
    dot_file = sys.argv[1]
    
    try:
        nodes, edges = parse_dot_file(dot_file)
        print(f"Parsed {len(nodes)} nodes and {len(edges)} edges from {dot_file}")
        
        results, roots = analyze_indegree_depth_buckets(nodes, edges)
        
        print(f"\nRoot nodes (in-degree 0): {roots}")
        print("\nIn-Degree Analysis: Average Max-Depth by In-Degree Bucket")
        print("=" * 70)
        print(f"{'In-Degree':<10} {'Nodes':<6} {'Avg Depth':<10} {'Max Depth':<10} {'Min Depth':<10} {'Sample Nodes'}")
        print("-" * 70)
        
        total_nodes = 0
        weighted_avg_depth = 0
        
        for result in results:
            sample_display = ', '.join(result['sample_nodes'][:2])
            if len(result['sample_nodes']) > 2:
                sample_display += f" (+ {result['node_count'] - 2} more)"
            
            print(f"{result['in_degree']:<10} {result['node_count']:<6} {result['avg_depth']:<10.2f} {result['max_depth']:<10} {result['min_depth']:<10} {sample_display}")
            
            total_nodes += result['node_count']
            weighted_avg_depth += result['avg_depth'] * result['node_count']
        
        # Summary statistics
        print("\nSummary:")
        print(f"Total analyzed nodes: {total_nodes}")
        print(f"Overall weighted average depth: {weighted_avg_depth / total_nodes:.2f}")
        print(f"In-degree range: {min(r['in_degree'] for r in results)} - {max(r['in_degree'] for r in results)}")
        
        # Show nodes with highest in-degree
        max_indegree = max(r['in_degree'] for r in results)
        max_indegree_nodes = [r for r in results if r['in_degree'] == max_indegree][0]
        print(f"Highest in-degree ({max_indegree}): {', '.join(max_indegree_nodes['all_nodes'])}")
        
    except FileNotFoundError:
        print(f"Error: File {dot_file} not found")
        sys.exit(1)
    except Exception as e:
        print(f"Error: {e}")
        sys.exit(1)

if __name__ == "__main__":
    main() 
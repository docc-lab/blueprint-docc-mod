#!/usr/bin/env python3
"""
Calculate complexity scores for generated workflows.

Complexity dimensions:
- Structural complexity (depth, width, branching)
- Call pattern complexity (sequential vs parallel, dependencies)
- Business logic complexity (service relationships, coordination)
"""

import json
import argparse
from typing import Dict, List, Any, Tuple
from pathlib import Path
import math


class WorkflowComplexityScorer:
    """Calculate various complexity metrics for workflows."""
    
    def __init__(self):
        self.metrics = {}
    
    def calculate_complexity_score(self, workflow: Dict[str, Any]) -> Dict[str, Any]:
        """Calculate comprehensive complexity metrics for a workflow."""
        
        # Extract calls from workflow
        calls = workflow.get("calls", [])
        
        # Calculate structural metrics
        structural_metrics = self._calculate_structural_complexity(calls)
        
        # Calculate call pattern metrics
        pattern_metrics = self._calculate_call_pattern_complexity(calls)
        
        # Calculate business logic complexity
        business_metrics = self._calculate_business_complexity(calls)
        
        # Calculate overall complexity score
        overall_score = self._calculate_overall_score(structural_metrics, pattern_metrics, business_metrics)
        
        return {
            "workflow_id": workflow.get("workflow_id", "unknown"),
            "overall_score": overall_score,
            "structural_complexity": structural_metrics,
            "call_pattern_complexity": pattern_metrics,
            "business_logic_complexity": business_metrics,
            "complexity_profile": workflow.get("complexity_profile", {})
        }
    
    def _calculate_structural_complexity(self, calls: List[Dict]) -> Dict[str, float]:
        """Calculate structural complexity metrics."""
        
        def analyze_structure(calls: List[Dict], depth: int = 1) -> Tuple[int, int, int, int, Dict[int, int]]:
            """Recursively analyze structure."""
            total_spans = len(calls)
            max_depth = depth
            max_width = len(calls)
            total_branches = 0
            depth_distribution = {depth: len(calls)}
            
            for call in calls:
                if call.get("children"):
                    child_spans, child_depth, child_width, child_branches, child_dist = analyze_structure(
                        call["children"], depth + 1
                    )
                    total_spans += child_spans
                    max_depth = max(max_depth, child_depth)
                    max_width = max(max_width, child_width)
                    total_branches += child_branches + 1
                    
                    # Merge depth distribution
                    for d, count in child_dist.items():
                        depth_distribution[d] = depth_distribution.get(d, 0) + count
            
            return total_spans, max_depth, max_width, total_branches, depth_distribution
        
        total_spans, max_depth, max_width, total_branches, depth_dist = analyze_structure(calls)
        
        # Calculate structural complexity score
        depth_complexity = max_depth / 10.0  # Normalize to 0-1
        width_complexity = max_width / 10.0   # Normalize to 0-1
        span_complexity = min(1.0, total_spans / 50.0)  # Normalize to 0-1
        branch_complexity = min(1.0, total_branches / 20.0)  # Normalize to 0-1
        
        # Calculate depth distribution variance
        depths = list(depth_dist.keys())
        if len(depths) > 1:
            depth_variance = (max(depths) - min(depths)) / 10.0
        else:
            depth_variance = 0.0
        
        structural_score = (depth_complexity + width_complexity + span_complexity + branch_complexity + depth_variance) / 5.0
        
        return {
            "score": structural_score,
            "max_depth": max_depth,
            "max_width": max_width,
            "total_spans": total_spans,
            "total_branches": total_branches,
            "depth_distribution": depth_dist,
            "depth_variance": depth_variance,
            "depth_complexity": depth_complexity,
            "width_complexity": width_complexity,
            "span_complexity": span_complexity,
            "branch_complexity": branch_complexity
        }
    
    def _calculate_call_pattern_complexity(self, calls: List[Dict]) -> Dict[str, float]:
        """Calculate call pattern complexity metrics."""
        
        def analyze_patterns(calls: List[Dict]) -> Tuple[int, int, int, int, int]:
            """Analyze call patterns."""
            sequential_chains = 0
            parallel_groups = 0
            cross_dependencies = 0
            total_dependencies = 0
            max_parallel = 0
            
            for call in calls:
                # Count dependencies
                dependencies = len(call.get("depends_on", []))
                total_dependencies += dependencies
                
                # Cross-dependencies (dependencies on siblings)
                if dependencies > 1:
                    cross_dependencies += 1
                
                # Analyze children
                if call.get("children"):
                    child_sequential, child_parallel, child_cross, child_deps, child_max_parallel = analyze_patterns(call["children"])
                    sequential_chains += child_sequential
                    parallel_groups += child_parallel
                    cross_dependencies += child_cross
                    total_dependencies += child_deps
                    max_parallel = max(max_parallel, child_max_parallel)
                
                # Check for sequential patterns (single child)
                if call.get("children") and len(call["children"]) == 1:
                    sequential_chains += 1
                
                # Check for parallel patterns (multiple children)
                if call.get("children") and len(call["children"]) > 1:
                    parallel_groups += 1
                    max_parallel = max(max_parallel, len(call["children"]))
            
            return sequential_chains, parallel_groups, cross_dependencies, total_dependencies, max_parallel
        
        seq_chains, par_groups, cross_deps, total_deps, max_par = analyze_patterns(calls)
        
        # Calculate pattern complexity scores
        sequential_complexity = min(1.0, seq_chains / 10.0)
        parallel_complexity = min(1.0, par_groups / 5.0)
        dependency_complexity = min(1.0, total_deps / 20.0)
        cross_dependency_complexity = min(1.0, cross_deps / 5.0)
        max_parallel_complexity = min(1.0, max_par / 8.0)
        
        pattern_score = (sequential_complexity + parallel_complexity + dependency_complexity + 
                        cross_dependency_complexity + max_parallel_complexity) / 5.0
        
        return {
            "score": pattern_score,
            "sequential_chains": seq_chains,
            "parallel_groups": par_groups,
            "cross_dependencies": cross_deps,
            "total_dependencies": total_deps,
            "max_parallel": max_par,
            "sequential_complexity": sequential_complexity,
            "parallel_complexity": parallel_complexity,
            "dependency_complexity": dependency_complexity,
            "cross_dependency_complexity": cross_dependency_complexity,
            "max_parallel_complexity": max_parallel_complexity
        }
    
    def _calculate_business_complexity(self, calls: List[Dict]) -> Dict[str, float]:
        """Calculate business logic complexity metrics."""
        
        def analyze_business_logic(calls: List[Dict], depth: int = 1) -> Tuple[int, int, int, int]:
            """Analyze business logic complexity."""
            coordination_points = 0
            validation_chains = 0
            processing_chains = 0
            integration_points = 0
            
            for call in calls:
                # Check for coordination (multiple dependencies)
                if len(call.get("depends_on", [])) > 1:
                    coordination_points += 1
                
                # Check for validation patterns (early in chain)
                if depth <= 2 and call.get("children"):
                    validation_chains += 1
                
                # Check for processing patterns (middle of chain)
                if 2 < depth <= 4 and call.get("children"):
                    processing_chains += 1
                
                # Check for integration patterns (deep in chain)
                if depth > 4:
                    integration_points += 1
                
                # Recursively analyze children
                if call.get("children"):
                    child_coord, child_valid, child_proc, child_integ = analyze_business_logic(call["children"], depth + 1)
                    coordination_points += child_coord
                    validation_chains += child_valid
                    processing_chains += child_proc
                    integration_points += child_integ
            
            return coordination_points, validation_chains, processing_chains, integration_points
        
        coord_points, valid_chains, proc_chains, integ_points = analyze_business_logic(calls)
        
        # Calculate business complexity scores
        coordination_complexity = min(1.0, coord_points / 5.0)
        validation_complexity = min(1.0, valid_chains / 3.0)
        processing_complexity = min(1.0, proc_chains / 3.0)
        integration_complexity = min(1.0, integ_points / 3.0)
        
        business_score = (coordination_complexity + validation_complexity + 
                         processing_complexity + integration_complexity) / 4.0
        
        return {
            "score": business_score,
            "coordination_points": coord_points,
            "validation_chains": valid_chains,
            "processing_chains": proc_chains,
            "integration_points": integ_points,
            "coordination_complexity": coordination_complexity,
            "validation_complexity": validation_complexity,
            "processing_complexity": processing_complexity,
            "integration_complexity": integration_complexity
        }
    
    def _calculate_overall_score(self, structural: Dict, pattern: Dict, business: Dict) -> float:
        """Calculate overall complexity score."""
        
        # Weight the different complexity dimensions
        structural_weight = 0.4
        pattern_weight = 0.35
        business_weight = 0.25
        
        overall_score = (
            structural["score"] * structural_weight +
            pattern["score"] * pattern_weight +
            business["score"] * business_weight
        )
        
        return round(overall_score, 3)
    
    def score_workflow_file(self, filepath: Path) -> Dict[str, Any]:
        """Score a workflow from a JSON file."""
        with open(filepath, 'r') as f:
            workflow = json.load(f)
        
        return self.calculate_complexity_score(workflow)
    
    def score_workflow_directory(self, directory: Path) -> List[Dict[str, Any]]:
        """Score all workflows in a directory."""
        results = []
        
        for filepath in directory.glob("*.json"):
            if filepath.name.startswith("abstract_"):
                result = self.score_workflow_file(filepath)
                results.append(result)
        
        # Sort by overall score
        results.sort(key=lambda x: x["overall_score"], reverse=True)
        
        return results


def print_complexity_report(results: List[Dict[str, Any]]):
    """Print a formatted complexity report."""
    
    print("=" * 80)
    print("WORKFLOW COMPLEXITY ANALYSIS")
    print("=" * 80)
    
    for i, result in enumerate(results, 1):
        print(f"\n{i}. {result['workflow_id']}")
        print(f"   Overall Score: {result['overall_score']:.3f}")
        
        # Structural metrics
        struct = result["structural_complexity"]
        print(f"   Structural: {struct['score']:.3f} (depth={struct['max_depth']}, width={struct['max_width']}, spans={struct['total_spans']})")
        
        # Pattern metrics
        pattern = result["call_pattern_complexity"]
        print(f"   Patterns: {pattern['score']:.3f} (seq={pattern['sequential_chains']}, par={pattern['parallel_groups']}, deps={pattern['total_dependencies']})")
        
        # Business metrics
        business = result["business_logic_complexity"]
        print(f"   Business: {business['score']:.3f} (coord={business['coordination_points']}, valid={business['validation_chains']}, proc={business['processing_chains']})")
        
        # Target profile
        profile = result.get("complexity_profile", {})
        if profile:
            print(f"   Target: height={profile.get('height', 'N/A')}, length={profile.get('length', 'N/A')}, variability={profile.get('branch_variability', 'N/A'):.2f}")
    
    print(f"\n{'='*80}")
    print(f"Analyzed {len(results)} workflows")
    
    if results:
        scores = [r["overall_score"] for r in results]
        print(f"Score range: {min(scores):.3f} - {max(scores):.3f}")
        print(f"Average score: {sum(scores)/len(scores):.3f}")


def main():
    parser = argparse.ArgumentParser(description="Calculate complexity scores for workflows")
    parser.add_argument("--workflow-dir", default="numbered_workflows", help="Directory containing workflow files")
    parser.add_argument("--workflow-file", help="Score a single workflow file")
    parser.add_argument("--output", help="Output file for detailed results")
    
    args = parser.parse_args()
    
    scorer = WorkflowComplexityScorer()
    
    if args.workflow_file:
        # Score single workflow
        result = scorer.score_workflow_file(Path(args.workflow_file))
        print_complexity_report([result])
    else:
        # Score all workflows in directory
        workflow_dir = Path(args.workflow_dir)
        if not workflow_dir.exists():
            print(f"Error: Directory {workflow_dir} does not exist")
            return
        
        results = scorer.score_workflow_directory(workflow_dir)
        print_complexity_report(results)
        
        # Save detailed results if requested
        if args.output:
            with open(args.output, 'w') as f:
                json.dump(results, f, indent=2)
            print(f"\nDetailed results saved to {args.output}")


if __name__ == "__main__":
    main() 
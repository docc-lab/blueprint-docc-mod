#!/usr/bin/env python3
"""
Generate abstract workflows with configurable complexity parameters.

Parameters:
- height: Maximum fanout (width) at any level
- length: Maximum chained call length (depth)
- depth_decay: How quickly the probability of continuing decreases with depth
- width_decay: How quickly the probability of wide fan-out decreases with depth
- branch_variability: How much branches vary from each other (0.0 = uniform, 1.0 = highly variable)
- deep_branch_probability: Probability that any given branch will be a "deep branch"

Each workflow is generated as a span tree with numbered spans.
"""

import json
import random
import argparse
from typing import List, Dict, Any, Tuple
from dataclasses import dataclass
from pathlib import Path


@dataclass
class ComplexityProfile:
    """Defines the complexity characteristics of a workflow."""
    height: int  # Maximum fanout at any level
    length: int  # Maximum chained call length
    depth_decay: float  # How quickly depth probability decays (0.1 = slow decay, 0.9 = fast decay)
    width_decay: float  # How quickly width probability decays (0.1 = slow decay, 0.9 = fast decay)
    branch_variability: float  # How much branches vary from each other (0.0 = uniform, 1.0 = highly variable)
    deep_branch_probability: float  # Probability that any given branch will be a "deep branch"
    name: str    # Descriptive name for the complexity profile


class AbstractWorkflowGenerator:
    """Generates abstract workflows with specified complexity parameters."""
    
    def __init__(self, seed: int = None):
        if seed:
            random.seed(seed)
        self.next_span_id = 0
    
    def generate_workflow(self, profile: ComplexityProfile) -> Dict[str, Any]:
        """Generate a single workflow with the specified complexity profile."""
        
        workflow = {
            "workflow_id": f"abstract_{profile.name.lower().replace(' ', '_')}",
            "complexity_profile": {
                "height": profile.height,
                "length": profile.length,
                "depth_decay": profile.depth_decay,
                "width_decay": profile.width_decay,
                "branch_variability": profile.branch_variability,
                "deep_branch_probability": profile.deep_branch_probability,
                "name": profile.name
            },
            "calls": []
        }
        
        # Reset span ID counter
        self.next_span_id = 0
        
        # Generate root span
        root_span = self._create_span("root", [], [])
        workflow["calls"].append(root_span)
        
        # Generate child spans based on complexity profile
        self._generate_children(root_span, profile, 1)
        
        return workflow
    
    def _create_span(self, operation: str, depends_on: List[int], children: List[Dict]) -> Dict[str, Any]:
        """Create a span with the specified properties."""
        span_id = self.next_span_id
        self.next_span_id += 1
        
        return {
            "call_id": span_id,
            "service": f"Service_{span_id}",
            "operation": operation,
            "depends_on": depends_on,
            "business_purpose": f"Abstract operation {span_id}",
            "children": children
        }
    
    def _generate_children(self, parent_span: Dict, profile: ComplexityProfile, current_depth: int):
        """Recursively generate child spans based on complexity profile with mixed asymmetry."""
        
        if current_depth > profile.length:
            return
        
        # Calculate base decayed probabilities
        base_depth_probability = max(0.05, 1.0 - (current_depth / profile.length) * profile.depth_decay)
        width_probability = max(0.1, 1.0 - (current_depth / profile.length) * profile.width_decay)
        
        # Determine how many children this span should have (with decay)
        max_possible_children = min(profile.height, random.randint(1, profile.height))
        actual_children = max(1, int(max_possible_children * width_probability))
        
        # Generate children with variable depth
        children = []
        for i in range(actual_children):
            # Determine if this child should continue growing (with branch variability)
            continue_probability = base_depth_probability
            
            # Apply branch variability - some branches will be very different from others
            if random.random() < profile.branch_variability:
                # This branch will have different characteristics
                if random.random() < profile.deep_branch_probability:
                    # This is a "deep branch" - much higher chance of continuing
                    continue_probability = min(0.95, continue_probability * 3)
                else:
                    # This is a "shallow branch" - much lower chance of continuing
                    continue_probability = max(0.02, continue_probability * 0.2)
            
            child_span = self._create_span(
                f"operation_{parent_span['call_id']}_{i}",
                [parent_span["call_id"]],  # Depend on parent
                []
            )
            
            # Add some cross-dependencies between siblings (realistic)
            if i > 0 and random.random() < 0.3:  # 30% chance of cross-dependency
                sibling_id = children[-1]["call_id"]
                child_span["depends_on"].append(sibling_id)
            
            children.append(child_span)
            
            # Recursively generate children for this child with variable probability
            if random.random() < continue_probability:
                self._generate_children(child_span, profile, current_depth + 1)
        
        parent_span["children"] = children
    
    def generate_workflow_variations(self, profiles: List[ComplexityProfile]) -> List[Dict[str, Any]]:
        """Generate multiple workflows with different complexity profiles."""
        workflows = []
        
        for profile in profiles:
            workflow = self.generate_workflow(profile)
            workflows.append(workflow)
        
        return workflows


def create_complexity_profiles() -> List[ComplexityProfile]:
    """Create a set of complexity profiles for testing."""
    return [
        ComplexityProfile(height=2, length=3, depth_decay=0.5, width_decay=0.3, branch_variability=0.2, deep_branch_probability=0.3, name="Simple"),
        ComplexityProfile(height=3, length=4, depth_decay=0.4, width_decay=0.2, branch_variability=0.4, deep_branch_probability=0.4, name="Medium"),
        ComplexityProfile(height=4, length=5, depth_decay=0.3, width_decay=0.15, branch_variability=0.6, deep_branch_probability=0.5, name="Complex"),
        ComplexityProfile(height=5, length=6, depth_decay=0.25, width_decay=0.1, branch_variability=0.7, deep_branch_probability=0.6, name="Very Complex"),
        ComplexityProfile(height=6, length=7, depth_decay=0.2, width_decay=0.08, branch_variability=0.8, deep_branch_probability=0.7, name="Extremely Complex"),
        # Add some asymmetric profiles
        ComplexityProfile(height=8, length=3, depth_decay=0.8, width_decay=0.1, branch_variability=0.3, deep_branch_probability=0.2, name="Wide Shallow"),
        ComplexityProfile(height=2, length=8, depth_decay=0.1, width_decay=0.8, branch_variability=0.9, deep_branch_probability=0.8, name="Narrow Deep"),
        ComplexityProfile(height=6, length=6, depth_decay=0.3, width_decay=0.2, branch_variability=0.5, deep_branch_probability=0.4, name="Balanced Complex"),
        # Add some extreme asymmetric profiles
        ComplexityProfile(height=4, length=6, depth_decay=0.1, width_decay=0.5, branch_variability=0.9, deep_branch_probability=0.8, name="Deep Asymmetric"),
        ComplexityProfile(height=6, length=4, depth_decay=0.6, width_decay=0.1, branch_variability=0.3, deep_branch_probability=0.2, name="Wide Asymmetric"),
        # Add mixed asymmetry profiles
        ComplexityProfile(height=4, length=5, depth_decay=0.3, width_decay=0.2, branch_variability=0.8, deep_branch_probability=0.6, name="Mixed Asymmetric"),
        ComplexityProfile(height=5, length=6, depth_decay=0.2, width_decay=0.15, branch_variability=0.9, deep_branch_probability=0.7, name="Highly Mixed"),
    ]


def analyze_workflow_complexity(workflow: Dict[str, Any]) -> Dict[str, Any]:
    """Analyze the actual complexity of a generated workflow."""
    
    def count_spans(calls: List[Dict]) -> int:
        total = len(calls)
        for call in calls:
            total += count_spans(call.get("children", []))
        return total
    
    def get_max_depth(calls: List[Dict], current_depth: int = 1) -> int:
        max_depth = current_depth
        for call in calls:
            if call.get("children"):
                child_depth = get_max_depth(call["children"], current_depth + 1)
                max_depth = max(max_depth, child_depth)
        return max_depth
    
    def get_max_width(calls: List[Dict]) -> int:
        max_width = len(calls)
        for call in calls:
            if call.get("children"):
                child_width = get_max_width(call["children"])
                max_width = max(max_width, child_width)
        return max_width
    
    def get_depth_distribution(calls: List[Dict], depth_counts: Dict[int, int] = None, current_depth: int = 1) -> Dict[int, int]:
        """Get distribution of spans at each depth level."""
        if depth_counts is None:
            depth_counts = {}
        
        depth_counts[current_depth] = depth_counts.get(current_depth, 0) + len(calls)
        
        for call in calls:
            if call.get("children"):
                get_depth_distribution(call["children"], depth_counts, current_depth + 1)
        
        return depth_counts
    
    def get_branch_lengths(calls: List[Dict], lengths: List[int] = None, current_length: int = 0) -> List[int]:
        """Get the lengths of all branches in the workflow."""
        if lengths is None:
            lengths = []
        
        if not calls:
            lengths.append(current_length)
            return lengths
        
        for call in calls:
            if call.get("children"):
                get_branch_lengths(call["children"], lengths, current_length + 1)
            else:
                lengths.append(current_length + 1)
        
        return lengths
    
    total_spans = count_spans(workflow["calls"])
    max_depth = get_max_depth(workflow["calls"])
    max_width = get_max_width(workflow["calls"])
    depth_distribution = get_depth_distribution(workflow["calls"])
    branch_lengths = get_branch_lengths(workflow["calls"])
    
    return {
        "total_spans": total_spans,
        "max_depth": max_depth,
        "max_width": max_width,
        "depth_distribution": depth_distribution,
        "branch_lengths": branch_lengths,
        "avg_branch_length": sum(branch_lengths) / len(branch_lengths) if branch_lengths else 0,
        "max_branch_length": max(branch_lengths) if branch_lengths else 0,
        "min_branch_length": min(branch_lengths) if branch_lengths else 0,
        "branch_length_variance": (max(branch_lengths) - min(branch_lengths)) if branch_lengths else 0,
        "target_height": workflow["complexity_profile"]["height"],
        "target_length": workflow["complexity_profile"]["length"],
        "depth_decay": workflow["complexity_profile"]["depth_decay"],
        "width_decay": workflow["complexity_profile"]["width_decay"],
        "branch_variability": workflow["complexity_profile"]["branch_variability"],
        "deep_branch_probability": workflow["complexity_profile"]["deep_branch_probability"]
    }


def main():
    parser = argparse.ArgumentParser(description="Generate abstract workflows with configurable complexity")
    parser.add_argument("--output-dir", default="numbered_workflows", help="Output directory for workflows")
    parser.add_argument("--seed", type=int, help="Random seed for reproducible generation")
    parser.add_argument("--profiles", nargs="+", help="Specific complexity profiles to generate (e.g., Simple Medium Complex)")
    
    # Custom complexity parameters
    parser.add_argument("--height", type=int, help="Maximum fanout (width) at any level")
    parser.add_argument("--length", type=int, help="Maximum chained call length (depth)")
    parser.add_argument("--depth-decay", type=float, help="How quickly depth probability decays (0.1 = slow decay, 0.9 = fast decay)")
    parser.add_argument("--width-decay", type=float, help="How quickly width probability decays (0.1 = slow decay, 0.9 = fast decay)")
    parser.add_argument("--branch-variability", type=float, help="How much branches vary from each other (0.0 = uniform, 1.0 = highly variable)")
    parser.add_argument("--deep-branch-probability", type=float, help="Probability that any given branch will be a 'deep branch'")
    parser.add_argument("--custom-name", default="Custom", help="Name for custom workflow")
    
    args = parser.parse_args()
    
    # Create output directory
    output_dir = Path(args.output_dir)
    output_dir.mkdir(exist_ok=True)
    
    # Initialize generator
    generator = AbstractWorkflowGenerator(seed=args.seed)
    
    # Determine which profiles to use
    profiles = []
    
    if args.profiles:
        # Use predefined profiles
        all_profiles = create_complexity_profiles()
        profile_map = {p.name.lower(): p for p in all_profiles}
        profiles = [profile_map[name.lower()] for name in args.profiles if name.lower() in profile_map]
    
    # Check if custom parameters are provided
    custom_params = [
        args.height, args.length, args.depth_decay, 
        args.width_decay, args.branch_variability, args.deep_branch_probability
    ]
    
    if any(param is not None for param in custom_params):
        # Validate that all required parameters are provided
        if any(param is None for param in custom_params):
            print("Error: If using custom parameters, all complexity parameters must be provided:")
            print("  --height, --length, --depth-decay, --width-decay, --branch-variability, --deep-branch-probability")
            return
        
        # Create custom profile
        custom_profile = ComplexityProfile(
            height=args.height,
            length=args.length,
            depth_decay=args.depth_decay,
            width_decay=args.width_decay,
            branch_variability=args.branch_variability,
            deep_branch_probability=args.deep_branch_probability,
            name=args.custom_name
        )
        profiles.append(custom_profile)
    
    # If no profiles specified, use all predefined profiles
    if not profiles:
        profiles = create_complexity_profiles()
    
    # Generate workflows
    workflows = generator.generate_workflow_variations(profiles)
    
    # Save workflows and generate analysis
    for workflow in workflows:
        # Save workflow
        filename = f"{workflow['workflow_id']}.json"
        filepath = output_dir / filename
        
        with open(filepath, 'w') as f:
            json.dump(workflow, f, indent=2)
        
        # Analyze complexity
        analysis = analyze_workflow_complexity(workflow)
        
        print(f"Generated: {filename}")
        print(f"  Target: height={analysis['target_height']}, length={analysis['target_length']}")
        print(f"  Decay: depth={analysis['depth_decay']}, width={analysis['width_decay']}")
        print(f"  Variability: branch={analysis['branch_variability']}, deep_prob={analysis['deep_branch_probability']}")
        print(f"  Actual: width={analysis['max_width']}, depth={analysis['max_depth']}, total_spans={analysis['total_spans']}")
        print(f"  Branch lengths: min={analysis['min_branch_length']}, avg={analysis['avg_branch_length']:.1f}, max={analysis['max_branch_length']}")
        print(f"  Branch variance: {analysis['branch_length_variance']}")
        print(f"  Depth distribution: {analysis['depth_distribution']}")
        print()
    
    print(f"Generated {len(workflows)} workflows in {output_dir}")


if __name__ == "__main__":
    main() 
# Numbered Service Workflow Protocol

A protocol for creating realistic microservice workflows by first generating arbitrary call graph structures using numbered services, then assigning meaningful names and behaviors.

## Core Idea

1. **Generate numbered service call graphs** (Service_0, Service_1, etc.)
2. **Assign real service names** based on business context
3. **Inject behavioral patterns** (caching, validation, enrichment)
4. **Validate and document** the final workflow

## Key Benefits

- **Structure first, naming later** - Focus on realistic call patterns
- **Flexible complexity** - Create arbitrary depth without constraints
- **Easy validation** - Check at multiple levels (structure, naming, behavior)
- **Research ready** - Generate complexity for distributed tracing evaluation

## Current Implementation

### Abstract Workflow Generation

We can now generate abstract workflows with configurable complexity parameters:

```bash
# Generate simple workflow
python tools/generate_abstract_workflows.py --profiles Simple

# Generate multiple complexity levels
python tools/generate_abstract_workflows.py --profiles Simple Medium Complex

# Generate with specific seed for reproducibility
python tools/generate_abstract_workflows.py --profiles Simple --seed 42
```

### Custom Complexity Parameters

You can also define custom complexity profiles using command line arguments:

```bash
# Create a custom profile with high variability
python tools/generate_abstract_workflows.py \
  --height 4 \
  --length 5 \
  --depth-decay 0.3 \
  --width-decay 0.2 \
  --branch-variability 0.9 \
  --deep-branch-probability 0.7 \
  --custom-name "High Variability" \
  --seed 42
```

### Complexity Parameters

- **height**: Maximum fanout (width) at any level
- **length**: Maximum chained call length (depth)
- **depth_decay**: How quickly depth probability decays (0.1 = slow decay, 0.9 = fast decay)
- **width_decay**: How quickly width probability decays (0.1 = slow decay, 0.9 = fast decay)
- **branch_variability**: How much branches vary from each other (0.0 = uniform, 1.0 = highly variable)
- **deep_branch_probability**: Probability that any given branch will be a "deep branch"

### Predefined Complexity Profiles

Available complexity profiles:
- **Simple**: height=2, length=3, depth_decay=0.5, width_decay=0.3, branch_variability=0.2, deep_branch_probability=0.3
- **Medium**: height=3, length=4, depth_decay=0.4, width_decay=0.2, branch_variability=0.4, deep_branch_probability=0.4
- **Complex**: height=4, length=5, depth_decay=0.3, width_decay=0.15, branch_variability=0.6, deep_branch_probability=0.5
- **Very Complex**: height=5, length=6, depth_decay=0.25, width_decay=0.1, branch_variability=0.7, deep_branch_probability=0.6
- **Extremely Complex**: height=6, length=7, depth_decay=0.2, width_decay=0.08, branch_variability=0.8, deep_branch_probability=0.7
- **Wide Shallow**: height=8, length=3, depth_decay=0.8, width_decay=0.1, branch_variability=0.3, deep_branch_probability=0.2
- **Narrow Deep**: height=2, length=8, depth_decay=0.1, width_decay=0.8, branch_variability=0.9, deep_branch_probability=0.8
- **Balanced Complex**: height=6, length=6, depth_decay=0.3, width_decay=0.2, branch_variability=0.5, deep_branch_probability=0.4
- **Deep Asymmetric**: height=4, length=6, depth_decay=0.1, width_decay=0.5, branch_variability=0.9, deep_branch_probability=0.8
- **Wide Asymmetric**: height=6, length=4, depth_decay=0.6, width_decay=0.1, branch_variability=0.3, deep_branch_probability=0.2
- **Mixed Asymmetric**: height=4, length=5, depth_decay=0.3, width_decay=0.2, branch_variability=0.8, deep_branch_probability=0.6
- **Highly Mixed**: height=5, length=6, depth_decay=0.2, width_decay=0.15, branch_variability=0.9, deep_branch_probability=0.7

### Visualization

View the generated workflow structures:

```bash
python tools/visualize_workflow.py numbered_workflows/abstract_simple.json
```

## Next Steps

1. **Service Assignment**: Map numbered services to real service names
2. **Behavior Injection**: Add caching, validation, enrichment patterns
3. **Validation Framework**: Ensure realistic business logic
4. **Documentation Generation**: Create markdown documentation 
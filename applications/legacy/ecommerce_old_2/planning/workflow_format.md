# Workflow JSON Format

This document defines the JSON format for representing microservice workflows with explicit dependencies and call hierarchies.

## Format Specification

### Root Structure
```json
{
  "workflow_id": "string",
  "endpoint": "string", 
  "description": "string",
  "business_purpose": "string",
  "calls": [
    {
      "call_id": 0,
      "service": "Root Service Name",
      "operation": "endpoint_operation",
      "depends_on": [],
      "business_purpose": "Handle the endpoint request",
      "children": [...]
    }
  ]
}
```

**Root Service Requirements**:
- Every workflow must have exactly one root service call (call_id: 0)
- Root service represents the microservice that handles the endpoint
- Root service orchestrates all other service calls as children
- Root service has no dependencies (depends_on: [])

### Call Structure
```json
{
  "call_id": 0,
  "service": "Service Name",
  "operation": "operation_name",
  "depends_on": [0, 1],
  "business_purpose": "Description",
  "children": [...]
}
```

## Workflow Design Approach

Workflows should be designed based on realistic business requirements and service dependencies, not predefined complexity rules. The complexity level should be determined after the workflow is created based on its actual characteristics.

### Design Principles
1. **Business-Driven**: Workflows should represent actual business operations
2. **Service-Driven**: Use realistic service dependencies and call patterns
3. **Natural Complexity**: Let complexity emerge from business requirements
4. **Post-Classification**: Determine complexity after workflow creation

### Complexity Analysis
After creating a workflow, analyze:
- **Depth**: Maximum nesting level in the call tree
- **Width**: Number of root-level service calls
- **Total Services**: Total number of unique services involved
- **Dependency Patterns**: Sequential vs parallel call patterns

## Key Features

### Call IDs
- Numeric IDs starting from 0
- Restart from 0 for each sibling set
- Enables unambiguous dependency tracking

### Dependencies
- `depends_on` array contains call IDs from same level
- Implicit parent-child relationships through nesting
- Supports complex dependency patterns

### Business Context
- Each call includes business purpose
- Workflow includes overall business purpose
- Enables traceability to business requirements

## Example Usage

See `simple_workflow_example.json` and `complex_workflow_example.json` for concrete examples.


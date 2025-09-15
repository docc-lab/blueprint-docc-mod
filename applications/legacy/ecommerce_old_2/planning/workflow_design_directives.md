# Workflow Design Directives

This document establishes design patterns and directives for creating realistic microservice workflows that mirror actual distributed system architectures.

## Core Design Principles

### 1. **Root Service Pattern**
- Every workflow must have a **root service** that handles the endpoint
- Root service represents the actual microservice that receives the HTTP request
- Root service orchestrates calls to other services as children
- Root service is responsible for the endpoint's business logic

**Example Root Services**:
- `User Management Service` for `/user/*` endpoints
- `Product Catalog Service` for `/products/*` endpoints
- `Shopping Cart Service` for `/cart/*` endpoints
- `Checkout Service` for `/checkout/*` endpoints

### 2. **Service Responsibility Separation**
- Each service should have a single, well-defined responsibility
- Services should not duplicate functionality
- Data ownership should be clear and non-overlapping

### 3. **Realistic Call Patterns**
- Workflows should mirror actual microservice communication patterns
- Avoid artificial complexity or oversimplification
- Let business requirements drive design decisions

### 4. **Dependency Management**
- Use explicit dependencies to show service relationships
- Avoid circular dependencies
- Prefer sequential over parallel when business logic requires it

## Service Type Patterns

### **Enrichment Services**
**Pattern**: Orchestrator that calls multiple specialized services
- **Do**: Have children that call other services for data
- **Don't**: Store enrichment data themselves
- **Do**: Combine and format results from multiple sources
- **Don't**: Depend on other services as siblings

**Example Structure**:
```json
{
  "service": "User Enrichment Service",
  "children": [
    {"service": "Preferences Service"},
    {"service": "Account History Service"},
    {"service": "User Search Service"}
  ]
}
```

### **Cache Services**
**Pattern**: Cache-Aside pattern for performance optimization
- **Do**: Appear **before** the main data service for cache checks
- **Do**: Have the main data service depend on cache service (for cache miss logic)
- **Do**: Have cache update service depend on main data service
- **Do**: All operations at the same level (siblings, not parent-child)
- **Don't**: Appear as children of data services
- **Don't**: Store primary data

**Example Structure**:
```json
[
  {
    "call_id": 0,
    "service": "User Cache Service",
    "operation": "get_cached_user_profile",
    "depends_on": [],
    "business_purpose": "Check cache for user profile data"
  },
  {
    "call_id": 1, 
    "service": "User Profile Service",
    "operation": "get_user_profile",
    "depends_on": [0],  // Only called if cache miss
    "business_purpose": "Retrieve user profile from database"
  },
  {
    "call_id": 2,
    "service": "User Cache Service", 
    "operation": "update_cache",
    "depends_on": [1],  // Update cache with fresh data
    "business_purpose": "Update cache with retrieved data"
  }
]
```

**Realistic Flow**:
1. **Cache Check** (first call)
2. **Data Service** (if cache miss, depends on cache check)
3. **Cache Update** (depends on data service, updates cache with fresh data)

### **Validation Services**
**Pattern**: Data integrity and business rule enforcement
- **Do**: Appear early in workflows
- **Do**: Have children for complex validation chains
- **Don't**: Modify data (only validate)
- **Do**: Return validation results

### **Authentication & Authorization Services**
**Pattern**: Security and access control
- **Do**: Appear early in workflows that require authentication
- **Do**: Have clear dependency chains
- **Don't**: Be bypassed for security-sensitive operations

### **History/Audit Services**
**Pattern**: Logging and compliance
- **Do**: Appear near the end of workflows
- **Do**: Depend on the operations they're logging
- **Don't**: Block main workflow execution
- **Do**: Run in parallel when possible

## Workflow Construction Rules

### **Depth Creation Patterns**

1. **Validation Chains**
   ```
   Main Service → Validation Service → Sub-Validation Service → Sub-Sub-Validation
   ```

2. **Enrichment Chains**
   ```
   Main Service → Enrichment Service → Data Service A → Data Service B → Data Service C
   ```

3. **Security Chains**
   ```
   Main Service → Auth Service → Authorization Service → Permission Service → Role Service
   ```

4. **Integration Chains**
   ```
   Main Service → Gateway Service → External Service → Response Processing → Data Transformation
   ```

### **Width Creation Patterns**

1. **Parallel Validation**
   ```
   Main Service
   ├─ Validation Service A
   ├─ Validation Service B
   └─ Validation Service C
   ```

2. **Parallel Enrichment**
   ```
   Main Service
   ├─ Enrichment Service A
   ├─ Enrichment Service B
   └─ Enrichment Service C
   ```

3. **Parallel Processing**
   ```
   Main Service
   ├─ Processing Service A
   ├─ Processing Service B
   └─ Processing Service C
   ```

## Service Interaction Patterns

### **Data Retrieval Pattern**
```
Primary Service → Cache Service (child)
```

### **Enrichment Pattern**
```
Primary Service → Enrichment Service → Multiple Data Services (children)
```

### **Validation Pattern**
```
Primary Service → Validation Service → Sub-Validation Services (children)
```

### **Security Pattern**
```
Primary Service → Auth Service → Authorization Service → Permission Service
```

### **Audit Pattern**
```
Primary Service → History Service (parallel, near end)
```

## Complexity Guidelines

### **When to Create Depth**
- Complex validation requirements
- Multi-step enrichment processes
- Security chains with multiple checks
- Integration with external systems
- Compliance and audit requirements

### **When to Create Width**
- Independent validation checks
- Parallel enrichment processes
- Independent processing steps
- Multiple data source requirements

### **When to Avoid Complexity**
- Simple data retrieval
- Basic CRUD operations
- Straightforward business logic
- Performance-critical paths

## Common Anti-Patterns to Avoid

1. **Service Duplication**: Don't have multiple services doing the same thing
2. **Artificial Depth**: Don't create depth without business justification
3. **Circular Dependencies**: Don't create dependency cycles
4. **Over-Caching**: Don't cache everything - only cache frequently accessed data
5. **Over-Validation**: Don't validate the same thing multiple times
6. **Service Bloat**: Don't create services that do too many things

## Decision Framework

When designing a workflow, ask:

1. **What is the business purpose?**
2. **What data is needed?**
3. **What validations are required?**
4. **What security checks are needed?**
5. **What enrichment would improve the result?**
6. **What should be logged/audited?**
7. **What can be cached for performance?**

This framework ensures workflows are business-driven rather than technically-driven. 
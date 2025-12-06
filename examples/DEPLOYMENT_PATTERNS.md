# Deployment Patterns: goproc.Deploy + linuxcontainer.Deploy vs thrift.Deploy + CreateProcess + CreateContainer

This document analyzes the differences between two deployment patterns used in Blueprint examples.

## Pattern 1: goproc.Deploy + linuxcontainer.Deploy (SockShop Pattern)

### Usage Example
```go
applyDockerDefaults := func(serviceName string, useHTTP ...bool) {
    // Application-level modifiers
    retries.AddRetries(spec, serviceName, 3)
    clientpool.Create(spec, serviceName, 20)
    opentelemetry.Instrument(spec, serviceName, trace_collector)
    
    // RPC deployment
    if len(useHTTP) > 0 && useHTTP[0] {
        http.Deploy(spec, serviceName)
    } else {
        grpc.Deploy(spec, serviceName)
    }
    
    // Process and container deployment
    goproc.Deploy(spec, serviceName)
    linuxcontainer.Deploy(spec, serviceName)
}
```

### Characteristics

1. **Implicit Naming**
   - `goproc.Deploy(spec, "user_service")` creates process `"user_proc"`
   - `linuxcontainer.Deploy(spec, "user_service")` creates container `"user_ctr"`
   - Naming convention: `_service` → `_proc` → `_ctr`

2. **Convenience Functions**
   - `goproc.Deploy()` is a wrapper around `goproc.CreateProcess()` with automatic naming
   - `linuxcontainer.Deploy()` is a wrapper around `linuxcontainer.CreateContainer()` with automatic naming
   - Designed for single-service-per-process-per-container deployments

3. **Order of Operations**
   ```
   Service (application-level)
     ↓
   RPC Deployment (grpc/http) - makes service network-accessible
     ↓
   goproc.Deploy() - wraps service in a process
     ↓
   linuxcontainer.Deploy() - wraps process in a container
   ```

4. **What Happens Internally**
   - `goproc.Deploy()`:
     - Extracts service prefix (removes `_service` suffix)
     - Creates process name: `{prefix}_proc`
     - Calls `goproc.CreateProcess(spec, procName, serviceName)`
     - Returns the process name
   - `linuxcontainer.Deploy()`:
     - Extracts service prefix (removes `_service` suffix)
     - Creates container name: `{prefix}_ctr`
     - Calls `linuxcontainer.CreateContainer(spec, ctrName, serviceName)`
     - Returns the container name
   - **Note**: `linuxcontainer.Deploy()` expects `serviceName` to already be a process-level service

5. **RPC Protocol**
   - Typically uses **gRPC** or **HTTP**
   - RPC deployment happens before process deployment

---

## Pattern 2: thrift.Deploy + goproc.CreateProcess + linuxcontainer.CreateContainer (DSB_SN Pattern)

### Usage Example
```go
applyDockerDefaults := func(sp wiring.WiringSpec, serviceName string, procName string, ctrName string) string {
    retries.AddRetries(sp, serviceName, 3)
    clientpool.Create(sp, serviceName, 20)
    opentelemetry.Instrument(sp, serviceName, trace_collector)
    
    thrift.Deploy(sp, serviceName)
    goproc.CreateProcess(sp, procName, serviceName)
    return linuxcontainer.CreateContainer(sp, ctrName, procName)
}
```

### Characteristics

1. **Explicit Naming**
   - Process name: `"urlshorten_proc"` (explicitly provided)
   - Container name: `"urlshorten_container"` (explicitly provided)
   - Full control over naming conventions

2. **Explicit Functions**
   - `thrift.Deploy()` - Deploys service with Thrift RPC
   - `goproc.CreateProcess()` - Creates process with explicit name
   - `linuxcontainer.CreateContainer()` - Creates container with explicit name
   - Allows multiple services per process/container (via `AddToProcess`/`AddToContainer`)

3. **Order of Operations**
   ```
   Service (application-level)
     ↓
   thrift.Deploy() - makes service network-accessible via Thrift
     ↓
   goproc.CreateProcess() - wraps service in a process (explicit name)
     ↓
   linuxcontainer.CreateContainer() - wraps process in a container (explicit name)
   ```

4. **What Happens Internally**
   - `thrift.Deploy()`:
     - Generates Thrift client and server code
     - Creates address node for service discovery
     - Adds client-side and server-side modifiers
   - `goproc.CreateProcess()`:
     - Creates a process node with the specified name
     - Adds the service to the process
     - Sets up default logger and metric collector
   - `linuxcontainer.CreateContainer()`:
     - Creates a container node with the specified name
     - Adds the process to the container
     - Returns the container name

5. **RPC Protocol**
   - Uses **Thrift** for RPC
   - Thrift deployment happens before process deployment

---

## Key Differences

### 1. Naming Strategy

| Aspect | Pattern 1 (Deploy) | Pattern 2 (Create) |
|--------|-------------------|-------------------|
| Process Naming | Implicit (`service_proc`) | Explicit (provided as parameter) |
| Container Naming | Implicit (`service_ctr`) | Explicit (provided as parameter) |
| Flexibility | Fixed naming convention | Full control over names |

**Example:**
```go
// Pattern 1: Automatic naming
goproc.Deploy(spec, "user_service")        // Creates "user_proc"
linuxcontainer.Deploy(spec, "user_service") // Creates "user_ctr"

// Pattern 2: Explicit naming
goproc.CreateProcess(spec, "my_custom_proc", "user_service")
linuxcontainer.CreateContainer(spec, "my_custom_container", "my_custom_proc")
```

### 2. Multi-Service Support

| Aspect | Pattern 1 (Deploy) | Pattern 2 (Create) |
|--------|-------------------|-------------------|
| Multiple Services | Each service gets its own process/container | Can add multiple services to same process/container |
| Flexibility | One service per process/container | Flexible grouping |

**Example:**
```go
// Pattern 1: Each service gets its own process/container
goproc.Deploy(spec, "service1")  // Creates "service1_proc"
goproc.Deploy(spec, "service2")  // Creates "service2_proc"

// Pattern 2: Can group services
goproc.CreateProcess(spec, "shared_proc", "service1", "service2")
goproc.AddToProcess(spec, "shared_proc", "service3")  // Add more later
```

### 3. Return Values

| Function | Pattern 1 | Pattern 2 |
|----------|-----------|-----------|
| `goproc.Deploy()` | Returns process name | N/A |
| `goproc.CreateProcess()` | N/A | Returns process name |
| `linuxcontainer.Deploy()` | Returns container name | N/A |
| `linuxcontainer.CreateContainer()` | N/A | Returns container name |

### 4. RPC Protocol

| Pattern | RPC Protocol | When Applied |
|---------|--------------|--------------|
| Pattern 1 | gRPC or HTTP | Before process deployment |
| Pattern 2 | Thrift | Before process deployment |

### 5. Service Level Expectations

| Pattern | linuxcontainer Expects |
|---------|------------------------|
| Pattern 1 | `linuxcontainer.Deploy()` expects service to already be process-level |
| Pattern 2 | `linuxcontainer.CreateContainer()` expects process name (not service name) |

**Important**: In Pattern 1, `linuxcontainer.Deploy()` is called with the service name, but internally it expects the service to already be wrapped in a process (which happens via `goproc.Deploy()`). In Pattern 2, `linuxcontainer.CreateContainer()` is explicitly called with the process name.

---

## When to Use Each Pattern

### Use Pattern 1 (goproc.Deploy + linuxcontainer.Deploy) When:
- ✅ You want simple, one-service-per-container deployments
- ✅ You're using gRPC or HTTP (not Thrift)
- ✅ You're okay with implicit naming conventions
- ✅ You want concise, readable wiring specs
- ✅ You don't need to group multiple services

**Best for**: Simple microservices architectures where each service runs in its own container.

### Use Pattern 2 (thrift.Deploy + CreateProcess + CreateContainer) When:
- ✅ You need explicit control over process/container names
- ✅ You're using Thrift for RPC
- ✅ You want to group multiple services in the same process/container
- ✅ You need custom naming conventions
- ✅ You want to track container names for deployment orchestration

**Best for**: Complex deployments with custom naming, Thrift-based services, or when grouping services.

---

## Code Flow Comparison

### Pattern 1 Flow
```go
// 1. Define service
user_service := workflow.Service[user.UserService](spec, "user_service", user_db)

// 2. Apply modifiers
retries.AddRetries(spec, "user_service", 3)
grpc.Deploy(spec, "user_service")           // Makes network-accessible

// 3. Deploy to process (creates "user_proc")
goproc.Deploy(spec, "user_service")

// 4. Deploy to container (creates "user_ctr")
linuxcontainer.Deploy(spec, "user_service")  // Note: uses service name, but expects process-level
```

### Pattern 2 Flow
```go
// 1. Define service
user_service := workflow.Service[socialnetwork.UserService](spec, "user_service", ...)

// 2. Apply modifiers
retries.AddRetries(spec, "user_service", 3)
thrift.Deploy(spec, "user_service")         // Makes network-accessible via Thrift

// 3. Create process with explicit name
goproc.CreateProcess(spec, "user_proc", "user_service")

// 4. Create container with explicit name (uses process name)
user_ctr := linuxcontainer.CreateContainer(spec, "user_container", "user_proc")
```

---

## Implementation Details

### goproc.Deploy Implementation
```go
func Deploy(spec wiring.WiringSpec, serviceName string) string {
    servicePrefix, _ := strings.CutSuffix(serviceName, "_service")
    procName := servicePrefix + "_proc"
    CreateProcess(spec, procName, serviceName)
    return procName
}
```

### linuxcontainer.Deploy Implementation
```go
func Deploy(spec wiring.WiringSpec, serviceName string) string {
    servicePrefix, _ := strings.CutSuffix(serviceName, "_service")
    ctrName := servicePrefix + "_ctr"
    CreateContainer(spec, ctrName, serviceName)  // Uses serviceName, expects it to be process-level
    return ctrName
}
```

### Key Insight
`linuxcontainer.Deploy()` accepts a `serviceName` parameter, but it expects that service to already be wrapped in a process. The Blueprint compiler will resolve this correctly because `goproc.Deploy()` adds a modifier that converts the service to a process-level service.

---

## Summary Table

| Feature | Pattern 1 (Deploy) | Pattern 2 (Create) |
|---------|-------------------|-------------------|
| **Naming** | Implicit | Explicit |
| **RPC** | gRPC/HTTP | Thrift |
| **Multi-service** | No | Yes |
| **Convenience** | High | Medium |
| **Flexibility** | Low | High |
| **Code Verbosity** | Low | Medium |
| **Use Case** | Simple microservices | Complex deployments |

Both patterns achieve the same end result (service deployed in a container), but differ in:
- **Convenience vs Control**: Pattern 1 prioritizes convenience, Pattern 2 prioritizes control
- **RPC Protocol**: Different protocols (gRPC/HTTP vs Thrift)
- **Naming**: Implicit vs explicit
- **Grouping**: Single service vs multiple services per process/container


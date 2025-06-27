# AI Notes - Persistent Knowledge Base

## Project Overview
- **Project Name**: blueprint-docc-mod
- **Type**: Blueprint framework for distributed systems with DOCC (Distributed Object Communication and Coordination) integration
- **Language**: Primarily Go, with Python components (d2k8s, k8s-convert)
- **Architecture**: Plugin-based system with IR (Intermediate Representation) for code generation

## Key Components

### Core Framework
- `blueprint/` - Main Go framework with wiring, IR, and core plugins
- `plugins/` - Extensible plugin system for various technologies
- `runtime/` - Runtime implementation and plugin registry
- `examples/` - Sample applications (hotel reservation, social network, etc.)

### Infrastructure Tools
- `d2k8s/` - Python tool for converting to Kubernetes
- `k8s-convert/` - Kubernetes conversion utilities
- `opentelemetry-stable/` - OpenTelemetry integration

### Notable Plugins
- **otelcol** - OpenTelemetry Collector integration
- **golang** - Go code generation
- **http/grpc** - HTTP/gRPC client/server generation
- **docker/dockercompose** - Container orchestration
- **databases** - MongoDB, MySQL, Redis, Memcached, RabbitMQ
- **tracing** - Jaeger, Zipkin, XTrace integration

## Important Patterns

### IR (Intermediate Representation)
- Used for code generation and system specification
- Defined in `blueprint/pkg/ir/`
- Plugins implement IR interfaces for their specific domains

### Wiring System
- Application composition and dependency injection
- Defined in `blueprint/pkg/wiring/`
- Handles namespace management and application assembly

### Plugin Architecture
- Plugins provide IR implementations and wiring functions
- Runtime plugins implement actual functionality
- Registry system for plugin discovery

## Development Workflow
- Use `go.work` for multi-module development
- Examples demonstrate complete application patterns
- Testing framework in `test/` directory
- Documentation generation scripts in `scripts/`

## Key Files to Remember
- `go.work` - Workspace configuration
- `blueprint/pkg/ir/ir.go` - Core IR definitions
- `blueprint/pkg/wiring/application.go` - Application wiring
- `plugins/otelcol/` - OpenTelemetry integration example
- `examples/dsb_hotel/` - Complex application example

## Important Notes
- This is a research/academic project (DOCC Lab)
- Focuses on distributed systems and microservices
- Uses code generation for infrastructure automation
- Supports multiple deployment targets (Docker, K8s, etc.)

---
*Last Updated: [Current Date]*
*This file contains persistent knowledge that should be referenced in future conversations.* 
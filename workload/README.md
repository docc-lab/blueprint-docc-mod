# Blueprint Workload Testing Suite

A comprehensive, standalone workload testing suite for Blueprint applications. This suite provides deep workflow testing capabilities that traverse multiple service hops on causal paths, enabling thorough testing of distributed systems and observability.

## Features

- **Deep Workflow Testing**: Tests workflows that traverse multiple service hops
- **Multi-threaded User Simulation**: Concurrent user simulation with configurable parameters
- **Comprehensive Metrics**: Detailed performance and observability metrics
- **Application Agnostic**: Works with any Blueprint application
- **Standalone Operation**: No dependency on Blueprint workload infrastructure

## Directory Structure

```
workload/
├── README.md                 # This file
├── go.mod                    # Go module definition
├── go.sum                    # Go module checksums
├── cmd/                      # Command-line tools
│   ├── init-data/           # Data initialization tool
│   │   ├── main.go
│   │   └── init.go
│   └── ecommerce-workload/  # Main workload generator
│       ├── main.go
│       └── workload.go
├── internal/                 # Internal packages
│   ├── client/              # HTTP client utilities
│   ├── metrics/             # Metrics collection
│   └── workflows/           # Workflow definitions
├── examples/                # Example configurations
│   └── sockshop/           # SockShop-specific examples
└── scripts/                 # Helper scripts
    ├── run_workload.sh      # Main runner script
    └── analyze_results.py   # Results analysis
```

## Quick Start

### Prerequisites

- Go 1.19 or later
- Access to a deployed Blueprint application (e.g., SockShop)
- Network connectivity to the application's frontend service

### Basic Usage

1. **Initialize test data:**
   ```bash
   cd workload
   go run cmd/init-data/main.go -frontend-url=http://192.168.64.11:32170
   ```

2. **Run workload:**
   ```bash
   go run cmd/ecommerce-workload/main.go -frontend-url=http://192.168.64.11:32170 -users=10 -duration=5m
   ```

3. **Use the runner script:**
   ```bash
   ./scripts/run_workload.sh --workload-users 20 --duration 10m --mix realistic
   ```

## Configuration

### Environment Variables

- `FRONTEND_URL`: Frontend service URL (default: http://192.168.64.11:32170)
- `CATALOGUE_SIZE`: Number of catalogue items to generate (default: 100)
- `USER_COUNT`: Number of users to pre-create (default: 50)
- `WORKLOAD_USERS`: Number of concurrent workload users (default: 10)
- `WORKLOAD_DURATION`: Workload duration (default: 5m)
- `WORKLOAD_MIX`: Workload mix type (default: realistic)

### Workload Mixes

- **realistic**: Complete e-commerce workflows (browse → cart → register → order)
- **browsing**: Heavy catalogue browsing operations
- **purchasing**: Focus on order processing workflows
- **stress**: High-frequency requests with minimal think time

## Deep Workflow Testing

This suite is specifically designed to test deep workflows that traverse multiple service hops:

### Service Hop Patterns

1. **Browse Catalogue** (2 hops): Frontend → Catalogue
2. **Add to Cart** (3 hops): Frontend → Catalogue → Cart
3. **Register User** (2 hops): Frontend → User
4. **Add Address/Payment** (2 hops): Frontend → User
5. **Place Order** (6 hops): Frontend → Order → User → Cart → Payment → Shipping
6. **Check Orders** (2 hops): Frontend → Order

### Research Value

These deep workflows are essential for:
- **Distributed Tracing Research**: Testing span reconstruction across service boundaries
- **Observability Analysis**: Understanding service interaction patterns
- **Performance Testing**: Identifying bottlenecks in multi-service workflows
- **Error Propagation**: Testing how errors propagate through service chains

## Metrics and Analysis

The suite collects comprehensive metrics:

- **Request Statistics**: Total requests, success rate, latency distribution
- **Service Hop Analysis**: Distribution of requests by service hop count
- **Operation Statistics**: Breakdown by operation type
- **Performance Metrics**: Average, min, max latency per operation
- **Error Analysis**: Detailed error tracking and categorization

Results are saved in JSON format for further analysis and visualization.

## Integration with Observability Stack

This workload suite is designed to work seamlessly with:
- **OpenTelemetry Collector**: Generates traces for span reconstruction testing
- **Jaeger**: Provides trace visualization and analysis
- **Custom Processors**: Tests span reconstruction and agent contact protocols

## Development

### Adding New Workflows

1. Define workflow in `internal/workflows/`
2. Implement HTTP client calls in `internal/client/`
3. Add metrics collection in `internal/metrics/`
4. Update main workload generator

### Adding New Applications

1. Create application-specific examples in `examples/`
2. Define application-specific workflows
3. Add configuration templates
4. Update documentation

## Examples

See `examples/sockshop/` for SockShop-specific configurations and workflows.

## Contributing

This is a research tool designed for distributed systems and observability research. Contributions that enhance deep workflow testing capabilities are welcome.

## License

This project follows the same license as the parent Blueprint project.

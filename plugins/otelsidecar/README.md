# OpenTelemetry Sidecar Plugin

This plugin provides OpenTelemetry sidecar functionality for Blueprint applications.

## Overview

The OpenTelemetry Sidecar plugin allows applications to process and forward traces to either a Jaeger or Zipkin collector. The sidecar acts as an intermediary between the application and the collector, providing additional processing capabilities.

## Usage

To use the OpenTelemetry Sidecar plugin in your Blueprint application:

```go
import "github.com/blueprint-uservices/blueprint/plugins/otelsidecar"

// In your wiring spec:
// For Jaeger:
sidecar := otelsidecar.DeploySidecar(spec, "otel_sidecar", "jaeger")

// For Zipkin:
sidecar := otelsidecar.DeploySidecar(spec, "otel_sidecar", "zipkin")
```

## Configuration

The OpenTelemetry Sidecar can be configured with the following options:

- `WithPort(port int)`: Sets the port for the sidecar to listen on (default: 4318)
- `WithImage(image string)`: Sets the Docker image to use (default: "otel/opentelemetry-collector")

## Implementation Details

The plugin consists of the following components:

- `ir_sidecar.go`: Defines the IR node for the OpenTelemetry sidecar
- `ir_sidecar_client.go`: Defines the client-side IR node
- `wiring.go`: Contains the wiring logic for deploying the sidecar

## Dependencies

- OpenTelemetry Collector Docker image
- OpenTelemetry SDK for Go
- Either Jaeger or Zipkin collector (depending on configuration) 
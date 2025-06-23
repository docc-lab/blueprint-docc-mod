# Tracing Agent Plugin

This plugin provides integration with a distributed tracing agent for Blueprint applications. It is analogous to the Jaeger and Zipkin plugins, but instead of exporting traces directly to a central backend, it communicates with a distributed tracing agent that can perform intermediate processing.

- The wiring and IR node structure closely follows the patterns in `plugins/jaeger` and `plugins/zipkin`.
- The runtime implementation will be found in `runtime/plugins/tracingagent`.

Implementation details to be filled in. 
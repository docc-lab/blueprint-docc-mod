# To add OTel collector to existing app manually (after blueprint code generation)

You need to follow steps below

- Add OTel collector implementation into plugin
  - replace the one build/docker dir for **every single service** with `manual-otel-plugin-template.go`

- Update dependency builder to use OTel collector
    For **every single** service, update the `<servicename>_proc.go` manually

    ```go
    // Add a new definition for the OTel collector client
    b.Define("otel.collector", func(n *golang.Namespace) (any, error) {
        var addr string
        if err := n.Get("otel.collector.addr", &addr); err != nil {
            return nil, err
        }

        return opentelemetry.NewOTelCollectorTracer(n.Context(), addr, "cart-service")
    })

    // Then update any references to zipkin.client to use otel.collector instead
    b.Define("cart_service.server.ot", func(n *golang.Namespace) (any, error) {
        var service cart.CartService
        if err := n.Get("cart_service", &service); err != nil {
            return nil, err
        }

        var otCollectorClient backend.Tracer
        // Change this line to use otel.collector instead of zipkin.client
        if err := n.Get("otel.collector", &otCollectorClient); err != nil {
            return nil, err
        }

        return ot.New_CartService_OTServerWrapper(n.Context(), service, otCollectorClient)
    })
    ```

- Add dependencies in go.mod for each service

    ```bash
    go get go.opentelemetry.io/otel/exporters/otlp/otlptrace
    go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc
    ```

- Create an OpenTelemetry collector configuration file & build image for it
  - create a new dir under `build/docker` dir for otel_collector, then copy the `otel-collector-config.yaml` into it.
  - Update docker compose yaml, making it ready to build otel collector image

    ``` yaml
    services:
        # Other existing services...
    
    otel-collector:
        image: otel/opentelemetry-collector:latest
        command: ["--config=/etc/otel-collector-config.yaml"]
        volumes:
            - ./otel-collector-config.yaml:/etc/otel-collector-config.yaml 
        ports:
            - "4317:4317"  # OTLP gRPC
            - "4318:4318"  # OTLP HTTP
        networks:
            - your-network-name  # Use existing network if any
    ```

  - run `docker compose build` as usual to build otel collector image

- Deploy & run the OpenTelemetry collector in docker/k8s
  - `docker compose up` should work directly for docker environment
  - for k8s deployment
    - use the ims-buildpus-k8s-cvrt.sh as usual
    - copy the otel-collector-k8sconfigmap.yaml into `build/docker` dir
    - apply all yaml files, including otel configmap one, by `kubectl apply -f .`
    - verify k8s collector deployment

        ```bash
        // Verify the ConfigMap was created
        kubectl get configmap otel-collector-config

        // Check if the collector pod is running
        kubectl get pods -l app=otel-collector

        // Verify the service is created
        kubectl get service otel-collector
        ```

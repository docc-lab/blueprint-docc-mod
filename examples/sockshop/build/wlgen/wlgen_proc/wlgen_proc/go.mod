module blueprint/goproc/wlgen_proc

go 1.23.6

require (
	github.com/blueprint-uservices/blueprint/examples/sockshop/workflow v0.0.0-20240405152959-f078915d2306
	github.com/blueprint-uservices/blueprint/examples/sockshop/workload v0.0.0-00010101000000-000000000000
	github.com/blueprint-uservices/blueprint/runtime v0.0.0-20240619221802-d064c5861c1e
)

require (
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	go.mongodb.org/mongo-driver v1.15.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/otel v1.35.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdoutmetric v1.26.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.26.0 // indirect
	go.opentelemetry.io/otel/metric v1.35.0 // indirect
	go.opentelemetry.io/otel/sdk v1.35.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.34.0 // indirect
	go.opentelemetry.io/otel/trace v1.35.0 // indirect
	golang.org/x/exp v0.0.0-20240416160154-fe59bbe5cc7f // indirect
	golang.org/x/sys v0.30.0 // indirect
)

replace github.com/blueprint-uservices/blueprint/runtime => ../runtime

replace github.com/blueprint-uservices/blueprint/examples/sockshop/workflow => ../workflow

replace github.com/blueprint-uservices/blueprint/examples/sockshop/workload => ../workload

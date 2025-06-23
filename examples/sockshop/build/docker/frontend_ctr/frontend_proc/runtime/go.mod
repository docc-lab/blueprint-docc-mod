module github.com/blueprint-uservices/blueprint/runtime

go 1.23.6

toolchain go1.23.6

require (
	github.com/DistributedClocks/GoVector v0.0.0-20240117185643-ae07272d0ebd
	github.com/blueprint-uservices/blueprint/plugins v0.0.0-20240405152959-f078915d2306
	github.com/bradfitz/gomemcache v0.0.0-20230905024940-24af94b03874
	github.com/go-redis/redis/v8 v8.11.5
	github.com/go-sql-driver/mysql v1.8.1
	github.com/jmoiron/sqlx v1.4.0
	github.com/mattn/go-sqlite3 v1.14.22
	github.com/rabbitmq/amqp091-go v1.9.0
	github.com/stretchr/testify v1.10.0
	github.com/tracingplane/tracingplane-go v0.0.0-20171025152126-8c4e6f79b148
	gitlab.mpi-sws.org/cld/tracing/tracing-framework-go v0.0.0-20211206181151-6edc754a9f2a
	go.mongodb.org/mongo-driver v1.15.0
	go.opentelemetry.io/otel v1.35.0
	go.opentelemetry.io/otel/exporters/jaeger v1.17.0
	go.opentelemetry.io/otel/exporters/stdout/stdoutmetric v1.26.0
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.26.0
	go.opentelemetry.io/otel/exporters/zipkin v1.26.0
	go.opentelemetry.io/otel/metric v1.35.0
	go.opentelemetry.io/otel/sdk v1.35.0
	go.opentelemetry.io/otel/sdk/metric v1.34.0
	go.opentelemetry.io/otel/trace v1.35.0
	golang.org/x/exp v0.0.0-20240416160154-fe59bbe5cc7f
	gonum.org/v1/gonum v0.15.1
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/blueprint-uservices/blueprint/blueprint v0.0.0-20240120085724-a66c24cd32b1 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/daviddengcn/go-colortext v1.0.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.26.1 // indirect
	github.com/klauspost/compress v1.17.8 // indirect
	github.com/montanaflynn/stats v0.7.1 // indirect
	github.com/openzipkin/zipkin-go v0.4.3 // indirect
	github.com/otiai10/copy v1.14.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/vmihailenco/msgpack/v5 v5.4.1 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.1.2 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240424034433-3c2c7870ae76 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.35.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.35.0 // indirect
	go.opentelemetry.io/proto/otlp v1.5.0 // indirect
	golang.org/x/crypto v0.33.0 // indirect
	golang.org/x/mod v0.17.0 // indirect
	golang.org/x/net v0.35.0 // indirect
	golang.org/x/sync v0.11.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	golang.org/x/tools v0.21.1-0.20240508182429-e35e4ccd0d2d // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250218202821-56aae31c358a // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250218202821-56aae31c358a // indirect
	google.golang.org/grpc v1.71.0 // indirect
	google.golang.org/protobuf v1.36.5 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/blueprint-uservices/blueprint/examples/sockshop/workflow => ../workflow

replace blueprint/goproc/frontend_proc => ../frontend_proc

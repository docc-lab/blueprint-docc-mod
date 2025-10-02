# burstload

```go
import "github.com/blueprint-uservices/blueprint/plugins/burstload"
```

Package burstload provides a Blueprint modifier for generating burst load on service calls.

The plugin creates burst generation logic inside the service that generates bursts independently of business logic. It records real parameters from actual service calls (every 200 requests) and uses this real data for burst generation. It follows the pattern: Generate X requests/sec for Y seconds, then wait Z seconds, repeat. This is useful for load testing, stress testing, and simulating realistic traffic scenarios.

Usage:

```go
import "github.com/blueprint-uservices/blueprint/plugins/burstload"
burstload.AddBurstLoad(spec, "my_service", 10, "30s", "10s") // 10 req/sec for 30s, then wait 10s, repeat
```

## Index

- [burstload](#burstload)
  - [Index](#index)
  - [func AddBurstLoad](#func-addburstload)
  - [type BurstLoadGenerator](#type-burstloadgenerator)
  - [Environment Variables](#environment-variables)
    - [Supported Parameters](#supported-parameters)
    - [Examples](#examples)
    - [Kubernetes Deployment Example](#kubernetes-deployment-example)
    - [Docker Compose Example](#docker-compose-example)
  - [Generated Code Behavior](#generated-code-behavior)
    - [Burst Pattern Timeline](#burst-pattern-timeline)
    - [Data Recording and Replay](#data-recording-and-replay)
    - [Context Usage](#context-usage)
  - [Example Usage](#example-usage)
    - [How Data Recording Works](#how-data-recording-works)

<a name="AddBurstLoad"></a>
## func [AddBurstLoad](https://github.com/Blueprint-uServices/blueprint/blob/main/plugins/burstload/wiring.go#L31)

```go
func AddBurstLoad(spec wiring.WiringSpec, serviceName string, burst_size int64, burst_duration string, burst_interval string)
```

Add burst generation for the specified service. Uses a [blueprint.WiringSpec]. Creates burst generation logic inside the service that generates bursts independently of business logic. Pattern: Generate `burst_size` requests/sec for `burst_duration`, then wait `burst_interval`, repeat.

Parameters:
- **burst_size**: X requests per second during burst periods
- **burst_duration**: Y seconds - how long each burst period lasts
- **burst_interval**: Z seconds - wait time between burst periods

Usage:

```go
AddBurstLoad(spec, "my_service", 10, "30s", "10s")
```

This will generate 10 requests/sec for 30 seconds, then wait 10 seconds, then repeat the pattern.


<a name="BurstLoadGenerator"></a>
## type BurstLoadGenerator

```go
type BurstLoadGenerator struct {
    golang.Service
    golang.GeneratesFuncs
    golang.Instantiable

    InstanceName  string
    Wrapped       golang.Service
    BurstSize     int64  // X requests/sec
    BurstDuration string // Y seconds (burst generation period)
    BurstInterval string // Z seconds (wait between burst periods)
}
```

BurstLoadGenerator is a Blueprint IR node representing a burst load generator that runs inside the service and generates bursts independently of business logic.

## Environment Variables

The burst load plugin supports runtime configuration through service-specific environment variables:

### Supported Parameters

- **`{SERVICE_NAME}_BURST_SIZE`**: Number of requests per second during burst periods
- **`{SERVICE_NAME}_BURST_DURATION`**: Duration of each burst period (Go duration format)
- **`{SERVICE_NAME}_BURST_INTERVAL`**: Wait time between burst periods (Go duration format)
- **`{SERVICE_NAME}_BURST_METHODS`**: Comma-separated list of methods to call during burst generation (defaults to first method)

### Examples

```bash
# Configure payment service
PAYMENT_BURST_SIZE=20
PAYMENT_BURST_DURATION=45s
PAYMENT_BURST_INTERVAL=15s
PAYMENT_BURST_METHODS=ProcessPayment,RefundPayment

# Configure user service
USER_BURST_SIZE=8
USER_BURST_DURATION=60s
USER_BURST_INTERVAL=30s
USER_BURST_METHODS=GetUser,CreateUser,UpdateUser
```

### Kubernetes Deployment Example

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: payment-service
spec:
  template:
    spec:
      containers:
      - name: payment-service
        image: myapp/payment-service:latest
        env:
        - name: PAYMENT_BURST_SIZE
          value: "20"
        - name: PAYMENT_BURST_DURATION
          value: "2m"
        - name: PAYMENT_BURST_INTERVAL
          value: "30s"
        - name: PAYMENT_BURST_METHODS
          value: "ProcessPayment,RefundPayment"
```

### Docker Compose Example

```yaml
version: '3.8'
services:
  payment-service:
    image: myapp/payment-service
    environment:
      - PAYMENT_BURST_SIZE=15
      - PAYMENT_BURST_DURATION=45s
      - PAYMENT_BURST_INTERVAL=20s
      - PAYMENT_BURST_METHODS=ProcessPayment,GetBalance
      
  user-service:
    image: myapp/user-service
    environment:
      - USER_BURST_SIZE=8
      - USER_BURST_DURATION=60s
      - USER_BURST_INTERVAL=10s
      - USER_BURST_METHODS=GetUser,CreateUser
```

## Generated Code Behavior

The plugin generates burst load generators that:

1. **Run inside the service**: Burst generation logic is embedded within the service process
2. **Independent of business logic**: Burst generation happens separately from normal service operations
3. **Record real data**: Sample actual call parameters every 200 requests from business logic
4. **Use realistic data**: Generate burst calls with real recorded parameters instead of dummy data
5. **Make real RPC calls**: Generate actual requests to downstream services using real data
6. **Follow burst pattern**: Generate X req/sec for Y seconds, wait Z seconds, repeat
7. **Configurable methods**: Choose which service methods to use for burst generation
8. **Background execution**: Use goroutines for non-blocking burst generation
9. **Graceful lifecycle**: Start with service, stop on shutdown
10. **Runtime configurable**: Override parameters and methods via environment variables

### Burst Pattern Timeline

```
Time:    0s        30s    40s        70s    80s        110s
Period:  [====BURST====][wait][====BURST====][wait][====BURST====]
Rate:    10 req/s       10s    10 req/s       10s    10 req/s
```

### Data Recording and Replay

The generated code implements intelligent data recording:

- **Sampling frequency**: Records real parameters every 200 requests (0.5% overhead)
- **Real data capture**: Stores actual parameters from business logic calls
- **Memory management**: Keeps only last 50 recorded calls to prevent memory issues
- **Realistic burst testing**: Uses real production data for burst generation
- **Method selection**: Configurable via environment variables (defaults to first method)

### Context Usage

The generated code uses context appropriately:

- **Generator lifecycle context**: Controls start/stop of entire generator
- **Burst duration context**: Controls individual burst period timing (EXPECTED timeout)
- **Request contexts**: Fresh context for each RPC call (independent)

## Example Usage

The SockShop application includes a burst load demonstration that shows different burst patterns for different services:

```bash
# Run the SockShop burst load demo
go run examples/sockshop/wiring/main.go burstload_demo
```

This creates:
- **Payment service**: 20 req/sec for 45s, wait 15s (high load)
- **User service**: 10 req/sec for 30s, wait 20s (moderate load)  
- **Order service**: 15 req/sec for 60s, wait 10s (intensive load)
- **Other services**: 5 req/sec for 20s, wait 30s (light load)

You can customize the burst patterns and methods using environment variables:

```bash
# Override payment service burst pattern and methods
PAYMENT_BURST_SIZE=30 \
PAYMENT_BURST_DURATION=2m \
PAYMENT_BURST_INTERVAL=45s \
PAYMENT_BURST_METHODS=ProcessPayment,RefundPayment,GetBalance \
go run examples/sockshop/wiring/main.go burstload_demo
```

### How Data Recording Works

1. **Normal Operation**: Service handles regular business logic calls
2. **Sampling**: Every 200th call gets recorded (parameters stored)
3. **Burst Generation**: Uses recorded real data for realistic burst testing
4. **Example Flow**:
   ```
   Call #1-199: Normal processing
   Call #200: ProcessPayment(1500, "USD") → RECORDED
   Call #201-399: Normal processing  
   Call #400: CreateUser("john@example.com") → RECORDED
   ...
   Burst Generation: Uses real data (1500, "USD") for ProcessPayment bursts
   ```

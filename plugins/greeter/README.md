# Greeter Plugin

The Greeter plugin provides a simple service that can greet users with hello and goodbye messages.

## Usage

To use the Greeter service in your wiring spec:

```go
greeter_service := greeter.Service(spec, "greeter")
```

This will create a new Greeter service instance that can be used by other services in your application.

## Interface

The Greeter service provides the following methods:

```go
type GreeterService interface {
    // SayHello greets a person by name
    SayHello(ctx context.Context, name string) (string, error)
    // SayGoodbye bids farewell to a person by name
    SayGoodbye(ctx context.Context, name string) (string, error)
}
```

## Example

Here's a complete example of how to use the Greeter service in a workflow:

```go
func main() {
    spec := wiring.NewWiringSpec()
    
    // Create the greeter service
    greeter_service := greeter.Service(spec, "greeter")
    
    // Create a workflow service that uses the greeter
    workflow.Service[MyService](spec, "my_service", greeter_service)
    
    // Build and run the application
    app := spec.Build()
    app.Run()
}
```

## Implementation

The Greeter service is implemented as a simple in-memory service in the Blueprint runtime package. The implementation is located in `runtime/plugins/greeter/greeter.go`. 
# Fancy Greeter Plugin

The Fancy Greeter plugin provides a service that extends the basic Greeter service with additional functionality like titles and emotional tones.

## Usage

To use the Fancy Greeter service in your wiring spec:

```go
// First create a basic greeter service
basic_greeter := greeter.Service(spec, "basic_greeter")

// Then create a fancy greeter that uses the basic greeter
fancy_greeter := fancygreeter.Service(spec, "fancy_greeter", basic_greeter)
```

This will create a new Fancy Greeter service instance that can be used by other services in your application.

## Interface

The Fancy Greeter service provides the following methods:

```go
type FancyGreeterService interface {
    // GreetWithTitle greets a person with their name and title
    GreetWithTitle(ctx context.Context, name string, title string) (string, error)
    // FarewellWithEmotion bids farewell with an emotional tone
    FarewellWithEmotion(ctx context.Context, name string, emotion string) (string, error)
}
```

## Example

Here's a complete example of how to use the Fancy Greeter service in a workflow:

```go
func main() {
    spec := wiring.NewWiringSpec()
    
    // Create the basic greeter service
    basic_greeter := greeter.Service(spec, "basic_greeter")
    
    // Create the fancy greeter service that uses the basic greeter
    fancy_greeter := fancygreeter.Service(spec, "fancy_greeter", basic_greeter)
    
    // Create a workflow service that uses the fancy greeter
    workflow.Service[MyService](spec, "my_service", fancy_greeter)
    
    // Build and run the application
    app := spec.Build()
    app.Run()
}
```

## Implementation

The Fancy Greeter service is implemented as a wrapper around the basic Greeter service in the Blueprint runtime package. The implementation is located in `runtime/plugins/fancygreeter/fancygreeter.go`.

The service demonstrates:
1. Service composition through dependency injection
2. Extending basic functionality with additional features
3. How to create compound services that build on top of simpler services 
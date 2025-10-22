# Greeter Microservices Example

This is a simple Blueprint example that demonstrates a basic microservices architecture with two services:
- A basic greeter service that provides simple greeting functionality
- A fancy greeter service that extends the basic greeter with additional features

## Getting Started

Before running the example applications, make sure you have installed the recommended [prerequisites](../../docs/manual/requirements.md).

## Compiling the Application

To compile the application, we execute `wiring/main.go` and specify which wiring spec to compile. To view options and list wiring specs, run:

```bash
go run wiring/main.go -h
```

The following will compile the `docker` wiring spec to the directory `build`. This will fail if the pre-requisite gRPC and protocol buffers compilers aren't installed.

```bash
go run wiring/main.go -o build -w docker
```

If you navigate to the `build` directory, you will now see a number of build artifacts:
* `build/docker` contains docker images for the various containers of the application, as well as a `docker-compose.yml` file for starting and stopping all containers
* `build/docker/*` contain the individual docker images for services, including a Dockerfile and the golang source code
* `build/gotests` contain the unit tests

## Configure and Run the Application

To run the application, we will need to set a number of environment variables. Blueprint will generate a `.local.env` file with some default values for these;
you can modify them if necessary.

Set the docker environment variables:

```bash
cd build
cp .local.env docker/.env
```

If this is your first time, you will need to build the containers:

```bash
cd docker
docker compose build
```

Run the application:

```bash
docker compose up
```

## Invoke the Application

The Fancy Greeter service is exposed via HTTP. The port at which the service will be exposed is determined by the value of the variable `FANCYGREETER_HTTP_DIAL_ADDR` in the generated `.local.env` file.

For example, the value of the variable might be declared as `FANCYGREETER_HTTP_DIAL_ADDR=localhost:12356`.

You can invoke the service using curl:

```bash
# Greet with a title
curl -X POST http://localhost:12356/GreetWithTitle \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice", "title": "Dr."}'

# Farewell with emotion
curl -X POST http://localhost:12356/FarewellWithEmotion \
  -H "Content-Type: application/json" \
  -d '{"name": "Bob", "emotion": "with joy"}'
```

Alternatively, you can use the generated client code in `build/golang/golang/main.go` to interact with the service programmatically.

## Changing the Application's Wiring Spec

The Greeter application comes with a number of out-of-the-box configurations; run `main.go` with the `-h` flag to list them, or view the documentation for the [wiring/specs](wiring/specs) package.

As a starting point for implementing your own custom wiring spec, we recommend duplicating and building off of the [basic.go](wiring/specs/basic.go) wiring spec. After implementing your spec,
make sure that you add it to [wiring/main.go](wiring/main.go) so that it can be selected on the command line. 
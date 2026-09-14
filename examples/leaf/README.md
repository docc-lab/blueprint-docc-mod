# leaf application

Leaf is a simple application that is not representative of a real application.  Its intended use is to provide examples of Blueprint API usage; it is frequently referenced by plugin documentation.

For synthetic service graphs with arbitrary fan-out and configurable concurrency,
see the [configurable fan-out example](FANOUT.md). Its `docker_fanout` wiring spec
accepts JSON or YAML topologies with nested branches, shared downstream services,
and composed sequences, parallel groups, repetitions, and delays.

For a more realistic application to run, look at the [sockshop](../sockshop) or [dsb_sn](../dsb_sn), or other applications listed in the [examples](..) directory.

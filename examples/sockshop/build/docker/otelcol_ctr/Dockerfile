FROM otel/opentelemetry-collector

# Copy the configuration file
COPY config.yaml /etc/otelcol/config.yaml

# Set the command to use the configuration file
CMD ["--config", "/etc/otelcol/config.yaml"]

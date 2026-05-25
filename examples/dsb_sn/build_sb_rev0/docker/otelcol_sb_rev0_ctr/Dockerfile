FROM 10.10.1.1:30000/otelcontribcol:latest

# Copy the configuration file
COPY config.yaml /etc/otelcol/config.yaml

# Set the command to use the configuration file
CMD ["--config", "/etc/otelcol/config.yaml"]

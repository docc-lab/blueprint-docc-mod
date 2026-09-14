FROM scratch
COPY spanload /spanload
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
ENTRYPOINT ["/spanload"]

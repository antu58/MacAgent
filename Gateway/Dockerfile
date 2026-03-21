FROM scratch

WORKDIR /app
COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY build/gateway /app/gateway
COPY default_intents.json /app/default_intents.json

EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/app/gateway"]

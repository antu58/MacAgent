FROM scratch

WORKDIR /app
COPY build/gateway /app/gateway
COPY default_intents.json /app/default_intents.json

EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/app/gateway"]

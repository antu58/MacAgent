FROM scratch

WORKDIR /app
COPY build/gateway-web /app/gateway-web
COPY static /app/static

EXPOSE 19091
USER 65532:65532
ENTRYPOINT ["/app/gateway-web"]

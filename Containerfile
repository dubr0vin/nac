FROM node:latest AS frontend
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:latest AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY --from=frontend /src/web/dist ./web/dist
RUN CGO_ENABLED=1 go build -trimpath -o /nac .

FROM debian:stable-slim
LABEL org.opencontainers.image.source=https://github.com/dubr0vin/nac
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir /data && chown 65532:65532 /data
COPY --from=backend /nac /usr/local/bin/nac
USER 65532:65532
ENV NAC_ADDR=0.0.0.0:8080 NAC_DB=/data/nac.db
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/nac"]

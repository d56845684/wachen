FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN go mod tidy && CGO_ENABLED=0 go build -o /out/scheduler ./cmd/scheduler \
    && CGO_ENABLED=0 go build -o /out/worker ./cmd/worker \
    && CGO_ENABLED=0 go build -o /out/mockgoogle ./cmd/mockgoogle \
    && CGO_ENABLED=0 go build -o /out/ingestion ./cmd/ingestion \
    && CGO_ENABLED=0 go build -o /out/webhook ./cmd/webhook

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata curl
COPY --from=build /out/ /bin/
# 預設跑 worker；scheduler / mockgoogle 由 compose command 覆蓋
CMD ["/bin/worker"]

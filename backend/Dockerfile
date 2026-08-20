FROM golang:1.26-alpine AS builder

RUN apk add --no-cache gcc libpcap-dev musl-dev
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/contest-monitor .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates libpcap
RUN addgroup -S monitor && adduser -S -G monitor monitor
WORKDIR /app

COPY --from=builder /out/contest-monitor /app/contest-monitor

EXPOSE 8081
USER monitor
ENTRYPOINT ["/app/contest-monitor"]

FROM golang:alpine AS builder

RUN apk add --no-cache libpcap-dev gcc musl-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o main .

FROM alpine:latest
RUN apk add --no-cache libpcap
WORKDIR /app
COPY --from=builder /app/main .
EXPOSE 8080
CMD ["./main"]
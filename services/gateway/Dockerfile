FROM golang:1.23-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -o /robin-gateway .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata curl sqlite-libs
COPY --from=builder /robin-gateway /robin-gateway
EXPOSE 8080
CMD ["/robin-gateway"]

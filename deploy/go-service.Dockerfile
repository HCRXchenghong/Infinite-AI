FROM golang:1.25.6-alpine AS builder

WORKDIR /app
ENV GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG SERVICE_PATH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/service ${SERVICE_PATH}

FROM alpine:3.22

WORKDIR /app

COPY --from=builder /out/service /app/service
COPY db /app/db
COPY .env.example /app/.env.example

EXPOSE 1003 1004 1005

ENTRYPOINT ["/app/service"]

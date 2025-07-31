# ---- Build stage ----
FROM golang:1.22 AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o service-b .

# ---- Runtime stage ----
FROM gcr.io/distroless/base-debian12
WORKDIR /
COPY --from=builder /app/service-b /service-b
ENV PORT=8081
EXPOSE 8081
ENTRYPOINT ["/service-b"]

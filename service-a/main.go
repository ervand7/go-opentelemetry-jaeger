package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ServiceBResponse struct {
	Service   string `json:"service"`
	Operation string `json:"operation"`
	Name      string `json:"name"`
	Message   string `json:"message"`
}

type CombinedResponse struct {
	Service       string           `json:"service"`        // service-a
	CalledService ServiceBResponse `json:"called_service"` // nested service-b response
}

func main() {
	ctx := context.Background()

	port := getenv("PORT", "8080")
	serviceName := getenv("OTEL_SERVICE_NAME", "service-a")
	otelEndpoint := getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otel-collector:4317")

	shutdown, err := setupTracerProvider(ctx, serviceName, otelEndpoint)
	if err != nil {
		log.Fatalf("failed to init tracer provider: %v", err)
	}
	defer func() { _ = shutdown(ctx) }()

	// HTTP client with otel transport for outgoing requests (creates client spans + injects context)
	client := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   5 * time.Second,
	}

	mux := http.NewServeMux()
	mux.Handle("/hello", otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context() // carries the current span context

		name := r.URL.Query().Get("name")
		if name == "" {
			name = "World"
		}

		// Call service-b with context propagation
		req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://service-b:8081/work?name=%s", name), nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// client spans will be created by the otelhttp transport
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, fmt.Sprintf("service-b error: %s", err.Error()), http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()

		var serviceBResp ServiceBResponse
		if err := json.NewDecoder(resp.Body).Decode(&serviceBResp); err != nil {
			http.Error(w, "invalid JSON from service-b", http.StatusInternalServerError)
			return
		}

		result := CombinedResponse{
			Service:       "service-a",
			CalledService: serviceBResp,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}), "hello"))

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	log.Printf("service-a listening on :%s", port)
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func setupTracerProvider(ctx context.Context, serviceName, otelEndpoint string) (func(ctx context.Context) error, error) {
	driver := otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint(otelEndpoint),
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	exp, err := otlptrace.New(ctx, driver)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, err
	}

	bsp := sdktrace.NewBatchSpanProcessor(exp)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

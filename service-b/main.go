package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type response struct {
	Service   string `json:"service"`
	Operation string `json:"operation"`
	Name      string `json:"name"`
	Message   string `json:"message"`
}

func main() {
	ctx := context.Background()

	port := getenv("PORT", "8081")
	serviceName := getenv("OTEL_SERVICE_NAME", "service-b")
	otelEndpoint := getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otel-collector:4317")

	shutdown, err := setupTracerProvider(ctx, serviceName, otelEndpoint)
	if err != nil {
		log.Fatalf("failed to init tracer provider: %v", err)
	}
	defer func() { _ = shutdown(ctx) }()

	mux := http.NewServeMux()
	mux.Handle("/work", otelhttp.NewHandler(http.HandlerFunc(workHandler), "work"))

	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	log.Printf("service-b listening on :%s", port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
}

func workHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tr := otel.Tracer("service-b")
	_, span := tr.Start(ctx, "businessLogic")
	defer span.End()

	name := r.URL.Query().Get("name")
	if name == "" {
		name = "World"
	}

	// Simulate some work
	sleep := time.Duration(50+rand.Intn(100)) * time.Millisecond
	time.Sleep(sleep)
	span.SetAttributes(attribute.Int64("simulated.sleep_ms", int64(sleep/time.Millisecond)))

	// Simulate nested span (e.g., DB)
	dbCtx, dbSpan := tr.Start(ctx, "mockDB")
	time.Sleep(time.Duration(20+rand.Intn(40)) * time.Millisecond)
	dbSpan.End()

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(response{
		Service:   "service-b",
		Operation: "work",
		Name:      name,
		Message:   "Hello from service-b",
	})
	_ = dbCtx // demonstrate nested span usage
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

# OpenTelemetry - Jaeger Tracing
Two Go microservices (`service-a` and `service-b`) with **OpenTelemetry** tracing enabled.  
Traffic flows: **client → service-a → service-b**. Traces are exported via **OTLP** to the **OpenTelemetry Collector**, then sent to **Jaeger** for visualization.

## Architecture
```
[You] --> http://localhost:8080/hello?name=Alice
   |
   v
 service-a (HTTP server, OTel tracing + context propagation)
   |
   v (HTTP client with otelhttp)
 service-b (HTTP server, OTel tracing)
   |
   v
 OTel Collector (receives OTLP traces on :4317)
   |
   v
 Jaeger (UI on http://localhost:16686)
```

## Quick start
```bash
# From repository root
docker compose up --build
```

In another terminal:
```bash
curl "http://localhost:8080/hello?name=Alice"
```

Open Jaeger UI:
- **http://localhost:16686/**
  - Find service: `service-a` or `service-b`
  - Trace should show: `/hello` (service-a) → `/work` (service-b) with nested spans

## How it works (key points)
1. **Instrumentation & Tracer Provider**
   - Each service sets up a Tracer Provider with an **OTLP exporter** pointing to `otel-collector:4317`.
   - A `service.name` resource attribute distinguishes services.

2. **HTTP Server Instrumentation**
   - We wrap handlers with `otelhttp.NewHandler(…, "route-name")` to automatically create **server spans**.

3. **HTTP Client Instrumentation**
   - We use `http.Client{ Transport: otelhttp.NewTransport(http.DefaultTransport) }` to create **client spans** and **inject** W3C Trace Context headers into the outbound request.

4. **Context propagation**
   - The incoming request `Context` carries trace context; pass it downstream and to HTTP calls so spans are properly linked.

5. **Collector → Jaeger**
   - The OpenTelemetry Collector receives traces from the services and exports them to Jaeger. Jaeger provides the UI and storage for traces.

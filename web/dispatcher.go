package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

var tracer = otel.Tracer("wordsmith-web")

func main() {
	rand.Seed(time.Now().UnixNano())

	shutdown := initTracer()
	defer shutdown(context.Background())

	apiHost := os.Getenv("API_HOST")
	if apiHost == "" {
		apiHost = "api.wordsmith.svc.cluster.local"
	}
	fwd := &forwarder{apiHost, 8080}
	http.Handle("/words/", http.StripPrefix("/words", fwd))
	http.Handle("/", http.FileServer(http.Dir("static")))

	fmt.Println("Listening on port 80")
	http.ListenAndServe(":80", nil)
}

func initTracer() func(context.Context) error {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "telemetry-ingest.dynatrace.svc.cluster.local:4317"
	}

	ctx := context.Background()
	exporter, err := otlptracegrpc.New(
		ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		log.Printf("failed to create exporter: %v", err)
		return func(context.Context) error { return nil }
	}

	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName("wordsmith-web")))
	if err != nil {
		log.Printf("failed to create resource: %v", err)
		return func(context.Context) error { return nil }
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	return provider.Shutdown
}

type forwarder struct {
	host string
	port int
}

func (f *forwarder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "wordsmith.web.forward")
	defer span.End()

	span.SetAttributes(attribute.String("http.route", r.URL.Path))

	addrs, err := net.LookupHost(f.host)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		log.Println("Error", err)
		http.Error(w, err.Error(), 500)
		return
	}

	log.Printf("%s %d available ips: %v", r.URL.Path, len(addrs), addrs)
	ip := addrs[rand.Intn(len(addrs))]
	log.Printf("%s I choose %s", r.URL.Path, ip)
	span.SetAttributes(attribute.String("upstream.host", ip))

	url := fmt.Sprintf("http://%s:%d%s", ip, f.port, r.URL.Path)
	log.Printf("%s Calling %s", r.URL.Path, url)

	if err = copy(ctx, url, ip, w); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		log.Println("Error", err)
		http.Error(w, err.Error(), 500)
		return
	}
}

func copy(ctx context.Context, url, ip string, w http.ResponseWriter) error {
	resp, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	clientResp, err := http.DefaultClient.Do(resp)
	if err != nil {
		return err
	}
	defer clientResp.Body.Close()

	for header, values := range clientResp.Header {
		for _, value := range values {
			w.Header().Add(header, value)
		}
	}
	w.Header().Set("source", ip)

	buf, err := io.ReadAll(clientResp.Body)
	if err != nil {
		return err
	}

	_, err = w.Write(buf)
	return err
}

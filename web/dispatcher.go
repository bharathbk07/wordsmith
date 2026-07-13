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
	"strings"
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
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.FileServer(http.Dir("static")).ServeHTTP(w, r)
			return
		}
		serveIndex(w, r)
	})

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

func serveIndex(w http.ResponseWriter, r *http.Request) {
	content, err := os.ReadFile("static/index.html")
	if err != nil {
		log.Printf("failed to read index template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rumScript := os.Getenv("DYNATRACE_RUM_SCRIPT_URL")
	rumAppID := os.Getenv("DYNATRACE_RUM_APP_ID")
	if rumAppID == "" {
		rumAppID = "wordsmith-web"
	}

	body := string(content)
	body = strings.ReplaceAll(body, "__DYNATRACE_RUM_APP_ID__", rumAppID)
	body = strings.ReplaceAll(body, "__DYNATRACE_RUM_SCRIPT_URL__", rumScript)
	w.Header().Set("content-type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func (f *forwarder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	route := strings.TrimPrefix(r.URL.Path, "/")
	if route == "" {
		route = "root"
	}
	spanName := r.URL.Path
	ctx, span := tracer.Start(r.Context(), spanName)
	defer span.End()

	span.SetAttributes(
		attribute.String("http.method", r.Method),
		attribute.String("http.route", r.URL.Path),
		attribute.String("http.target", r.URL.RequestURI()),
		attribute.String("http.scheme", "http"),
	)

	addrs, err := net.LookupHost(f.host)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		log.Println("Error", err)
		http.Error(w, err.Error(), 500)
		return
	}

	log.Printf("%s %d available ips: %v", r.URL.Path, len(addrs), addrs)

	// Attempt connection to the resolved IPs in a randomized order to provide resiliency
	perm := rand.Perm(len(addrs))
	var lastErr error
	for _, idx := range perm {
		ip := addrs[idx]
		span.SetAttributes(attribute.String("upstream.host", ip))
		url := fmt.Sprintf("http://%s:%d%s", ip, f.port, r.URL.Path)
		log.Printf("%s calling %s", r.URL.Path, url)

		if lastErr = copy(ctx, url, ip, w); lastErr == nil {
			return
		}
		log.Printf("Error calling %s: %v. Retrying other IPs...", ip, lastErr)
	}

	span.RecordError(lastErr)
	span.SetStatus(codes.Error, lastErr.Error())
	log.Println("All upstream hosts failed. Last error:", lastErr)
	http.Error(w, lastErr.Error(), 500)
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

import com.google.common.base.Charsets;
import com.google.common.base.Supplier;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpHandler;
import com.sun.net.httpserver.HttpServer;
import io.opentelemetry.api.GlobalOpenTelemetry;
import io.opentelemetry.api.common.AttributeKey;
import io.opentelemetry.api.common.Attributes;
import io.opentelemetry.api.trace.Span;
import io.opentelemetry.api.trace.SpanKind;
import io.opentelemetry.api.trace.StatusCode;
import io.opentelemetry.api.trace.Tracer;
import io.opentelemetry.context.Scope;
import io.opentelemetry.exporter.otlp.trace.OtlpGrpcSpanExporter;
import io.opentelemetry.sdk.OpenTelemetrySdk;
import io.opentelemetry.sdk.resources.Resource;
import io.opentelemetry.sdk.trace.SdkTracerProvider;
import io.opentelemetry.sdk.trace.export.BatchSpanProcessor;

import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.sql.Connection;
import java.sql.DriverManager;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.Statement;
import java.util.NoSuchElementException;

public class Main {
    private static final Tracer tracer = configureOpenTelemetry();

    public static void main(String[] args) throws Exception {
        Class.forName("org.postgresql.Driver");

        HttpServer server = HttpServer.create(new InetSocketAddress(8080), 0);
        server.createContext("/noun", handler(() -> randomWord("nouns"), "nouns"));
        server.createContext("/verb", handler(() -> randomWord("verbs"), "verbs"));
        server.createContext("/adjective", handler(() -> randomWord("adjectives"), "adjectives"));
        server.start();
    }

    private static Tracer configureOpenTelemetry() {
        String endpoint = System.getenv().getOrDefault("OTEL_EXPORTER_OTLP_ENDPOINT", "telemetry-ingest.dynatrace.svc.cluster.local:4317");
        String exporterEndpoint = endpoint.startsWith("http://") || endpoint.startsWith("https://") ? endpoint : "http://" + endpoint;
        Resource resource = Resource.getDefault().merge(Resource.create(Attributes.of(AttributeKey.stringKey("service.name"), "wordsmith-api")));
        SdkTracerProvider tracerProvider = SdkTracerProvider.builder()
                .setResource(resource)
                .addSpanProcessor(BatchSpanProcessor.builder(OtlpGrpcSpanExporter.builder().setEndpoint(exporterEndpoint).build()).build())
                .build();
        OpenTelemetrySdk sdk = OpenTelemetrySdk.builder().setTracerProvider(tracerProvider).build();
        GlobalOpenTelemetry.set(sdk);
        return sdk.getTracer("wordsmith-api");
    }

    private static String randomWord(String table) {
        try (Connection connection = DriverManager.getConnection("jdbc:postgresql://db:5432/postgres", "postgres", "")) {
            try (Statement statement = connection.createStatement()) {
                try (ResultSet set = statement.executeQuery("SELECT word FROM " + table + " ORDER BY random() LIMIT 1")) {
                    while (set.next()) {
                        return set.getString(1);
                    }
                }
            }
        } catch (SQLException e) {
            e.printStackTrace();
        }

        throw new NoSuchElementException(table);
    }

    private static HttpHandler handler(Supplier<String> word, String table) {
        return exchange -> {
            Span span = tracer.spanBuilder("wordsmith.api.word").setSpanKind(SpanKind.SERVER).startSpan();
            try (Scope scope = span.makeCurrent()) {
                span.setAttribute("word.table", table);
                String response = "{\"word\":\"" + word.get() + "\"}";
                byte[] bytes = response.getBytes(Charsets.UTF_8);

                System.out.println(response);

                exchange.getResponseHeaders().add("content-type", "application/json; charset=utf-8");
                exchange.getResponseHeaders().add("cache-control", "private, no-cache, no-store, must-revalidate, max-age=0");
                exchange.getResponseHeaders().add("pragma", "no-cache");

                exchange.sendResponseHeaders(200, bytes.length);

                try (OutputStream os = exchange.getResponseBody()) {
                    os.write(bytes);
                }
            } catch (Exception e) {
                span.recordException(e);
                span.setStatus(StatusCode.ERROR, e.getMessage());
                throw e;
            } finally {
                span.end();
            }
        };
    }
}

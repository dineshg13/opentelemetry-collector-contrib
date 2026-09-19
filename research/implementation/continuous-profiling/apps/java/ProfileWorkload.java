// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
import com.sun.net.httpserver.HttpServer;
import java.net.InetSocketAddress;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.file.Files;
import java.nio.file.Path;

public class ProfileWorkload {
  private static volatile boolean running = true;
  private static volatile double checksum;

  private static void profileHotLoop() {
    double value = 0;
    for (int i = 0; i < 400000; i++) value += Math.sqrt(i);
    checksum += value;
  }

  public static void main(String[] args) throws Exception {
    boolean tracing = "true".equals(System.getenv("DD_TRACE_ENABLED"));
    if (tracing && !"otlp".equals(System.getenv("DD_TRACE_OTEL_EXPORTER"))) {
      throw new IllegalArgumentException("This application only permits OTLP trace export");
    }
    Runtime.getRuntime().addShutdownHook(new Thread(() -> running = false));
    var server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
    server.createContext("/work", exchange -> {
      for (int i = 0; i < 25; i++) profileHotLoop();
      byte[] body = "profile workload".getBytes();
      exchange.sendResponseHeaders(200, body.length);
      exchange.getResponseBody().write(body);
      exchange.close();
    });
    server.start();
    var client = HttpClient.newHttpClient();
    var request = HttpRequest.newBuilder(URI.create("http://127.0.0.1:" + server.getAddress().getPort() + "/work")).build();
    System.out.println("{\"language\":\"java\",\"sdk\":\"1.66.0\",\"otlp_tracing\":" + tracing + "}");
    Files.writeString(Path.of("/tmp/ready"), "");
    long duration = Long.parseLong(System.getenv().getOrDefault("WORKLOAD_SECONDS", "0"));
    long deadline = duration == 0 ? Long.MAX_VALUE : System.nanoTime() + duration * 1_000_000_000L;
    while (running && System.nanoTime() < deadline) {
      client.send(request, HttpResponse.BodyHandlers.discarding());
      Thread.sleep(20);
    }
    server.stop(0);
    System.out.println("{\"complete\":true,\"checksum_positive\":" + (checksum > 0) + "}");
  }
}

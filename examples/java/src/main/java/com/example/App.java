package com.example;

import dev.dbos.transact.DBOS;
import dev.dbos.transact.config.DBOSConfig;
import dev.dbos.transact.workflow.Step;
import dev.dbos.transact.workflow.Workflow;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpHandler;
import com.sun.net.httpserver.HttpServer;

import java.io.IOException;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.sql.Connection;
import java.sql.DriverManager;
import java.sql.PreparedStatement;

public class App {

    public interface OrderService {
        String step1(String orderId);
        String step2(String orderId);
        String orderWorkflow(String orderId);
    }

    public static class OrderServiceImpl implements OrderService {
        private final String dbUrl;
        private OrderService proxy;

        public OrderServiceImpl(String dbUrl) {
            this.dbUrl = dbUrl;
        }

        public void setProxy(OrderService proxy) {
            this.proxy = proxy;
        }

        private void recordStep(String stepName) {
            String wfId = DBOS.workflowId();
            if (wfId == null) {
                wfId = "unknown";
            }
            try (Connection conn = DriverManager.getConnection(dbUrl, "relay", "relay")) {
                try (PreparedStatement stmt = conn.prepareStatement(
                        "CREATE TABLE IF NOT EXISTS test_step_executions (" +
                        "workflow_id TEXT NOT NULL, " +
                        "step_name TEXT NOT NULL, " +
                        "executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()" +
                        ")")) {
                    stmt.execute();
                }
                try (PreparedStatement stmt = conn.prepareStatement(
                        "INSERT INTO test_step_executions (workflow_id, step_name, executed_at) VALUES (?, ?, NOW())")) {
                    stmt.setString(1, wfId);
                    stmt.setString(2, stepName);
                    stmt.executeUpdate();
                }
            } catch (Exception e) {
                System.err.println("Failed to record step execution: " + e.getMessage());
            }
        }

        @Override
        @Step
        public String step1(String orderId) {
            recordStep("step1");
            return "step1-completed";
        }

        @Override
        @Step
        public String step2(String orderId) {
            recordStep("step2");
            return "step2-completed";
        }

        @Override
        @Workflow
        public String orderWorkflow(String orderId) {
            proxy.step1(orderId);
            String role = System.getenv("ROLE");
            if ("primary".equalsIgnoreCase(role)) {
                try {
                    // Sleep for 30 minutes to stay in-flight during chaos kill and offline mutation tests
                    Thread.sleep(1800000);
                } catch (InterruptedException ignored) {
                    Thread.currentThread().interrupt();
                }
            }
            proxy.step2(orderId);
            return "order-" + orderId + "-completed";
        }
    }

    public static void main(String[] args) throws Exception {
        String appName = System.getenv("DBOS_APP_NAME");
        if (appName == null || appName.isEmpty()) {
            appName = "java-sample-app";
        }
        String relayURL = System.getenv("RELAY_URL");
        if (relayURL == null || relayURL.isEmpty()) {
            relayURL = "http://localhost:8090";
        }
        String apiKey = System.getenv("RELAY_API_KEY");
        if (apiKey == null) {
            apiKey = "";
        }
        String dbUrl = System.getenv("DBOS_SYSTEM_DATABASE_URL");
        if (dbUrl == null || dbUrl.isEmpty()) {
            dbUrl = "postgres://relay:relay@postgres:5432/relay?sslmode=disable";
        }

        String jdbcUrl = dbUrl;
        if (jdbcUrl.startsWith("postgres://")) {
            jdbcUrl = jdbcUrl.replace("postgres://", "jdbc:postgresql://");
        } else if (jdbcUrl.startsWith("postgresql://")) {
            jdbcUrl = jdbcUrl.replace("postgresql://", "jdbc:postgresql://");
        }
        jdbcUrl = jdbcUrl.replaceAll("//[^@]+@", "//");

        DBOSConfig config = DBOSConfig.defaults(appName)
                .withDatabaseUrl(jdbcUrl)
                .withDbUser("relay")
                .withDbPassword("relay")
                .withConductorKey(apiKey)
                .withConductorDomain(relayURL)
                .withMigrate(true);

        DBOS dbos = new DBOS(config);
        OrderServiceImpl serviceImpl = new OrderServiceImpl(jdbcUrl);
        OrderService proxy = dbos.registerProxy(OrderService.class, serviceImpl);
        serviceImpl.setProxy(proxy);

        int httpPort = 8083;
        String portEnv = System.getenv("HTTP_PORT");
        if (portEnv != null) {
            try {
                httpPort = Integer.parseInt(portEnv);
            } catch (NumberFormatException ignored) {}
        }

        HttpServer server = HttpServer.create(new InetSocketAddress(httpPort), 0);
        server.createContext("/trigger", new HttpHandler() {
            @Override
            public void handle(HttpExchange exchange) throws IOException {
                try {
                    var handle = dbos.startWorkflow(() -> proxy.orderWorkflow("java-order"));
                    String resp = "{\"workflow_id\":\"" + handle.workflowId() + "\"}";
                    exchange.getResponseHeaders().set("Content-Type", "application/json");
                    byte[] bytes = resp.getBytes("UTF-8");
                    exchange.sendResponseHeaders(200, bytes.length);
                    try (OutputStream os = exchange.getResponseBody()) {
                        os.write(bytes);
                    }
                } catch (Exception e) {
                    String err = "{\"error\":\"" + e.getMessage() + "\"}";
                    byte[] bytes = err.getBytes("UTF-8");
                    exchange.sendResponseHeaders(500, bytes.length);
                    try (OutputStream os = exchange.getResponseBody()) {
                        os.write(bytes);
                    }
                }
            }
        });
        server.createContext("/fork", new HttpHandler() {
            @Override
            public void handle(HttpExchange exchange) throws IOException {
                try {
                    var handle = dbos.startWorkflow(() -> proxy.orderWorkflow("java-fork"));
                    String resp = "{\"workflow_id\":\"" + handle.workflowId() + "\"}";
                    exchange.getResponseHeaders().set("Content-Type", "application/json");
                    byte[] bytes = resp.getBytes("UTF-8");
                    exchange.sendResponseHeaders(200, bytes.length);
                    try (OutputStream os = exchange.getResponseBody()) {
                        os.write(bytes);
                    }
                } catch (Exception e) {
                    String err = "{\"error\":\"" + e.getMessage() + "\"}";
                    byte[] bytes = err.getBytes("UTF-8");
                    exchange.sendResponseHeaders(500, bytes.length);
                    try (OutputStream os = exchange.getResponseBody()) {
                        os.write(bytes);
                    }
                }
            }
        });
        server.createContext("/health", new HttpHandler() {
            @Override
            public void handle(HttpExchange exchange) throws IOException {
                byte[] bytes = "OK".getBytes("UTF-8");
                exchange.sendResponseHeaders(200, bytes.length);
                try (OutputStream os = exchange.getResponseBody()) {
                    os.write(bytes);
                }
            }
        });
        server.setExecutor(null);

        String role = System.getenv("ROLE");
        if ("secondary".equalsIgnoreCase(role)) {
            try {
                // Allow primary to complete initial database migrations/setup
                Thread.sleep(3000);
            } catch (InterruptedException ignored) {}
        }

        dbos.launch();
        System.out.println("DBOS Java sample application launched successfully for app " + appName);

        server.start();

        Thread.currentThread().join();
    }
}

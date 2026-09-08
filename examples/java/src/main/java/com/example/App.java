package com.example;

import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.WebSocket;
import java.time.Instant;
import java.util.UUID;
import java.util.concurrent.CompletionStage;
import java.util.concurrent.CountDownLatch;

public class App {
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
            apiKey = "test-key";
        }

        String execID = "exec-java-" + UUID.randomUUID().toString().substring(0, 8);
        String wsScheme = relayURL.startsWith("wss") ? "wss" : "ws";
        String hostPart = relayURL.replaceFirst("^(https?|wss?)://", "");
        String wsUri = wsScheme + "://" + hostPart + "/websocket/" + appName + "/" + apiKey;

        // Standard SDK launch log format
        System.out.println("time=" + Instant.now().toString() + " level=INFO msg=\"DBOS launched\" app_version=v1.0.0 executor_id=" + execID + " language=java");
        System.out.flush();

        HttpClient client = HttpClient.newHttpClient();
        CountDownLatch latch = new CountDownLatch(1);

        client.newWebSocketBuilder()
            .buildAsync(URI.create(wsUri), new WebSocket.Listener() {
                private final StringBuilder buffer = new StringBuilder();

                @Override
                public void onOpen(WebSocket webSocket) {
                    webSocket.request(1);
                }

                @Override
                public CompletionStage<?> onText(WebSocket webSocket, CharSequence data, boolean last) {
                    buffer.append(data);
                    if (last) {
                        String msg = buffer.toString();
                        buffer.setLength(0);
                        handleMessage(webSocket, msg, execID);
                    }
                    webSocket.request(1);
                    return null;
                }

                @Override
                public CompletionStage<?> onClose(WebSocket webSocket, int statusCode, String reason) {
                    latch.countDown();
                    return null;
                }

                @Override
                public void onError(WebSocket webSocket, Throwable error) {
                    System.err.println("WebSocket error: " + error.getMessage());
                    latch.countDown();
                }
            }).join();

        latch.await();
    }

    private static void handleMessage(WebSocket ws, String msg, String execID) {
        try {
            if (msg.contains("\"type\":\"executor_info\"") || msg.contains("\"type\": \"executor_info\"")) {
                String reqId = extractField(msg, "request_id");
                String resp = "{\"type\":\"executor_info\",\"request_id\":\"" + reqId + "\",\"executor_id\":\"" + execID + "\",\"app_version\":\"v1.0.0\",\"language\":\"java\",\"dbos_version\":\"0.1.0\",\"hostname\":\"localhost\"}";
                ws.sendText(resp, true);
            } else if (msg.contains("\"type\":\"get_workflow\"") || msg.contains("\"type\": \"get_workflow\"")) {
                String reqId = extractField(msg, "request_id");
                String wfId = extractField(msg, "workflow_id");
                String resp = "{\"type\":\"get_workflow\",\"request_id\":\"" + reqId + "\",\"output\":{\"WorkflowUUID\":\"" + wfId + "\",\"Status\":\"SUCCESS\",\"WorkflowName\":\"helloWorkflow\",\"ApplicationVersion\":\"v1.0.0\"}}";
                ws.sendText(resp, true);
            }
        } catch (Exception e) {
            System.err.println("Failed handling message: " + e.getMessage());
        }
    }

    private static String extractField(String json, String field) {
        String pattern = "\"" + field + "\":\"";
        int idx = json.indexOf(pattern);
        if (idx != -1) {
            int start = idx + pattern.length();
            int end = json.indexOf("\"", start);
            if (end != -1) {
                return json.substring(start, end);
            }
        }
        return "req-id";
    }
}

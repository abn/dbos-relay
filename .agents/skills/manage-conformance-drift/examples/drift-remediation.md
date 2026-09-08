# Conformance Drift refinement Example

Concrete walkthrough illustrating how an agent detects, scopes, tests, and
fixes a protocol conformance discrepancy between Relay and DBOS Conductor.

---

## Scenario: Upstream SDK Introduces New Field in `ListWorkflowsResponse`

Suppose a new release of `dbos-transact-go` adds an optional `ExecutionTimeMs`
field to `ListWorkflowsResponseBody`. Conformance check `3.1` fails or produces an
unmarshaling error.

---

## Step 1: Detect Drift

Run the conformance suite via the CLI or Make:

```bash
$ ./bin/relay test-conformance --target http://localhost:8090 --key "$RELAY_API_KEY"

[RUN] Battery 3: REST & Wire Multiplexing (Observability)...
[FAIL] Battery 3: REST & Wire Multiplexing - 3.1 List Workflows: field ExecutionTimeMs missing or malformed
```

---

## Step 2: Verify Provenance in Permitted Source

Check the open-source SDK repository to inspect the exact wire type definition:

File: `dbos-transact-go/dbos/conductor_protocol.go`
```go
type ListWorkflowsResponseBody struct {
    WorkflowUUID    string  `json:"WorkflowUUID"`
    Status          *string `json:"Status,omitempty"`
    WorkflowName    *string `json:"WorkflowName,omitempty"`
    ExecutionTimeMs *int64  `json:"ExecutionTimeMs,omitempty"` // Added field
}
```

Provenance citation confirmed:
- Source: `github.com/dbos-inc/dbos-transact-go`
- File: `dbos/conductor_protocol.go`
- Commit: `<upstream-commit-hash>`

---

## Step 3: Write Failing Test First

Add a test case to `internal/protocol/codec_test.go` or `tests/conformance/`:

```go
func TestListWorkflowsResponseWithExecutionTime(t *testing.T) {
    wire := []byte(`{
        "type": "list_workflows",
        "request_id": "req-1",
        "output": [{
            "WorkflowUUID": "wf-1",
            "ExecutionTimeMs": 1500
        }]
    }`)

    msg, err := protocol.Decode(wire)
    if err != nil {
        t.Fatalf("Decode failed: %v", err)
    }

    resp, ok := msg.(*protocol.ListWorkflowsResponse)
    if !ok {
        t.Fatalf("expected *protocol.ListWorkflowsResponse, got %T", msg)
    }

    if resp.Output[0].ExecutionTimeMs == nil || *resp.Output[0].ExecutionTimeMs != 1500 {
        t.Errorf("expected ExecutionTimeMs 1500, got %v", resp.Output[0].ExecutionTimeMs)
    }
}
```

Run test to confirm failure:
```bash
$ go test -v ./internal/protocol -run TestListWorkflowsResponseWithExecutionTime
--- FAIL: TestListWorkflowsResponseWithExecutionTime (0.00s)
```

---

## Step 4: Apply Minimal Fix

Edit `internal/protocol/messages.go` to add the field with exact JSON tags and
updated provenance comment:

```go
// ListWorkflowsResponseBody provenance: Go SDK dbos-transact-go/dbos/conductor_protocol.go
type ListWorkflowsResponseBody struct {
    WorkflowUUID    string  `json:"WorkflowUUID"`
    Status          *string `json:"Status,omitempty"`
    WorkflowName    *string `json:"WorkflowName,omitempty"`
    ExecutionTimeMs *int64  `json:"ExecutionTimeMs,omitempty"`
    ...
}
```

---

## Step 5: Verify Conformance

Re-run unit tests and conformance suite:

```bash
$ go test -v ./internal/protocol -run TestListWorkflowsResponseWithExecutionTime
--- PASS: TestListWorkflowsResponseWithExecutionTime (0.00s)
PASS

$ make test/conformance
ok  	github.com/abn/relay/tests/conformance	0.035s

$ make check
check: ok
```

---

## Step 6: Commit and Record

Stage explicit files and commit using Conventional Commits:

```bash
git add internal/protocol/messages.go internal/protocol/codec_test.go
git commit -m "fix(protocol): add execution time field to list workflows response"
```

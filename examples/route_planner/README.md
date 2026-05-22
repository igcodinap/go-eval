# Route Planner Example

Demonstrates artifact-first agent regression checks. The toy planner returns a
human-readable answer plus structured route, state, and budget artifacts. The
eval suite verifies the structured workflow state with deterministic metrics
before any LLM judge would be needed.

## Run

```bash
# Evals are gated, so they skip by default.
go test ./...

# Run the full artifact eval suite.
GOEVAL=1 go test ./...
```

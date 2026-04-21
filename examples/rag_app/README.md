# RAG App Example

Demonstrates a full `go-eval` suite: a toy retriever and generator pipeline
evaluated with all five metrics under `go test`.

## Run

```bash
# Evals are gated, so they skip by default.
go test ./...

# Run the full eval suite.
GOEVAL=1 go test ./...
```

The `scriptedJudge` in `rag_test.go` returns canned responses so the example
is deterministic. In your own code, swap it for `OpenAIJudge` or your own
`Judge` implementation.

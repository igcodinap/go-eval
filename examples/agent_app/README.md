# Agent App Example

This example shows trace-aware agent evals with `RunAgent`.

```bash
go test ./...
GOEVAL=1 go test ./...
```

Unset `GOEVAL` and the evals skip. Set it and the suite scores the final
answer with `TaskCompletion` and the trace with `ToolUseCorrectness`.

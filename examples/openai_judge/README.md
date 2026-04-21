# OpenAI Judge Example

Demonstrates using the `adapters/openai` module with `go-eval`.

## Usage

```bash
export OPENAI_API_KEY=sk-...
go run -tags=example ./
```

The OpenAI adapter lives under `adapters/openai` as a separate Go module, so
the core `go-eval` package remains stdlib-only.

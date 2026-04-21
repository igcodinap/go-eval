# OpenAI Judge Example

Reference implementation of the `go-eval` `Judge` interface backed by the
OpenAI chat completions API.

## Usage

```bash
export OPENAI_API_KEY=sk-...
go run -tags=example ./
```

This lives in its own module so the OpenAI SDK stays out of the core
`go-eval` dependency tree. Copy `judge.go` into your own project or adapt it
for another provider.

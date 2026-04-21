module github.com/igcodinap/go-eval/examples/openai_judge

go 1.22

require (
	github.com/igcodinap/go-eval v0.0.0
	github.com/igcodinap/go-eval/adapters/openai v0.0.0
	github.com/sashabaranov/go-openai v1.41.2
)

replace github.com/igcodinap/go-eval => ../..

replace github.com/igcodinap/go-eval/adapters/openai => ../../adapters/openai

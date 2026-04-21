//go:build example

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	openai "github.com/sashabaranov/go-openai"

	eval "github.com/igcodinap/go-eval"
	openaieval "github.com/igcodinap/go-eval/adapters/openai"
)

func exampleMain() {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		log.Fatal("OPENAI_API_KEY not set")
	}
	judge := openaieval.NewJudge(openai.NewClient(key), openai.GPT4oMini)

	c := eval.Case{
		Input:   "What is the capital of France?",
		Output:  "Paris is the capital of France.",
		Context: []string{"Paris is the capital of France, located on the Seine."},
	}

	result, err := eval.Faithfulness{Threshold: 0.8}.Score(context.Background(), judge, c)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Faithfulness=%.2f passed=%v reason=%q\n", result.Score, result.Passed, result.Reason)
}

func main() { exampleMain() }

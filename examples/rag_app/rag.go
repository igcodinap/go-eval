// Package main is a demo RAG pipeline used to exercise go-eval.
package main

import "strings"

// Pipeline is a toy retriever and generator.
type Pipeline struct {
	Docs []string
}

// Retrieve returns up to k docs that share a word with q.
func (p *Pipeline) Retrieve(q string, k int) []string {
	var out []string
	qWords := strings.Fields(strings.ToLower(q))
	for _, doc := range p.Docs {
		docLower := strings.ToLower(doc)
		for _, word := range qWords {
			if strings.Contains(docLower, word) {
				out = append(out, doc)
				break
			}
		}
		if len(out) >= k {
			break
		}
	}
	return out
}

// Answer retrieves docs and returns a stitched answer.
func (p *Pipeline) Answer(q string) (string, []string) {
	docs := p.Retrieve(q, 3)
	if len(docs) == 0 {
		return "I don't know.", docs
	}
	return docs[0], docs
}

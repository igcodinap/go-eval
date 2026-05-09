package main

import (
	"strings"
	"time"

	eval "github.com/igcodinap/go-eval"
)

type Agent struct {
	Orders map[string]string
}

func (a *Agent) Answer(input string) (string, []eval.TraceSpan) {
	trace := []eval.TraceSpan{
		{
			Kind:    eval.SpanLLM,
			Name:    "planner",
			Input:   input,
			Output:  "look up order status",
			Latency: 5 * time.Millisecond,
		},
	}

	orderID := "42"
	status := a.Orders[orderID]
	if status == "" {
		status = "unknown"
	}

	trace = append(trace, eval.TraceSpan{
		Kind:    eval.SpanTool,
		Name:    "orders.lookup",
		Input:   "order_id=" + orderID,
		Output:  status,
		Latency: 2 * time.Millisecond,
		Metadata: map[string]any{
			"tool_version": "demo",
		},
	})

	output := "Your order status is " + status + "."
	if strings.Contains(strings.ToLower(status), "tomorrow") {
		output = "Your order arrives tomorrow."
	}
	return output, trace
}

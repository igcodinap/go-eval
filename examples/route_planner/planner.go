package main

import (
	"encoding/json"
	"fmt"
)

type routeArtifact struct {
	Status       string   `json:"status"`
	TotalMinutes int      `json:"total_minutes"`
	Stops        []string `json:"stops"`
	Legs         []leg    `json:"legs"`
}

type leg struct {
	Mode    string `json:"mode"`
	From    string `json:"from"`
	To      string `json:"to"`
	Minutes int    `json:"minutes"`
}

type stateArtifact struct {
	Payment paymentState `json:"payment"`
}

type paymentState struct {
	Ready  bool   `json:"ready"`
	Method string `json:"method"`
}

type budgetArtifact struct {
	Tokens int `json:"tokens"`
}

func planRoute(input string) (string, map[string]json.RawMessage, error) {
	_ = input

	route := routeArtifact{
		Status:       "ready",
		TotalMinutes: 98,
		Stops:        []string{"Universidad de Santiago", "Pajaritos", "Valparaiso"},
		Legs: []leg{
			{Mode: "metro", From: "Universidad de Santiago", To: "Pajaritos", Minutes: 24},
			{Mode: "bus", From: "Pajaritos", To: "Valparaiso", Minutes: 74},
		},
	}
	state := stateArtifact{
		Payment: paymentState{Ready: true, Method: "card"},
	}
	budget := budgetArtifact{Tokens: 742}

	artifacts, err := marshalArtifacts(map[string]any{
		"route":  route,
		"state":  state,
		"budget": budget,
	})
	if err != nil {
		return "", nil, err
	}

	output := fmt.Sprintf(
		"Take the metro to %s, then the bus to %s. Estimated time: %d minutes.",
		route.Stops[1],
		route.Stops[2],
		route.TotalMinutes,
	)
	return output, artifacts, nil
}

func marshalArtifacts(values map[string]any) (map[string]json.RawMessage, error) {
	out := make(map[string]json.RawMessage, len(values))
	for key, value := range values {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		out[key] = data
	}
	return out, nil
}

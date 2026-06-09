package eval

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// ScenarioDataset is the JSON file format for portable scenario definitions.
type ScenarioDataset struct {
	Scenarios []Scenario `json:"scenarios"`
}

// LoadScenarios reads portable JSON scenario definitions from path.
//
// Loaded scenarios include DriverName but not Driver. BindScenarioDrivers can
// attach app-owned drivers before passing scenarios to Runner.RunScenario.
func LoadScenarios(path string) ([]Scenario, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open scenarios %q: %w", path, err)
	}

	scenarios, readErr := DecodeScenarios(f)
	closeErr := f.Close()
	if readErr != nil && closeErr != nil {
		return nil, errors.Join(
			fmt.Errorf("load scenarios %q: %w", path, readErr),
			fmt.Errorf("close scenarios %q: %w", path, closeErr),
		)
	}
	if readErr != nil {
		return nil, fmt.Errorf("load scenarios %q: %w", path, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close scenarios %q: %w", path, closeErr)
	}
	return scenarios, nil
}

// DecodeScenarios reads portable JSON scenario definitions from r.
func DecodeScenarios(r io.Reader) ([]Scenario, error) {
	if r == nil {
		return nil, errors.New("decode scenarios: reader is nil")
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("decode scenarios: %w", err)
	}
	scenarios, err := decodeScenarioJSON(data)
	if err != nil {
		return nil, fmt.Errorf("decode scenarios: %w", err)
	}
	for i, scenario := range scenarios {
		if scenario.DriverName == "" {
			return nil, fmt.Errorf("scenario %d: driver is required", i+1)
		}
		if err := validateScenarioDefinition(scenario); err != nil {
			return nil, fmt.Errorf("scenario %q: %w", scenario.Name, err)
		}
	}
	return scenarios, nil
}

// BindScenarioDrivers attaches app-owned drivers to loaded scenarios.
func BindScenarioDrivers(scenarios []Scenario, drivers map[string]StepFunc) ([]Scenario, error) {
	out := make([]Scenario, len(scenarios))
	for i, scenario := range scenarios {
		out[i] = cloneScenarioDefinition(scenario)
		if out[i].Driver != nil {
			continue
		}
		if out[i].DriverName == "" {
			return nil, fmt.Errorf("scenario %q: driver is required", out[i].Name)
		}
		driver, ok := drivers[out[i].DriverName]
		if !ok || driver == nil {
			return nil, fmt.Errorf("scenario %q: driver %q not found", out[i].Name, out[i].DriverName)
		}
		out[i].Driver = driver
	}
	return out, nil
}

type scenarioDatasetJSON struct {
	Scenarios []scenarioJSON `json:"scenarios"`
}

type scenarioJSON struct {
	Name       string         `json:"name"`
	Tier       string         `json:"tier,omitempty"`
	DriverName string         `json:"driver"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	State      map[string]any `json:"state,omitempty"`
	Tools      []string       `json:"tools,omitempty"`
	Steps      []stepJSON     `json:"steps"`
	Repeat     ScenarioRepeat `json:"repeat,omitempty"`
}

type stepJSON struct {
	Name                  string         `json:"name"`
	Input                 string         `json:"input,omitempty"`
	RequiredTools         []string       `json:"required_tools,omitempty"`
	RequiredToolPatterns  []string       `json:"required_tool_patterns,omitempty"`
	ForbiddenTools        []string       `json:"forbidden_tools,omitempty"`
	ForbiddenToolPatterns []string       `json:"forbidden_tool_patterns,omitempty"`
	ForbiddenToolExcept   []string       `json:"forbidden_tool_except,omitempty"`
	RequiredArtifacts     []string       `json:"required_artifacts,omitempty"`
	ExpectedArtifacts     []string       `json:"expected_artifacts,omitempty"`
	ForbiddenArtifacts    []string       `json:"forbidden_artifacts,omitempty"`
	MaxToolCalls          int            `json:"max_tool_calls,omitempty"`
	ExpectFail            bool           `json:"expect_fail,omitempty"`
	Metadata              map[string]any `json:"metadata,omitempty"`
	Timeout               string         `json:"timeout,omitempty"`
	TimeoutNS             int64          `json:"timeout_ns,omitempty"`
}

func decodeScenarioJSON(data []byte) ([]Scenario, error) {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, errors.New("scenarios is null")
	}
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("[")) {
		var raw []scenarioJSON
		if err := decodeStrictJSON(data, &raw); err != nil {
			return nil, err
		}
		return scenariosFromJSON(raw)
	}
	var raw scenarioDatasetJSON
	if err := decodeStrictJSON(data, &raw); err != nil {
		return nil, err
	}
	if raw.Scenarios == nil {
		return nil, errors.New("scenarios is required")
	}
	return scenariosFromJSON(raw.Scenarios)
}

func scenariosFromJSON(raw []scenarioJSON) ([]Scenario, error) {
	out := make([]Scenario, len(raw))
	for i, scenario := range raw {
		steps, err := stepsFromJSON(scenario.Steps)
		if err != nil {
			return nil, fmt.Errorf("scenario %d: %w", i+1, err)
		}
		out[i] = Scenario{
			Name:       scenario.Name,
			Tier:       scenario.Tier,
			DriverName: scenario.DriverName,
			Metadata:   cloneMetadata(scenario.Metadata),
			State:      cloneMetadata(scenario.State),
			Tools:      NewToolRegistry(scenario.Tools...),
			Steps:      steps,
			Repeat:     scenario.Repeat,
		}
	}
	return out, nil
}

func stepsFromJSON(raw []stepJSON) ([]Step, error) {
	steps := make([]Step, len(raw))
	for i, step := range raw {
		timeout, err := parseScenarioStepTimeout(step)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i+1, err)
		}
		requiredArtifacts := append([]string(nil), step.RequiredArtifacts...)
		requiredArtifacts = append(requiredArtifacts, step.ExpectedArtifacts...)
		steps[i] = Step{
			Name:                  step.Name,
			Input:                 step.Input,
			RequiredTools:         append([]string(nil), step.RequiredTools...),
			RequiredToolPatterns:  append([]string(nil), step.RequiredToolPatterns...),
			ForbiddenTools:        append([]string(nil), step.ForbiddenTools...),
			ForbiddenToolPatterns: append([]string(nil), step.ForbiddenToolPatterns...),
			ForbiddenToolExcept:   append([]string(nil), step.ForbiddenToolExcept...),
			RequiredArtifacts:     requiredArtifacts,
			ForbiddenArtifacts:    append([]string(nil), step.ForbiddenArtifacts...),
			MaxToolCalls:          step.MaxToolCalls,
			ExpectFail:            step.ExpectFail,
			Metadata:              cloneMetadata(step.Metadata),
			Timeout:               timeout,
		}
	}
	return steps, nil
}

func parseScenarioStepTimeout(step stepJSON) (time.Duration, error) {
	if step.Timeout != "" && step.TimeoutNS != 0 {
		return 0, errors.New("timeout and timeout_ns are mutually exclusive")
	}
	if step.Timeout != "" {
		timeout, err := time.ParseDuration(step.Timeout)
		if err != nil {
			return 0, fmt.Errorf("timeout: %w", err)
		}
		return timeout, nil
	}
	if step.TimeoutNS < 0 {
		return 0, errors.New("timeout_ns must be non-negative")
	}
	return time.Duration(step.TimeoutNS), nil
}

func cloneScenarioDefinition(s Scenario) Scenario {
	return Scenario{
		Name:       s.Name,
		Tier:       s.Tier,
		DriverName: s.DriverName,
		Metadata:   cloneMetadata(s.Metadata),
		State:      cloneMetadata(s.State),
		Tools:      s.Tools,
		Driver:     s.Driver,
		Steps:      cloneSteps(s.Steps),
		Repeat:     s.Repeat,
	}
}

func cloneSteps(steps []Step) []Step {
	if steps == nil {
		return nil
	}
	out := make([]Step, len(steps))
	for i, step := range steps {
		out[i] = cloneStep(step)
	}
	return out
}

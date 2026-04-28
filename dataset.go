package eval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// Dataset is the JSON file format for external eval cases.
//
// Files are encoded as:
//
//	{
//	  "cases": [
//	    {
//	      "name": "france-capital",
//	      "input": "What is the capital of France?",
//	      "output": "Paris is the capital of France.",
//	      "expected": "Paris",
//	      "context": ["Paris is the capital of France."],
//	      "metadata": {"flow": "rag.answer", "tier": "critical", "dataset": "smoke/v1"}
//	    }
//	  ]
//	}
type Dataset struct {
	Cases []NamedCase `json:"cases"`
}

// MarshalJSON encodes Dataset with an array-valued cases field, even when empty.
func (d Dataset) MarshalJSON() ([]byte, error) {
	cases := d.Cases
	if cases == nil {
		cases = []NamedCase{}
	}
	var raw struct {
		Cases []NamedCase `json:"cases"`
	}
	raw.Cases = cases
	return json.Marshal(raw)
}

// UnmarshalJSON implements strict decoding for the dataset file format.
func (d *Dataset) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("dataset is null")
	}

	var raw struct {
		Cases json.RawMessage `json:"cases"`
	}
	if err := decodeStrictJSON(data, &raw); err != nil {
		return err
	}
	if raw.Cases == nil {
		return errors.New("cases is required")
	}
	if bytes.Equal(bytes.TrimSpace(raw.Cases), []byte("null")) {
		return errors.New("cases must be an array")
	}

	var cases []NamedCase
	if err := decodeStrictJSON(raw.Cases, &cases); err != nil {
		return fmt.Errorf("cases: %w", err)
	}
	d.Cases = cases
	return nil
}

// NamedCase pairs a table-driven test name with a Case.
type NamedCase struct {
	Name string
	Case Case
}

// MarshalJSON encodes NamedCase using the flattened dataset case format.
func (c NamedCase) MarshalJSON() ([]byte, error) {
	return json.Marshal(namedCaseJSON{
		Name:     c.Name,
		Input:    c.Case.Input,
		Output:   c.Case.Output,
		Expected: c.Case.Expected,
		Context:  c.Case.Context,
		Metadata: c.Case.Metadata,
	})
}

// UnmarshalJSON decodes NamedCase from the flattened dataset case format.
func (c *NamedCase) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("case is null")
	}

	var raw namedCaseJSON
	if err := decodeStrictJSON(data, &raw); err != nil {
		return err
	}
	c.Name = raw.Name
	c.Case = Case{
		Input:    raw.Input,
		Output:   raw.Output,
		Expected: raw.Expected,
		Context:  raw.Context,
		Metadata: raw.Metadata,
	}
	return nil
}

type namedCaseJSON struct {
	Name     string         `json:"name,omitempty"`
	Input    string         `json:"input,omitempty"`
	Output   string         `json:"output,omitempty"`
	Expected string         `json:"expected,omitempty"`
	Context  []string       `json:"context,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// LoadDataset reads a JSON dataset file.
func LoadDataset(path string) (Dataset, error) {
	f, err := os.Open(path)
	if err != nil {
		return Dataset{}, fmt.Errorf("open dataset %q: %w", path, err)
	}

	dataset, readErr := DecodeDataset(f)
	closeErr := f.Close()
	if readErr != nil && closeErr != nil {
		return Dataset{}, errors.Join(
			fmt.Errorf("load dataset %q: %w", path, readErr),
			fmt.Errorf("close dataset %q: %w", path, closeErr),
		)
	}
	if readErr != nil {
		return Dataset{}, fmt.Errorf("load dataset %q: %w", path, readErr)
	}
	if closeErr != nil {
		return Dataset{}, fmt.Errorf("close dataset %q: %w", path, closeErr)
	}
	return dataset, nil
}

// DecodeDataset reads a JSON dataset from r.
func DecodeDataset(r io.Reader) (Dataset, error) {
	if r == nil {
		return Dataset{}, errors.New("decode dataset: reader is nil")
	}

	var dataset Dataset
	dec := json.NewDecoder(r)
	if err := dec.Decode(&dataset); err != nil {
		return Dataset{}, fmt.Errorf("decode dataset: %w", err)
	}
	if err := ensureSingleJSONValue(dec); err != nil {
		return Dataset{}, fmt.Errorf("decode dataset: %w", err)
	}
	return dataset, nil
}

// LoadCases reads a JSON dataset file and returns only the eval cases.
func LoadCases(path string) ([]Case, error) {
	dataset, err := LoadDataset(path)
	if err != nil {
		return nil, err
	}
	return casesOnly(dataset.Cases), nil
}

// DecodeCases reads a JSON dataset from r and returns only the eval cases.
func DecodeCases(r io.Reader) ([]Case, error) {
	dataset, err := DecodeDataset(r)
	if err != nil {
		return nil, err
	}
	return casesOnly(dataset.Cases), nil
}

// LoadNamedCases reads a JSON dataset file and requires every case to have a name.
func LoadNamedCases(path string) ([]NamedCase, error) {
	dataset, err := LoadDataset(path)
	if err != nil {
		return nil, err
	}
	if err := validateNamedCases(dataset.Cases); err != nil {
		return nil, fmt.Errorf("load named cases %q: %w", path, err)
	}
	return dataset.Cases, nil
}

// DecodeNamedCases reads a JSON dataset from r and requires every case to have a name.
func DecodeNamedCases(r io.Reader) ([]NamedCase, error) {
	dataset, err := DecodeDataset(r)
	if err != nil {
		return nil, err
	}
	if err := validateNamedCases(dataset.Cases); err != nil {
		return nil, err
	}
	return dataset.Cases, nil
}

func validateNamedCases(cases []NamedCase) error {
	for i, c := range cases {
		if c.Name == "" {
			return fmt.Errorf("case %d: name is required", i+1)
		}
	}
	return nil
}

func casesOnly(cases []NamedCase) []Case {
	out := make([]Case, len(cases))
	for i, c := range cases {
		out[i] = c.Case
	}
	return out
}

func decodeStrictJSON(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	return ensureSingleJSONValue(dec)
}

func ensureSingleJSONValue(dec *json.Decoder) error {
	var extra any
	err := dec.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("multiple JSON values")
}

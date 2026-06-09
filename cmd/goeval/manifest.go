package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"strings"
	"time"

	"github.com/igcodinap/go-eval/compare"
)

const defaultConfigPath = "goeval.json"

type manifestFile struct {
	Profiles map[string]profileConfig `json:"profiles,omitempty"`
	Compare  compare.Policy           `json:"compare,omitempty"`
}

type profileConfig struct {
	Packages            []string               `json:"packages,omitempty"`
	Tiers               []string               `json:"tiers,omitempty"`
	ResultsDir          string                 `json:"results_dir,omitempty"`
	Prerequisites       []manifestPrerequisite `json:"prerequisites,omitempty"`
	MissingPrerequisite string                 `json:"missing_prerequisite,omitempty"`
}

type manifestPrerequisite struct {
	Type    string `json:"type"`
	Name    string `json:"name,omitempty"`
	Path    string `json:"path,omitempty"`
	Address string `json:"address,omitempty"`
}

type missingPrerequisite struct {
	Name   string
	Reason string
}

func loadManifest(path string, required bool) (manifestFile, bool, error) {
	if path == "" {
		path = defaultConfigPath
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !required {
			return manifestFile{}, false, nil
		}
		return manifestFile{}, false, err
	}
	defer func() { _ = f.Close() }()

	var manifest manifestFile
	dec := json.NewDecoder(f)
	if err := dec.Decode(&manifest); err != nil {
		return manifestFile{}, false, err
	}
	if err := validatePolicy(manifest.Compare); err != nil {
		return manifestFile{}, false, fmt.Errorf("compare policy: %w", err)
	}
	for name, profile := range manifest.Profiles {
		if err := validateProfile(name, profile); err != nil {
			return manifestFile{}, false, err
		}
	}
	return manifest, true, nil
}

func loadPolicyFile(path string) (compare.Policy, error) {
	f, err := os.Open(path)
	if err != nil {
		return compare.Policy{}, err
	}
	defer func() { _ = f.Close() }()

	var raw map[string]json.RawMessage
	dec := json.NewDecoder(f)
	if err := dec.Decode(&raw); err != nil {
		return compare.Policy{}, err
	}

	var policy compare.Policy
	if payload, ok := raw["compare"]; ok {
		if err := json.Unmarshal(payload, &policy); err != nil {
			return compare.Policy{}, err
		}
	} else {
		payload, err := json.Marshal(raw)
		if err != nil {
			return compare.Policy{}, err
		}
		if err := json.Unmarshal(payload, &policy); err != nil {
			return compare.Policy{}, err
		}
	}
	if err := validatePolicy(policy); err != nil {
		return compare.Policy{}, err
	}
	return policy, nil
}

func validateProfile(name string, profile profileConfig) error {
	switch profile.MissingPrerequisite {
	case "", "skip", "fail":
	default:
		return fmt.Errorf("profile %q: missing_prerequisite must be skip or fail", name)
	}
	for _, prereq := range profile.Prerequisites {
		if err := prereq.validate(); err != nil {
			return fmt.Errorf("profile %q: %w", name, err)
		}
	}
	return nil
}

func (p manifestPrerequisite) validate() error {
	switch p.Type {
	case "env":
		if p.Name == "" {
			return errors.New("env prerequisite requires name")
		}
	case "file":
		if p.Path == "" && p.Name == "" {
			return errors.New("file prerequisite requires path")
		}
	case "tcp":
		if p.Address == "" {
			return errors.New("tcp prerequisite requires address")
		}
	default:
		return fmt.Errorf("invalid prerequisite type %q", p.Type)
	}
	return nil
}

func validatePolicy(policy compare.Policy) error {
	if err := validateMetricPolicy("default", policy.Default); err != nil {
		return err
	}
	for metric, metricPolicy := range policy.Metrics {
		if err := validateMetricPolicy("metric "+metric, metricPolicy); err != nil {
			return err
		}
	}
	for tier, tierPolicy := range policy.Tiers {
		if err := validateMetricPolicy("tier "+tier, tierPolicy); err != nil {
			return err
		}
	}
	for metric, byTier := range policy.MetricTiers {
		for tier, metricTierPolicy := range byTier {
			if err := validateMetricPolicy("metric "+metric+" tier "+tier, metricTierPolicy); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateMetricPolicy(name string, policy compare.MetricPolicy) error {
	if policy.ScoreTolerance != nil && invalidNonNegative(*policy.ScoreTolerance) {
		return fmt.Errorf("%s score_tolerance must be non-negative", name)
	}
	if policy.FlakyScoreStdDev != nil && invalidNonNegative(*policy.FlakyScoreStdDev) {
		return fmt.Errorf("%s flaky_score_stddev must be non-negative", name)
	}
	return nil
}

func invalidNonNegative(value float64) bool {
	return value < 0 || math.IsNaN(value) || math.IsInf(value, 0)
}

func checkManifestPrerequisites(ctx context.Context, prereqs []manifestPrerequisite) []missingPrerequisite {
	var missing []missingPrerequisite
	for _, prereq := range prereqs {
		if err := prereq.check(ctx); err != nil {
			missing = append(missing, missingPrerequisite{Name: prereq.displayName(), Reason: err.Error()})
		}
	}
	return missing
}

func (p manifestPrerequisite) check(ctx context.Context) error {
	switch p.Type {
	case "env":
		if os.Getenv(p.Name) == "" {
			return fmt.Errorf("%s is not set", p.Name)
		}
		return nil
	case "file":
		_, err := os.Stat(p.filePath())
		return err
	case "tcp":
		dialer := net.Dialer{Timeout: time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", p.Address)
		if err != nil {
			return err
		}
		return conn.Close()
	default:
		return fmt.Errorf("invalid prerequisite type %q", p.Type)
	}
}

func (p manifestPrerequisite) displayName() string {
	switch p.Type {
	case "env":
		return "env " + p.Name
	case "file":
		return "file " + p.filePath()
	case "tcp":
		if p.Name != "" {
			return "tcp " + p.Name
		}
		return "tcp " + p.Address
	default:
		return strings.TrimSpace(p.Type + " " + p.Name)
	}
}

func (p manifestPrerequisite) filePath() string {
	if p.Path != "" {
		return p.Path
	}
	return p.Name
}

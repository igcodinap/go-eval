package eval

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"
)

const prerequisiteSkipPrefix = "goeval prerequisite missing:"

// Requirement describes an external prerequisite for an eval suite.
type Requirement interface {
	Name() string
	Check(context.Context) error
}

type requirement struct {
	name  string
	check func(context.Context) error
}

// Name returns the human-readable prerequisite name.
func (r requirement) Name() string { return r.name }

// Check verifies the prerequisite.
func (r requirement) Check(ctx context.Context) error { return r.check(ctx) }

// Require skips the test when any prerequisite is unavailable.
func Require(tb testing.TB, reqs ...Requirement) {
	tb.Helper()

	ctx := context.Background()
	for _, req := range reqs {
		if req == nil {
			continue
		}
		if err := req.Check(ctx); err != nil {
			tb.Skipf("%s %s: %v", prerequisiteSkipPrefix, req.Name(), err)
		}
	}
}

// Env requires a non-empty environment variable.
func Env(name string) Requirement {
	return requirement{
		name: "env " + name,
		check: func(context.Context) error {
			if os.Getenv(name) == "" {
				return fmt.Errorf("%s is not set", name)
			}
			return nil
		},
	}
}

// File requires an existing filesystem path.
func File(path string) Requirement {
	return requirement{
		name: "file " + path,
		check: func(context.Context) error {
			if _, err := os.Stat(path); err != nil {
				return err
			}
			return nil
		},
	}
}

// TCP requires a reachable TCP address.
func TCP(name string, address string) Requirement {
	return requirement{
		name: name,
		check: func(ctx context.Context) error {
			dialer := net.Dialer{Timeout: time.Second}
			conn, err := dialer.DialContext(ctx, "tcp", address)
			if err != nil {
				return err
			}
			return conn.Close()
		},
	}
}

// Func requires a caller-provided check to succeed.
func Func(name string, check func(context.Context) error) Requirement {
	return requirement{
		name: name,
		check: func(ctx context.Context) error {
			if check == nil {
				return nil
			}
			return check(ctx)
		},
	}
}

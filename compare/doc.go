// Package compare compares go-eval JSONL result files.
//
// The package is intended for tests and future CLI code that need to compare a
// baseline run against a current run. By default, rows are matched by test name
// and metric. Callers that encode a separate case identity in metadata can
// provide Options.Identity to include that value in the comparison key.
package compare

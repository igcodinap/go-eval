# go-eval Proposals

This directory contains design proposals for notable changes to `go-eval`.

The process is intentionally lightweight and follows the same shape as the Go
project's proposal flow: start with a GitHub issue, use that issue as the main
discussion thread, and add a checked-in design document when the change needs a
stable record.

## When to Write a Proposal

Use this process for changes that affect the public direction of the project,
including:

- new public APIs
- command-line tools
- result formats or compatibility-sensitive behavior
- new evaluation models such as conversations or datasets
- large adapters or integrations
- roadmap-sized feature groups

Small bug fixes, documentation edits, internal refactors, and narrow metric
additions can use the normal pull request flow.

## Process

1. Open a GitHub issue describing the proposal.
2. Discuss motivation, scope, alternatives, and open questions on the issue.
3. If the change is substantial, add a proposal document in this directory.
4. Keep the issue as the primary discussion thread.
5. Revise the proposal document as the discussion converges.
6. Once accepted, implementation work can proceed through ordinary issues and
   pull requests.

## Document Naming

Proposal documents use a stable numeric prefix:

```text
docs/proposals/0001-short-name.md
```

The number is assigned in repository order, not by GitHub issue number. Each
proposal document should link to its tracking issue once one exists.

## Proposal Status

Use one of these statuses:

- `Draft`: under active discussion
- `Accepted`: agreed direction; ready for implementation
- `Implemented`: shipped in a release
- `Deferred`: useful, but not in the current planning window
- `Rejected`: intentionally not pursuing

## Template

```md
# Proposal: Short Title

Status: Draft
Tracking issue: #TBD
Target release: vX.Y.0
Created: YYYY-MM-DD

## Summary

## Motivation

## Goals

## Non-goals

## Proposed Design

## Compatibility

## Risks

## Alternatives Considered

## Open Questions

## Implementation Plan
```

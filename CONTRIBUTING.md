# Contributing

Thanks for taking the time to look through the project.

## Local Workflow

Use the standard Go toolchain already declared in [`go.mod`](go.mod).

```bash
go test ./...
go test -race ./...
go test -bench=. ./pkg/limiter
go run ./cmd/mtl-demo
```

## Expectations

- Keep changes small and reviewable.
- Prefer standard library solutions unless a dependency clearly improves the design.
- Maintain deterministic tests where possible.
- Preserve the current tone of the repository: honest tradeoffs, clear naming, and readable package boundaries.

## Before Opening a Pull Request

- Format Go files with `gofmt`.
- Run `go vet ./...`.
- Run the test suite and race checks locally.
- Update the README when behavior, architecture, or usage changes.

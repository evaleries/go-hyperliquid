# Contributing to go-hyperliquid

Thank you for your interest in contributing to go-hyperliquid! This document provides guidelines and information for contributors.

## Development Setup

### Prerequisites

- Go 1.21 or later
- Git
- golangci-lint (optional, will be installed automatically)

### Getting Started

1. Fork the repository
2. Clone your fork:
   ```bash
   git clone https://github.com/yourusername/go-hyperliquid.git
   cd go-hyperliquid
   ```

3. Install dependencies:
   ```bash
   make deps
   ```

4. Install development tools:
   ```bash
   make install-tools
   ```

5. Set up git hooks (optional but recommended):
   ```bash
   make git-hooks
   ```

## Development Workflow

### Making Changes

1. Create a new branch:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. Make your changes

3. **Regenerate code** (if you modified any structs with `//go:generate` comments):
   ```bash
   make generate
   ```

4. Run all checks (format, vet, lint):
   ```bash
   make check
   ```

5. Run tests:
   ```bash
   make test
   ```

6. Commit your changes:
   ```bash
   git commit -m "feat: add your feature description"
   ```

> **⚠️ Important**: The CI will fail if `go vet` finds issues. Always run `make check` before pushing.

### JSON Codec

This project uses [sonic](https://github.com/bytedance/sonic) (`sonic.ConfigStd`) for high-performance JSON marshaling/unmarshaling — no code generation step is required. All JSON goes through the package-level `jsonCodec` in `json.go`; use it instead of `encoding/json` in library code.

Caveats when editing wire types:

- Give every wire field an explicit `json:"..."` tag. The codec matches keys case-insensitively (`sonic.ConfigStd` leaves `CaseSensitive` off, exactly like `encoding/json`), so a missing tag usually still decodes — don't rely on it: the outbound bytes come from the field name verbatim, and a typo'd name fails silently in only one direction.
- Payload types dispatched over the websocket must not implement `UnmarshalJSON` in a way that retains its input slice (as `MixedValue` does): sonic hands custom unmarshalers a no-copy view of the pooled read buffer.
- Do not add `,string` to string-kind fields (it double-encodes them).
- Hot types are precompiled via `pretouchJSON` in `NewWebsocketClient`/`NewExchange`; add new high-frequency payload types there.

Platform matrix (from sonic v1.15.3: `sonic.go` carries
`//go:build (amd64 && go1.17 && !go1.28) || (arm64 && go1.20 && !go1.28)`, `compat.go` the inverse):

- The JIT path is only active on amd64 with Go >=1.17 and on arm64 with Go >=1.20, and is disabled on Go >=1.28.
- Every other platform/toolchain silently falls back to an `encoding/json`-backed implementation: semantics are
  equivalent, but there is no JIT speedup and `PretouchMany` (used by `pretouchJSON`) is a no-op there.
- On JIT platforms sonic allocates executable memory, so W^X-hardened or otherwise restricted sandboxes can fail at first marshal.
- CI covers linux/amd64 only, so the fallback path is not exercised there.

### Commit Messages

We follow conventional commit format:

- `feat:` for new features
- `fix:` for bug fixes
- `docs:` for documentation changes
- `test:` for test additions or modifications
- `refactor:` for code refactoring
- `perf:` for performance improvements
- `ci:` for CI/CD changes

## Testing

### Running Tests

```bash
# Run all tests
make test

# Run tests with coverage
make coverage

# Run only short tests
make test-short

# Run tests excluding examples (CI mode)
make ci-test

# Run example tests separately
make examples
```

### Writing Tests

- Use table-driven tests when possible
- Use `testify/assert` and `testify/require` for assertions
- Include both positive and negative test cases
- Test edge cases and error conditions
- Mock external dependencies

### Test Coverage

We aim for high test coverage. Check coverage with:

```bash
make coverage
```

This generates a `coverage.html` file you can open in your browser.

## Code Style

### Formatting

Code is automatically formatted using:

```bash
make fmt
```

This runs:
- `go fmt`
- `goimports`
- `golines`

### Linting

We use `golangci-lint` for code linting:

```bash
make lint
```

The linting configuration is in `.golangci.yml`.

### Code Guidelines

1. **Naming**: Follow Go naming conventions
2. **Documentation**: Add godoc comments for exported functions, types, and packages
3. **Error Handling**: Always handle errors appropriately
4. **Interfaces**: Prefer small, focused interfaces
5. **Context**: Use `context.Context` for cancellation and timeouts
6. **Concurrency**: Use goroutines and channels safely

## Project Structure

```
├── .github/          # GitHub Actions workflows and templates
├── examples/         # Example code (excluded from CI tests)
├── client.go         # HTTP client implementation
├── exchange.go       # Trading API implementation
├── info.go          # Information API endpoints
├── models.go        # Core data models
├── signing.go       # Request signing utilities
├── types.go         # API types and structures
├── ws.go           # WebSocket client implementation
├── ws_types.go     # WebSocket message types
└── *_test.go       # Test files
```

## CI/CD

### GitHub Actions

We use GitHub Actions for:

- **CI**: Run tests, linting, and checks on multiple Go versions
- **Coverage**: Generate and upload coverage reports
- **Security**: Run security scans
- **Release**: Automated releases on tag push

### Make Targets

Common development commands:

```bash
make help           # Show all available targets
make ci-full        # Run complete CI pipeline locally
make ci-fmt-check   # Check code formatting (CI mode)
make ci-lint        # Run linter excluding examples
make ci-test        # Run tests excluding examples
```

## Pull Request Process

1. **Fork** the repository
2. **Create** a feature branch
3. **Make** your changes
4. **Add** tests for new functionality
5. **Run** `make ci-full` to ensure everything passes
6. **Push** to your fork
7. **Create** a pull request

### PR Requirements

- [ ] All tests pass (`make test`)
- [ ] Code is properly formatted (`make fmt`)
- [ ] `go vet` passes (`make vet`)
- [ ] Linter passes (`make lint`)
- [ ] Generated files are up to date (`make generate`)
- [ ] New code has appropriate test coverage
- [ ] Documentation is updated if needed
- [ ] Conventional commit messages are used

**Quick check before pushing:**
```bash
make generate && make check && make test
```

## Release Process

Releases are automated through GitHub Actions when a new tag is pushed:

```bash
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

## Getting Help

- **Issues**: Create a GitHub issue for bugs or feature requests
- **Discussions**: Use GitHub Discussions for questions
- **Documentation**: Check the README and godoc comments

## License

By contributing to go-hyperliquid, you agree that your contributions will be licensed under the MIT License.

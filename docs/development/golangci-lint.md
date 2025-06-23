# golangci-lint Integration

This project uses [golangci-lint](https://golangci-lint.run/) v2.1.6 for Go code linting and quality checks.

## Installation

golangci-lint is automatically installed when running:

```bash
make golangci-lint
```

Or install it manually:

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.6
```

## Usage

### Running Linters

```bash
# Run all enabled linters
make lint

# Run linters with auto-fix
make lint-fix

# Format code using golangci-lint formatters
make format
```

### Configuration

The project uses a comprehensive `.golangci.yml` configuration file with the following linters enabled:

#### Base Linters
- **errcheck**: Check for unchecked errors
- **govet**: Vet examines Go source code
- **ineffassign**: Detect ineffectual assignments
- **staticcheck**: Go static analysis (includes gosimple, stylecheck)
- **unused**: Check for unused constants, variables, functions and types

#### Code Quality
- **dupl**: Check for code duplication
- **gocyclo**: Check cyclomatic complexity
- **goconst**: Find repeated strings that could be constants
- **prealloc**: Find slice declarations that could potentially be preallocated
- **unconvert**: Remove unnecessary type conversions
- **unparam**: Check for unused function parameters

#### Style & Standards
- **revive**: Drop-in replacement of golint
- **misspell**: Find commonly misspelled English words

#### Security
- **gosec**: Security-focused linter

#### Testing
- **ginkgolinter**: Enforce standards of using ginkgo and gomega
- **testpackage**: Make sure that separate _test packages are used

#### Additional Quality
- **nakedret**: Find naked returns in functions greater than a specified function length
- **lll**: Reports long lines
- **gocritic**: Provides diagnostics that check for bugs, performance and style issues
- **gocognit**: Compute and check the cognitive complexity of functions
- **nestif**: Reports deeply nested if statements
- **err113**: Check the errors handling expressions
- **nolintlint**: Reports ill-formed or insufficient nolint directives
- **whitespace**: Detection of leading and trailing whitespace
- **wsl**: Add or remove empty lines

## Version 2.x Migration

This project has been migrated to golangci-lint v2.x which includes several breaking changes:

1. **Configuration version**: Must specify `version: "2"` in `.golangci.yml`
2. **Merged linters**: `gosimple` and `stylecheck` are now part of `staticcheck`
3. **Formatters**: `gofmt` and `goimports` are now handled via the `fmt` command
4. **Removed linters**: Some linters like `exportloopref` have been deprecated

## CI Integration

The linter runs automatically in CI/CD pipelines and can be integrated with:

- GitHub Actions
- Pre-commit hooks
- IDE integrations (VS Code, GoLand, etc.)

## Troubleshooting

### Unknown Linter Error
If you see "unknown linters" errors, check the [linter documentation](https://golangci-lint.run/usage/linters/) for v2.x compatibility.

### Configuration Validation
Verify your configuration with:
```bash
golangci-lint config verify
```

### Performance Issues
If linting is slow, consider:
- Reducing the number of enabled linters
- Using `--fast` flag for quick checks
- Excluding large generated files

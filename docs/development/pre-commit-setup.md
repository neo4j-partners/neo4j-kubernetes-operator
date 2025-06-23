# Pre-commit Setup

This project uses [pre-commit](https://pre-commit.com/) to run essential code quality checks before commits. The hooks are optimized for speed to ensure fast commits.

## Installation

Install pre-commit and set up the hooks:

```bash
# Install pre-commit (if not already installed)
pip install pre-commit

# Install the pre-commit hooks
pre-commit install
```

## Streamlined Hooks

The pre-commit configuration has been optimized for speed and focuses on essential checks:

### Essential File Checks (Fast)
- **trailing-whitespace**: Remove trailing whitespace
- **end-of-file-fixer**: Ensure files end with a newline
- **check-yaml**: Validate YAML syntax
- **check-added-large-files**: Prevent large files (>1MB) from being committed
- **check-case-conflict**: Check for case conflicts
- **check-merge-conflict**: Check for merge conflict markers
- **debug-statements**: Check for debug statements

### Fast Go Code Quality
- **go fmt**: Format Go code
- **go imports**: Organize imports
- **go mod tidy**: Clean up go.mod and go.sum
- **golangci-lint (fast)**: Run essential linters with 2-minute timeout

### Security & Validation
- **gitleaks**: Scan for secrets and sensitive information
- **yamllint**: Validate YAML files
- **commitizen**: Validate commit message format

## Fast Linting Configuration

The pre-commit hooks use a special `.golangci-precommit.yml` configuration that includes only fast, essential linters:

- **errcheck**: Check for unchecked errors
- **govet**: Vet examines Go source code
- **ineffassign**: Detect ineffectual assignments
- **staticcheck**: Go static analysis
- **unused**: Check for unused code
- **misspell**: Find misspelled words
- **gosec**: Security checks
- **revive**: Style checks

## Manual Commands

You can run the hooks manually:

```bash
# Run all hooks on all files
pre-commit run --all-files

# Run specific hook
pre-commit run golangci-lint-fast --all-files

# Run fast linting via Makefile
make lint-fast
```

## What Was Removed for Speed

To optimize commit speed, the following slow operations were removed:

- **go build**: Building the entire project (moved to CI/CD)
- **go vet**: Redundant with golangci-lint govet
- **Full test suite**: Tests run in CI/CD, not on every commit
- **Slow linters**: Complex linters like dupl, gocyclo, etc. (available in `make lint`)
- **check-executables-have-shebangs**: Not needed for this project

## Full Linting

For comprehensive linting (including slower checks), use:

```bash
# Full linting with all checks
make lint

# Full linting with fixes
make lint-fix
```

This separation allows for:
- **Fast commits**: Essential checks only (typically <30 seconds)
- **Thorough review**: Full linting before PR submission
- **CI/CD validation**: Complete testing and building in pipelines

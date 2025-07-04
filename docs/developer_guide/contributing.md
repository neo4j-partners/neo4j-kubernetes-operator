# Contributing

We welcome contributions to the Neo4j Enterprise Operator! This guide will help you get started.

## Getting Started

1.  **Fork and clone the repository.**

2.  **Set up your development environment:**

    ```bash
    make setup-dev
    ```

3.  **Make your changes.**

4.  **Run the tests:**

    ```bash
    make test
    ```

5.  **Submit a pull request.**

## Code Style

Please follow the standard Go code style. You can use `make fmt` to format your code. Code quality tools (`make lint`, `make vet`, `make security`) are available for local development but are not enforced by CI.

## Git Commit Messages

Please follow the [Conventional Commits](https://www.conventionalcommits.org/) specification for your commit messages.

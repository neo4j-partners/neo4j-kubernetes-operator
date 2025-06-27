# Development

This guide explains how to set up your development environment and get started with contributing to the Neo4j Enterprise Operator.

## Prerequisites

*   Go (v1.21+)
*   Docker
*   `kubectl`
*   `kind`
*   `make`

## Getting Started

1.  **Fork and clone the repository.**

2.  **Install the development tools:**

    ```bash
    make setup-dev
    ```

    This command will install all the necessary tools and dependencies for building and testing the operator.

3.  **Create a development cluster:**

    ```bash
    make dev-cluster
    ```

    This will create a local Kubernetes cluster using `kind`.

4.  **Run the operator locally:**

    ```bash
    make dev-run
    ```

    This will build and run the operator on your local machine, connected to the `kind` cluster. This allows for rapid iteration and debugging.

## Testing

To run the full test suite, use the following command:

```bash
make test
```

For more detailed information on testing, see the [Testing Guide](testing.md).

# Development

This guide explains how to set up your development environment and get started with contributing to the Neo4j Enterprise Operator.

## Prerequisites

*   Go (v1.21+)
*   Docker
*   `kubectl`
*   `kind`

## Getting Started

1.  **Fork and clone the repository.**

2.  **Install the development tools:**

    ```bash
    make setup-dev
    ```

3.  **Create a development cluster:**

    ```bash
    make dev-cluster
    ```

4.  **Run the operator locally:**

    ```bash
    make dev-run
    ```

## Testing

To run the test suite, use the following command:

```bash
make test
```

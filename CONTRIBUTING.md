# Contributing to maXwell IRC

Thank you for your interest in contributing! This document outlines how to get started, build the project, and submit contributions.

## Getting Started

### Prerequisites

- Go 1.26+ (https://golang.org/dl/)
- Make
- SQLite3 (or MySQL 8.0+ for optional database support)

### Building locally

Clone the repository and build:

```bash
git clone https://github.com/RealDtx/maxwell-irc.git
cd maxwell-irc
make build
```

The binary will be placed in `./bin/maxwell-irc`.

### Running tests

```bash
make test
```

### Testing with Docker

For local integration testing:

```bash
make docker-up    # Start services
make docker-down  # Stop services
```

## Code Style

- Follow standard Go conventions and style guidelines (https://golang.org/doc/effective_go)
- Format code with `gofmt` (automatically enforced by `go fmt ./...`)
- Run `go vet ./...` before submitting
- Keep functions focused and use clear, descriptive names

## Commit Messages

Use conventional commits style for clarity:

- `feat:` for new features
- `fix:` for bug fixes
- `refactor:` for code refactoring
- `docs:` for documentation changes
- `chore:` for dependency updates, build config, etc.

Example: `feat: add XDCC bot search filtering`

## Pull Requests

- Keep PRs focused and small (one feature or fix per PR)
- Provide a clear description of what was changed and why
- Include testing done: unit tests, manual testing, Pi testing, database testing
- Reference related issues where applicable
- Use the PR template when opening a PR

## Reporting Issues

Use the issue templates when reporting bugs or requesting features:

- **Bug reports**: Include steps to reproduce, environment details, and relevant logs
- **Feature requests**: Describe the problem you're solving and proposed solution

For bugs, include:

- Your OS and Go version
- Output from `journalctl -u maxwell-irc -f` if the service is running
- Steps to reproduce the issue
- Whether you're using SQLite or MySQL

## No CLA required

We don't require a Contributor License Agreement. Open a PR and we'll review it!

## Questions?

Feel free to open an issue to ask questions or discuss ideas before implementing.

Happy coding!

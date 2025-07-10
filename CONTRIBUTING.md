# Contributing to godi

Thank you for your interest in contributing to godi! This document provides guidelines and instructions for contributing.

## Getting Started

1. Fork the repository on GitHub
2. Clone your fork locally:
   ```bash
   git clone https://github.com/your-username/godi.git
   cd godi
   ```
3. Add the upstream repository:
   ```bash
   git remote add upstream https://github.com/junioryono/godi.git
   ```
4. Create a new branch for your feature or fix:
   ```bash
   git checkout -b feature/my-new-feature
   ```

## Development Setup

1. Ensure you have Go 1.24.3 or later installed
2. Install dependencies:
   ```bash
   go mod download
   ```
3. Run tests:
   ```bash
   go test ./...
   ```
4. Run tests with coverage:
   ```bash
   go test -cover ./...
   ```

## Making Changes

### Code Style

- Follow standard Go conventions and idioms
- Use `gofmt` to format your code
- Use meaningful variable and function names
- Add comments for exported types, functions, and methods
- Keep line length reasonable (around 120 characters)

### Commit Messages

- Use clear and descriptive commit messages
- Start with a verb in present tense (e.g., "Add", "Fix", "Update")
- Keep the first line under 72 characters
- Reference issues when applicable (e.g., "Fix #123")

Example:

```
Add support for custom service validators

- Implement ServiceValidator interface
- Add validation during provider build
- Include tests for validation logic

Fixes #123
```

### Testing

- Write tests for new functionality
- Ensure all tests pass before submitting
- Aim for high test coverage
- Include both unit tests and integration tests where appropriate
- Test edge cases and error conditions

### Documentation

- Update the README.md if adding new features
- Add or update godoc comments for public APIs
- Include examples in documentation where helpful
- Update CHANGELOG.md following the existing format

## Submitting Changes

1. Ensure all tests pass:

   ```bash
   go test ./...
   ```

2. Run linters:

   ```bash
   go vet ./...
   golint ./...  # If you have golint installed
   ```

3. Push your changes to your fork:

   ```bash
   git push origin feature/my-new-feature
   ```

4. Create a Pull Request on GitHub

### Pull Request Guidelines

- Fill out the pull request template completely
- Link to any related issues
- Ensure CI checks pass
- Be responsive to review feedback
- Keep your branch up to date with main

## Reporting Issues

- Check existing issues before creating a new one
- Use issue templates when available
- Provide clear reproduction steps
- Include relevant error messages and stack traces
- Mention your Go version and operating system

## Feature Requests

- Check if the feature has been requested before
- Clearly describe the use case
- Provide examples of how the feature would be used
- Be open to discussion about implementation approach

## Code Review Process

All submissions require review before being merged:

1. A maintainer will review your PR
2. They may request changes or ask questions
3. Once approved, your PR will be merged
4. Your contribution will be acknowledged in the release notes

## Development Tips

### Running Specific Tests

```bash
# Run tests for a specific package
go test ./provider

# Run a specific test
go test -run TestServiceProvider_Resolve

# Run tests with verbose output
go test -v ./...
```

### Debugging

- Use `fmt.Printf` or a debugger for troubleshooting
- The `-race` flag can help identify race conditions:
  ```bash
  go test -race ./...
  ```

### Benchmarking

- Run benchmarks:
  ```bash
  go test -bench=. ./...
  ```
- Compare benchmarks when optimizing performance

## Areas for Contribution

Here are some areas where contributions are particularly welcome:

- **Documentation**: Improve examples, fix typos, add clarifications
- **Testing**: Increase test coverage, add edge case tests
- **Performance**: Optimize hot paths, reduce allocations
- **Features**: Implement requested features from issues
- **Bug Fixes**: Fix reported issues
- **Examples**: Add example applications using godi

## Questions?

If you have questions about contributing:

1. Check existing documentation
2. Look through existing issues and PRs
3. Open a new issue with your question

Thank you for contributing to godi!

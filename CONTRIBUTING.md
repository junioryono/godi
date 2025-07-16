# Contributing to godi

Thank you for your interest in contributing to godi! This document provides guidelines and instructions for contributing.

## Table of Contents

- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Making Changes](#making-changes)
- [Commit Message Convention](#commit-message-convention)
- [Pull Request Process](#pull-request-process)
- [Testing](#testing)
- [Release Process](#release-process)
- [Areas for Contribution](#areas-for-contribution)

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
   git checkout -b feat/my-new-feature
   # or
   git checkout -b fix/issue-123
   ```

## Development Setup

1. Ensure you have Go 1.23 or later installed
2. Install dependencies:
   ```bash
   go mod download
   ```
3. Run tests:
   ```bash
   make test
   # or with coverage
   make test-cover
   ```
4. Run linter:
   ```bash
   make lint
   ```

## Making Changes

### Code Style

- Follow standard Go conventions and idioms
- Use `gofmt` to format your code
- Run `make lint` before committing
- Use meaningful variable and function names
- Add comments for exported types, functions, and methods
- Keep line length reasonable (around 120 characters)

### Testing

- Write tests for new functionality
- Ensure all tests pass before submitting
- Aim for high test coverage (run `make test-cover`)
- Include both unit tests and integration tests where appropriate
- Test edge cases and error conditions

### Documentation

- Update godoc comments for any API changes
- Add examples in documentation where helpful
- Update README.md if adding new features
- No need to update CHANGELOG.md - it's automated!

## Commit Message Convention

**This project uses [Conventional Commits](https://www.conventionalcommits.org/) for automated versioning and changelog generation.**

### Format

```
<type>(<scope>): <subject>

<body>

<footer>
```

### Types

Must be one of the following:

- **feat**: A new feature (triggers minor version bump)
- **fix**: A bug fix (triggers patch version bump)
- **docs**: Documentation only changes
- **style**: Changes that don't affect code meaning (white-space, formatting, etc)
- **refactor**: Code change that neither fixes a bug nor adds a feature
- **perf**: Performance improvement
- **test**: Adding or updating tests
- **build**: Changes to build system or dependencies
- **ci**: Changes to CI configuration files and scripts
- **chore**: Other changes that don't modify src or test files
- **revert**: Reverts a previous commit

### Scope (Optional)

The scope should be the name of the package affected:

- `provider`
- `collection`
- `module`
- `lifetime`
- `descriptor`
- `errors`
- `inout`
- `scope`
- `resolver`

### Subject

- Use imperative, present tense: "change" not "changed" nor "changes"
- Don't capitalize first letter
- No dot (.) at the end
- Keep under 50 characters

### Breaking Changes

Breaking changes must be indicated by:

1. Adding `!` after the type/scope: `feat!: remove deprecated method`
2. Adding `BREAKING CHANGE:` in the footer:

   ```
   feat: change provider interface

   BREAKING CHANGE: ServiceProvider.GetService() renamed to Resolve()
   ```

### Examples

```bash
# Feature
feat(provider): add support for singleton lifetime

# Bug fix
fix(collection): resolve panic when registering nil provider

# Breaking change
feat(module)!: change Module interface signature

# Breaking change with footer
feat: redesign service resolution

BREAKING CHANGE: Provider.GetService() has been renamed to Provider.Resolve()
to better align with .NET naming conventions.

# Documentation
docs: update installation instructions in README

# With scope and body
fix(provider): prevent concurrent map access

This fix adds a mutex to protect the provider map from
concurrent access, resolving the race condition reported
in issue #23.

Fixes #23
```

## Pull Request Process

### Before Creating a PR

1. **Update your fork**:

   ```bash
   git fetch upstream
   git checkout main
   git merge upstream/main
   git push origin main
   ```

2. **Rebase your feature branch**:

   ```bash
   git checkout feat/my-feature
   git rebase main
   ```

3. **Run all checks**:
   ```bash
   make test-cover
   make lint
   ```

### PR Title Format

1. **PR Title**: Must follow conventional commit format: `type(scope): description`

**Examples:**

- ✅ `feat: add support for custom providers`
- ✅ `feat(provider): add singleton lifetime support`
- ✅ `fix: resolve memory leak in scope disposal`
- ✅ `fix(collection): handle nil provider registration`
- ✅ `docs: update installation instructions`
- ✅ `refactor(module): simplify initialization logic`
- ✅ `test: add coverage for error scenarios`
- ✅ `feat!: redesign service resolution API` (breaking change)

**Bad examples:**

- ❌ `Update code` (no type)
- ❌ `FEAT: Add feature` (wrong capitalization)
- ❌ `Fixed the bug` (past tense, should be imperative)
- ❌ `feat: Added new provider.` (period at end, past tense)

2. **PR Description**: Fill out the template completely

   - Describe what changes you've made
   - Explain why the changes are needed
   - Link to any related issues
   - Include examples if applicable

3. **Keep PRs focused**: One feature or fix per PR

### After Creating the PR

1. **CI Checks**: Ensure all checks pass

   - Tests on multiple OS/Go versions
   - Linting
   - Commit message validation

2. **Code Review**:

   - Respond to feedback promptly
   - Make requested changes
   - Push new commits (don't force-push during review)

3. **Before Merge**:
   - Squash commits if requested
   - Ensure PR title follows conventional format (this becomes the merge commit)

### What Happens After Merge

When your PR is merged to `main`:

1. CI runs all tests again
2. If tests pass and commits include `feat`, `fix`, or breaking changes:
   - Version is automatically bumped
   - Changelog is automatically updated
   - A new GitHub release is created
   - A git tag is pushed

Your contribution will be automatically included in the next release!

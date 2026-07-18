# Security Policy

## Supported Versions

Security fixes are provided for the latest patch release of the current major version. Older majors may receive a fix at the maintainer's discretion but are not guaranteed support.

Run godi with the latest security patch of its supported Go release. CI pins a
patched toolchain and scans every module with `gosec` and `govulncheck`.

## Private Reporting

Report suspected vulnerabilities through [GitHub's private vulnerability reporting](https://github.com/junioryono/godi/security/advisories/new).

Include:

- the affected godi and Go versions
- the impacted package or integration
- a minimal reproduction or proof of concept
- the expected impact and any known mitigations

Do not open a public issue or pull request containing exploit details. The maintainer will acknowledge a complete report, investigate it, and coordinate disclosure and a patched release when the report is confirmed.

For ordinary bugs without a security impact, use the public bug-report template.

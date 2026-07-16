# Security Policy

## Supported versions

| Version | Supported |
|---------|-----------|
| 1.x     | ✅ |
| < 1.0   | ❌ |

Security fixes are released as patch versions on the latest minor line. Pin to
an exact `vX.Y.Z` and follow the [upgrade guide](docs/upgrade.md).

## Reporting a vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Report privately using GitHub's
[private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability):
go to the repository's **Security** tab → **Report a vulnerability**.
Alternatively, email **sean@seantestenv.co.uk** with the details.

Please include, where possible:

- A description of the issue and its impact.
- Steps to reproduce or a proof of concept.
- Affected version(s) and configuration.
- Any suggested remediation.

### What to expect

- **Acknowledgement** within 5 business days.
- An initial assessment and severity within 10 business days.
- Coordinated disclosure: we will work with you on a fix and a disclosure
  timeline, and credit you in the advisory unless you prefer to remain
  anonymous.

## Scope

This project's security guarantee is that no plaintext repository content is
ever pushed to the destination; only age ciphertext and a structural manifest
are. See [docs/security.md](docs/security.md) for the full threat model,
including what metadata is intentionally not encrypted and what is out of scope.

Reports we are especially interested in:

- Any path by which plaintext file content could reach the destination.
- Webhook signature bypass, replay bypass, or authentication weaknesses.
- Path traversal / command or URL injection via repository or owner names.
- Credential (token) leakage via logs, process listings, or error output.

Out of scope: compromise of the host running the service (it necessarily
handles plaintext and tokens), and loss of the age private key (backups are
unrecoverable by design without it).

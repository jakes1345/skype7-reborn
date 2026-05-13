# Security

## Supported versions

Security fixes are applied to the **latest** `master` and the **current beta / stable tag** when one exists. Older tags may not receive backports.

## Reporting a vulnerability

Please **do not** file public GitHub issues for undisclosed security vulnerabilities.

1. Open a **private** security advisory for this repository (GitHub **Security** tab → **Report a vulnerability**), or  
2. Contact the maintainers through a channel they have published for security contact (if different).

Include: affected component (client vs Nexus), reproduction steps, impact assessment, and whether you need coordinated disclosure timing.

## Scope notes

- The relay is designed as **honest-but-curious**; message bodies and WebRTC signaling fields are **end-to-end encrypted** from the client’s perspective. Assume **metadata** (who talks to whom, timing, IP at the edge) is visible to the operator.
- **TOFU key pinning** means the **first** session with a new peer is especially sensitive to active attack at the relay path; see `README.md` threat model.

We appreciate responsible disclosure.

# Security Policy

## Reporting a vulnerability

**Please do not open a public issue for a security problem.** This project is self-hosted by
alliances running their own instances, so a public report is a disclosure to every deployment at
once, before any of them can update.

Use GitHub's private vulnerability reporting instead:

**[Report a vulnerability](https://github.com/shodiwarmic/lastwar-alliance-manager/security/advisories/new)**
— or from the repository's **Security** tab → **Report a vulnerability**.

That opens a private thread visible only to the maintainer. Include what you did, what happened,
and what you expected; a proof of concept helps but a clear description is enough.

This is a hobby project maintained by one person, so please don't expect a same-day reply. You
will get an acknowledgement, and if a report is valid you'll be credited in the fix unless you'd
rather not be.

## Supported versions

There is one supported version: the latest `main` / the latest published image. Fixes land on
`main` and ship in the next image; there are no maintained release branches to backport to.

## Scope

In scope: authentication and session handling, the permissions matrix and rank gating, the mobile
bearer-token API (`/api/mobile/*`), invite and password-reset token handling, file upload and
download, SQL injection, and stored or reflected XSS.

Out of scope: anything requiring an already-authenticated administrator (an admin can legitimately
change almost anything), rate-limiting of ordinary application endpoints, findings that depend on
a deployment ignoring the hardening steps below, and vulnerabilities in `lastrank.fun` — that is a
third-party volunteer-run service, not ours; report those to its operators.

## Hardening a deployment

Two steps matter more than anything else, and both are on the operator:

1. **Change the default administrator password immediately.** A fresh install seeds
   `admin` / `admin123` and forces a change at first login — but the account exists from first
   boot, so an instance exposed to the internet before that first login is exposed with a
   published password. Do the first login before opening it up.
2. **Set `SESSION_KEY`** to at least 32 random characters in any deployment running
   `PRODUCTION=true`. If it is unset the app generates an ephemeral key and logs a warning, which
   silently logs every user out on each restart.

Beyond that: put the app behind the provided Caddy reverse proxy so it is served over HTTPS with
the security headers and CSP that `install.sh` configures, and keep the container image current —
Dependabot keeps dependencies moving on `main`.

# Security

Report security issues through the repository's private security reporting
channel. Do not open a public issue with exploit details, tokens, hostnames,
logs, or clipboard contents.

Clipport moves local clipboard contents into SSH sessions. Its intended
security boundaries are:

- Local HTTP endpoints must bind only to loopback.
- Shim bearer tokens live at `~/.config/clipport/token` with `0600`
  permissions.
- Tokens must not be embedded in executable scripts.
- Remote image uploads stay under `/tmp/clipport/...`.

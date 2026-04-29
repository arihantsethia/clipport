# Agent Notes

- Keep `clipctl paste` stdout path-only for remote image success; remote text
  success writes the text directly to stdout.
- Keep remote paths under `/tmp/clipport/...`.
- Do not add config knobs without a real usage need.
- HTTP endpoints must bind only to loopback.
- Remote shim tokens must stay in `~/.config/clipport/token` with `0600`, not
  in executable scripts.
- Run these before claiming a code change is done:

```bash
gofmt -w <changed go files>
go test ./...
go vet ./...
```

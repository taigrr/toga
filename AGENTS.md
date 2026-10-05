# AGENTS.md

Guidance for AI agents and contributors working on toga.

## Versioning: tags track the Go release

**toga's version tag MUST match the Go version it ships with.** toga is a Go
module proxy; each release is built against (and pinned to, via `go.mod`) a
specific Go toolchain, and the tag advertises exactly that.

- Go `1.27.1` is current → toga is tagged `v1.27.1`.
- When Go `1.27.2` ships, bump `go.mod` and tag `v1.27.2`. Same for `1.28.0`.
- Never invent a version number. There is no "minor bump for a big change" —
  the Go version is the only input.

If a change needs to land between Go releases (an emergency dependency swap,
a CI fix, a security patch), it still **must not** get its own tag. Commit to
`master`, let CI run, and the change ships with the next Go-aligned tag.

### Why this rule exists

`v1.28.0` was cut ahead of Go 1.28.0 to ship an emergency migration off
`minio-go` (the `minio/minio` Docker image was pulled, breaking the release
pipeline for `v1.27.1`). That was wrong: the version now claims a Go release
that does not exist yet, and no further release can be tagged until Go 1.28.1
is out. Do not repeat this — if a release is blocked, fix `master` and re-run
the release workflow on the *correct* tag (delete and re-create the tag if the
release never published) rather than inventing a higher version.

### Checklist before tagging

1. `go.mod` `go` directive equals the target Go release.
2. Tag is `v<that exact version>`.
3. `go build ./... && go test -race ./...` pass locally.
4. Pushing the tag triggers `.github/workflows/release.yml` (GoReleaser).

## Development

- Format with `goimports`; lint with `staticcheck`; vet with `go vet`.
- Run the full suite with `go test -race ./...`. S3/MinIO/lister tests need
  Docker (they start a pinned `rustfs/rustfs` container via
  `internal/storage/s3test`).
- `govulncheck ./...` must report zero reachable vulnerabilities.
- Follow existing patterns in `internal/storage/*` when adding a backend; wire
  it in `cmd/toga/storage.go` and `cmd/toga/handler.go` (`newLister`).

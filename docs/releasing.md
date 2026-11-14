# Releasing

Follow these steps for every release. The performance step is part of the release: the [Performance](/performance) page must describe the version you ship.

1. **Changelog.** Put the changes under the new version in `CHANGELOG.md`. Say the database version and any config change.
2. **Tests.**
   ```sh
   go vet ./...
   go test -race ./...
   python3 -I spec/genvectors.py | diff - spec/vectors.json
   ```
3. **Build with the version number.**
   ```sh
   V=0.2.0
   CGO_ENABLED=0 go build -ldflags "-X main.version=$V" -o /tmp/tm ./cmd/tallymoth
   CGO_ENABLED=0 go build -o /tmp/bench ./tools/bench
   docker build --build-arg VERSION=$V -f deploy/Dockerfile -t tallymoth:bench .
   ```
4. **Run the benchmarks** on a machine with nothing else running.
   ```sh
   /tmp/bench -bin /tmp/tm -docker tallymoth:bench -docs docs/performance.md
   tools/bench/matrix.sh /tmp/tm /tmp/bench /var/tmp/bench-data /var/tmp/bench-out
   /tmp/bench -report /var/tmp/bench-out -docs docs/performance.md
   ```
5. **Read the change in `docs/performance.md`.** Compare it with the last release. A number that moved more than about 10% needs a reason in the changelog, or a fix. Check that no row says `timed out`, `Killed` or shows errors unless that is the point of the row.
6. **Update other places** that quote numbers: the landing page, [Docker](/docker) and the README.
7. **Commit and push to `main`.** The `docs` workflow publishes the site.
8. **Tag.** `git tag v$V && git push origin v$V`. The `release` workflow then publishes the downloads (Linux, macOS and Windows; amd64 and arm64) as a GitHub release, and the Docker image `ghcr.io/javedh-dev/tallymoth` for amd64 and arm64.

## Why benchmarks are part of a release

A database that gets a little slower with each change is only noticed when a dashboard starts timing out. The same numbers for every release make the trend visible, and give users something to plan with. The History table on the Performance page is that record.

## Version numbers

- **Patch:** fixes, no config or database change.
- **Minor:** new features and optional fields, maybe a database upgrade. Old config files keep working.
- **Major** (`/v2`, or config `version: 2`): a breaking change, announced in the changelog before release. See [Architecture](/architecture#keeping-things-compatible).

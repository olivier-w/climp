# Releasing `climp`

`climp` releases are driven by Git tags.

- Pushing a tag matching `v*` triggers [`.github/workflows/release.yml`](.github/workflows/release.yml)
- The workflow builds Linux, macOS, and Windows binaries
- The workflow verifies `climp --version`
- The workflow uploads archives to the matching GitHub Release
- Scoop is updated separately via GoReleaser and [`olivier-w/scoop-bucket`](https://github.com/olivier-w/scoop-bucket)

## Prerequisites

- clean `main` branch
- `git`, `go`, `gh`, and `goreleaser` installed locally
- permission to push to `olivier-w/climp`
- a `GITHUB_TOKEN` that can update `olivier-w/scoop-bucket`

## 1. Prepare the release locally

Run the helper script with the version you want to publish:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\release.ps1 -Version v0.3.0 -UpdateReadme
```

This will:

- verify required tools are installed
- show commits since the last tag
- run:
  - `go build -o climp.exe .`
  - `go vet ./...`
  - `go test ./...`
- update the hardcoded README release links to the target version

Note: updating the README before the tag exists means those download links will briefly point at a release that does not exist yet. Keep that commit close to the actual release.

## 2. Commit the release prep

```powershell
git checkout main
git pull --ff-only origin main
git add README.md RELEASING.md scripts\release.ps1
git commit -m "docs: prepare v0.3.0 release"
git push origin main
```

## 3. Create and push the tag

```powershell
git tag v0.3.0
git push origin v0.3.0
```

## 4. Wait for GitHub Actions

The release workflow should produce these archives:

- `climp_v0.3.0_linux_amd64.tar.gz`
- `climp_v0.3.0_linux_arm64.tar.gz`
- `climp_v0.3.0_darwin_amd64.tar.gz`
- `climp_v0.3.0_darwin_arm64.tar.gz`
- `climp_v0.3.0_windows_amd64.zip`

Verify the release exists:

```powershell
gh release view v0.3.0 --repo olivier-w/climp
gh release view v0.3.0 --repo olivier-w/climp --json assets
```

## 5. Update Scoop

After the GitHub Release is live:

```powershell
$env:GITHUB_TOKEN = "<token with repo access>"
goreleaser release --clean
```

If you want to try using GoReleaser only for the Scoop update, test this path first:

```powershell
$env:GITHUB_TOKEN = "<token with repo access>"
$env:SCOOP_ONLY = "1"
goreleaser release --clean
```

## 6. Post-release verification

- confirm all 5 archives are present on the GitHub Release
- confirm the Windows archive name uses the `v` prefix
- confirm `climp --version` prints the new tag from a release binary
- confirm the README links resolve correctly
- confirm Scoop updates to the new version

## Failure guidance

- If the workflow fails before uploading assets, fix `main` and cut a new patch release unless the bad tag never shipped usable artifacts.
- If one archive is missing, rerun the failed workflow job and let the publish step upload with `--clobber`.
- If Scoop publishing fails, update `olivier-w/scoop-bucket` manually and keep the GitHub Release as the source of truth.

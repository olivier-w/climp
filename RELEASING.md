# Releasing `climp`

`climp` releases are driven by Git tags.

- Pushing a tag matching `v*` triggers [`.github/workflows/release.yml`](.github/workflows/release.yml); the workflow accepts only `vMAJOR.MINOR.PATCH` with an optional prerelease suffix
- The workflow builds Linux, macOS, and Windows binaries
- The workflow verifies `climp --version`
- The workflow uploads archives to a draft GitHub Release for review
- Scoop is updated separately via GoReleaser and [`olivier-w/scoop-bucket`](https://github.com/olivier-w/scoop-bucket)

## Prerequisites

- clean `main` branch
- `git`, `go`, `gh`, and `goreleaser` installed locally
- permission to push to `olivier-w/climp`
- a `GITHUB_TOKEN` that can update `olivier-w/scoop-bucket`

## 1. Prepare the release locally

Run the helper script with the version you want to publish:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\release.ps1 -Version v0.3.0
```

This will:

- verify required tools are installed
- show commits since the last tag
- run:
  - `go build -o climp.exe .`
  - `go vet ./...`
  - `go test ./...`
- leave the existing README download links in place until the release is public

Review the release notes and test results before creating the tag.

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

The release workflow should create a draft release with these archives:

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

## 5. Review and publish

Download and inspect all five draft archives. Test each platform when a suitable machine is available. Confirm the version output, Go build metadata, and release notes. Record any platform that could not be tested. The workflow does not run playback tests.

After approval, publish the draft in GitHub. Then update the README links to the published version.

## 6. Update Scoop

Review and update `olivier-w/scoop-bucket` separately after publication. Do not run `goreleaser release --clean` for a Scoop-only update without first verifying its effects: the current GoReleaser configuration can replace release assets.

## 7. Post-release verification

- confirm all 5 archives are present on the GitHub Release
- confirm the Windows archive name uses the `v` prefix
- confirm `climp --version` prints the new tag from a release binary
- inspect each binary with `go version -m` and confirm that its Go version meets `go.mod`
- use FFmpeg/ffprobe 9.0.2 or later and yt-dlp 2026.10.21 or later; these tools are installed separately from Climp
- check trusted and rejected certificates, including HLS child requests, before release
- confirm the README links resolve correctly
- confirm Scoop updates to the new version

## Failure guidance

- If the workflow fails before uploading assets, fix `main` and cut a new patch release unless the bad tag never shipped usable artifacts.
- If one archive is missing from a draft, rerun the workflow and review the replacement artifacts before publication. The workflow refuses to replace assets on a published release.
- If Scoop publishing fails, update `olivier-w/scoop-bucket` manually and keep the GitHub Release as the source of truth.

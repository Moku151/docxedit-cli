---
status: accepted
---

# Distribute verified multi-platform builds with GitHub

GitHub Actions provides both short-lived CI artifacts and durable GitHub Release assets for `docxedit`. Builds target macOS, Linux, and Windows on both `amd64` and `arm64`; artifacts may only be produced after `go test ./...` and `go vet ./...` succeed on all three operating systems and the full six-target build matrix compiles successfully. This trades additional runner time and storage for a consistent download experience and catches defects in the operating-system-specific safe-replacement paths before publication.

Every commit on `main` triggers the publishing workflow and updates the release if it passes verification while still being the branch head. Pull requests and `main` commits also retain CI artifacts for 14 days. Release binaries use `.tar.gz` archives on macOS and Linux and `.zip` archives on Windows; each release includes `SHA256SUMS` and GitHub build-provenance attestations. This deliberately favors immediate availability and verifiable origin over a tag-driven release ceremony or platform-specific signing certificates.

Publication updates one rolling `continuous` release instead of creating an immutable release for every commit. The release represents the latest successfully verified development state of `main` and makes no stability or compatibility promise. Short-lived CI artifacts preserve recent per-commit builds without turning the GitHub Releases page into a permanent build log.

Runs may verify commits concurrently, but a run updates the rolling release only while its commit is still the head of `main`; an older, slower run must never replace a newer result. Release asset names such as `docxedit_continuous_linux_amd64.tar.gz` remain stable across updates so consumers can use durable download URLs, while the commit identity is carried separately in release and provenance metadata.

The mutable `continuous` Git tag follows the verified commit so the release's source archives and binaries refer to the same source. Release binaries expose that identity through `docxedit --version` as `continuous+<commit-sha>`, while local builds report `devel`; no build timestamp is embedded. Workflow actions are pinned to complete commit SHAs and checked weekly by Dependabot. The README provides stable links and verification commands, but no remote installation script is executed on users' machines.

GitHub's immutable-release setting must remain disabled because it prevents both moving the `continuous` tag and replacing its assets. The repository had immutable releases disabled when this pipeline was introduced; enabling them later requires replacing the rolling-release model with immutable per-version releases.

GitHub replaces existing release assets non-atomically: an upload failure can leave a temporarily incomplete set. Archives are uploaded before the new checksum manifest, so a partial update is detectable through missing or mismatched checksums and attestations; the next successful current `main` run repairs the rolling release.

# Release Guide

The target audience for this guide is maintainers of this repo. In general, the release process is as follows:

1. Create a release branch
1. Create a release candidate
1. Test the release candidate
    1. If the release candidate is not satisfactory, go back to step 1
    1. If the release candidate is satisfactory, create an official release

## Notes

The versions of previous binaries are hard-coded at multiple places in celestia-app due to the multiplexer (until <https://github.com/celestiaorg/celestia-app/issues/4921> is resolved). In order to include code changes to historical binaries, you need to make a release of that code change and bump the hard-coded versions.

## Release branch

1. Navigate to <https://github.com/celestiaorg/celestia-app/branches>.
2. Click **New Branch**.
3. Create a release branch. Example name: `v4.1.0-release`. Example source: `v4.x`.

## Release Candidate

### Creating a release candidate

1. Navigate to <https://github.com/celestiaorg/celestia-app/releases/new>.
1. Choose a version tag based on [Semantic Versioning](https://semver.org/). Include the `-rc` suffix followed by the next integer. RCs start at 0.
1. Change the target branch to the branch you created in the previous section. Example `v4.1.0-release`.
1. Click **Generate release notes**.
1. Toggle on the **Set as a pre-release** checkbox.
1. **Publish release**.

### Release notes content

After GitHub generates the auto-generated commit list, edit the release notes to add the following sections at the top, before the auto-generated content:

1. **Upgrade Notice** — notable changes for node operators or library consumers (existing convention).
2. **Supported operating systems** — required for every release. Use this template:

   ```markdown
   ## Supported operating systems

   See the [canonical matrix in the README](https://github.com/celestiaorg/celestia-app/blob/<release-tag>/README.md#supported-operating-systems).

   - **Tested in CI**: <e.g., Ubuntu 24.04 LTS (Noble Numbat) on x86_64>
   - **Prebuilt binaries provided**: Linux (`amd64`, `arm64`), macOS (`amd64`, `arm64`). arm64 binaries are cross-compiled and are not executed in CI.
   - **Minimum glibc**: <version, e.g., 2.38>. <List incompatible distros, e.g., "Ubuntu 22.04 and older are not supported for multiplexer builds and will fail to start with a glibc version mismatch error.">
   - **Changes since the previous release**: <list any matrix changes, or "none">
   ```

   Pin the README link to the release tag (not `main`) so the matrix users read matches what was tested for *this* release.

3. **Link to** [docs/release-notes/release-notes.md](https://github.com/celestiaorg/celestia-app/blob/main/docs/release-notes/release-notes.md) for breaking changes (existing convention). Unlike the README link in item 2, this URL stays pinned to `main` because the release-notes file grows with each release, and `main` always reflects the most complete history.

If the CI OS matrix in `.github/workflows/*.yml` has changed since the previous release, you must also:

- Update the supported-OS content in `README.md` under `## Supported operating systems` (CI table, prebuilt-binaries section, glibc constraint, known incompatibilities — whichever changed).
- Update the `#### Supported operating systems` subsection under the new version's `### Node Operators` heading in `docs/release-notes/release-notes.md`.

These requirements apply to release candidates, testnet releases, and mainnet releases. The Testnet Release and Mainnet Release sections below reuse this content.

### After creating the release candidate

1. Wait until CI passes on the release and verify that prebuilt binaries were attached to the release.
1. Create a PR to bump the celestia-app dependency in [celestia-node](https://github.com/celestiaorg/celestia-node).
1. [Optional] Start a testnet via auto-devops that uses the release candidate. Confirm it works.
1. [Optional] Use the release candidate to sync from genesis. Confirm it works.

## Testnet Release

### Creating a pre-release

Follow the [creating a release candidate](#creating-a-release-candidate) section with the following considerations:

- The version tag should not include the `-rc` suffix. Instead append the release with `-arabica` or `-mocha` depending on the target network.
- The release notes should contain an **Upgrade Notice** section with notable changes for node operators or library consumers.
- The release notes section should contain a link to <https://github.com/celestiaorg/celestia-app/blob/main/docs/release-notes/release-notes.md> where we capture breaking changes
- The release notes should contain a **Supported operating systems** section per the [Release notes content](#release-notes-content) guide.

### After creating the release

1. Wait until CI passes on the release and verify that prebuilt binaries were attached to the release.
1. Create a PR to bump the celestia-app dependency in [celestia-node](https://github.com/celestiaorg/celestia-node).

## Mainnet Release

Follow the [creating a release candidate](#creating-a-release-candidate) section with the following considerations:

- The version tag should not include the `-rc`, `-arabica`, or `-mocha` suffix.
- Toggle off the **Set as a pre-release** checkbox.
- Toggle on the **Set as the latest release** checkbox.
- The release notes should contain a **Supported operating systems** section per the [Release notes content](#release-notes-content) guide.

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

### After creating the release

1. Wait until CI passes on the release and verify that prebuilt binaries were attached to the release.
1. Create a PR to bump the celestia-app dependency in [celestia-node](https://github.com/celestiaorg/celestia-node).

## Mainnet Release

Follow the [creating a release candidate](#creating-a-release-candidate) section with the following considerations:

- The version tag should not include the `-rc`, `-arabica`, or `-mocha` suffix.
- Toggle off the **Set as a pre-release** checkbox.
- Toggle on the **Set as the latest release** checkbox.

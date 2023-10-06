# `x/upgrade`

## Abstract

This upgrade module is a fork of the cosmos-sdk's
[x/upgrade](https://github.com/cosmos/cosmos-sdk/tree/main/x/upgrade) module
that removes the entrypoints to the standard upgrade module by not registering a
message server.

The goal of this approach is to force social consensus to reach an upgrade
instead of relying on token voting. It works by simply removing the ability of
the gov module to schedule an upgrade. This way, the only way to upgrade the
chain is to agree on the upgrade logic and the upgrade height offchain.

This fork registers the standard upgrade module types to preserve the ability to
marshal them. Additionally the keeper of the standard upgrade module is still
added to the application.

## Resources

1. <https://github.com/celestiaorg/celestia-app/pull/1491>
1. <https://github.com/cosmos/cosmos-sdk/blob/v0.46.15/x/upgrade/README.md>
1. <https://github.com/cosmos/cosmos-sdk/blob/v0.46.15/x/gov/README.md>

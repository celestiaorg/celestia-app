# `x/upgrade`

## Abstract

This upgrade module is a fork of the cosmos-sdk's
[x/upgrade](https://github.com/cosmos/cosmos-sdk/tree/main/x/upgrade) module
that removes the entrypoints to the standard upgrade module by not registering a
message server. It registers the standard upgrade module types to preserve the
ability to marshal them. Note that the keeper of the standard upgrade module is
still added to the application.

A consequence of the removal of the entrypoints is that
[cosmosvisor](https://github.com/cosmos/cosmos-sdk/tree/main/tools/cosmovisor)
will not work for celestia-app.

## Resources

1. <https://github.com/celestiaorg/celestia-app/pull/1491>

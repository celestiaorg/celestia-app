# ADR 024: Remove Bridge Node

## Changelog

- 2023/10/20: Initial draft

## Status

Proposed

## Context

As outlined in the first phase of [ADR-022](./adr-022-system-merge.md), the initial step in streamlining the architecture of the system is to run the consensus node through `celestia-node`'s binary with the goal of removing the separate bridge node and enabling the consensus node to directly publish the extended data squares to the DA network.

## Design

**Finalize Block**




**Node Package**

`celestia-app` will have a new package `node` that is responsible for initializing the file directory system for the consensus node and running the node based on a given file directory system. These two functionalities are similar to the two commands `init` and `start` in the `cmd` package.

`Start()` will have an additional callback argument:

 ```go
 func (core.Header, core.Commit, core.ValidatorSet, rsmt2d.ExtendedDataSquare)
 ```

This callback will be called every time the consensus node finalizes a block and pushes the EDS to the DA network.

node needs to run background services which are connected with das.Store for serving shares for example. We will need a struct for this,

In `celestia-node`, the push callback function will be wired to the storage engine 

We need to move the caching mechanism into the NMT package so both app and node can use it.

## Consequences

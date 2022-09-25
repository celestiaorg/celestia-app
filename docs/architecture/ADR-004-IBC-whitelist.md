# ADR 004: IBC Allowlist

## Terminology

All terminology is defined in [ICS 024](https://github.com/cosmos/ibc/tree/main/spec/core/ics-024-host-requirements) and some references are from corresponding implementation of [ICS 002](https://github.com/cosmos/ibc/tree/main/spec/core/ics-002-client-semantics) in [ibc-go](https://github.com/cosmos/ibc-go/blob/da1b7e0aaf4b7d466b1a7d1ed4f5d81149ff1d5b/modules/core/02-client)

## Changelog

- 2022-03-03: Initial Commit

## Context

While enabling IBC, we want to connect, and allow messages from selected chains. However, we don't want the ability for an arbitrary entity to create an IBC connection with a zone. This is so that we can keep the state machine as minimal and focused as possible.

ICS specification indicates that is possible by providing a custom `validateClientIdentifier`, but no such functionality exists currently.

Secondly, the ICS specification dictates that `createClient` takes in an `Identifier`. A potential solution could be to create a store for allowed `Identifiers` at genesis, and reject creation of clients for invalid identifiers. However, this is not true for the current state of the implementation. Rather, client ID is generated from an incremental counter `NextClientSequence` and the client type.

## Proposal

Fork IBC, and create a store of whitelisted public keys. Only `createClient` txns that are signed by private keys corresponding to whitelisted/stored public keys are deemed valid, and the rest are invalidated. Specific keys will only be required for client creation, and rest of IBC will work as is. 

## Alternative approaches

1. Simplest solution is fork to IBC, and add a small change to disallow creation/registration of new clients. Then we can create clients for the chains that we want to allow at genesis, and effectively create a whitelist. However, adding new clients or removing clients is not feasible under this.

2. Add a middleware to revert packets from non-whitelisted chains. This still adds state bloat since clients and corresponding connections are allowed to be established, but adds economic incentive in terms of lost gas from the malicious user/actor
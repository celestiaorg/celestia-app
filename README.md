# Fibre DA Specification

This is a specification of a DA protocol that extends Celestia. It uses a verifiable form of erasure coding to disseminate data such that it can be retrieved under honest majority assumptions without requiring full replication.

The specification is separated into the following sections:

1. [**Client**](./client.md): Captures the user facing API, describes how rsema1d is used, how the data is diseminated, and how the user can manage their accounts and pay for fibre blobs.
2. [**Server**](./server.md): Captures the API for storing user data, verifying correct construction and valid payment.
3. [**SDK Module**](./sdk_module.md): Specifies how the state machine handles payment, verifyig validator signatures and deducting from the escrow account.
4. [**Encoding**](./encoding.md): Specifies the encoding format of rows and the format of the shares in the original data square.

For specification of the rsema1d codec refer to this [document](https://github.com/celestiaorg/rsema1d/blob/main/SPEC.md)

# Multisig

Celestia inherits support for Multisig accounts from the Cosmos SDK. Multisig accounts behave similarly to regular accounts with the added requirement that a threshold of signatures is needed to authorize a transaction.

The maximum number of signatures allowed for a multisig account is determined by the parameter `auth.TxSigLimit` (see [parameters](./parameters.md)). The threshold and list of signers for a multisig account are set at the time of creation and can be viewed in the `pubkey` field of a key. For example:

```shell
$ celestia-appd keys show multisig
- address: celestia17rehcgutjfra8zhjl8675t8hhw8wsavzzutv06
  name: multisig
  pubkey: '{"@type":"/cosmos.crypto.multisig.LegacyAminoPubKey","threshold":2,"public_keys":[{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"AxMTEFDH8oyBPIH+d2MKfCIY1yAsEd0HVekoPaAOiu9c"},{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"Ax0ANkTPWcCDWy9O2TcUXw90Z0DxnX2zqPvhi4VJPUl5"},{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"AlUwWCGLzhclCMEKc2YLEap9H8JT5tWq1kB8BagU1TVH"}]}'
  type: multi
```

Please see the [Cosmos SDK docs](https://docs.cosmos.network/main/user/run-node/multisig-guide#step-by-step-guide-to-multisig-transactions) for more information on how to use multisig accounts.

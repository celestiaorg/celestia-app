# Modify Param

This doc contains steps to modify a parameter via governance. There are two ways to modify a parameter:

1. Legacy gov proposal (`submit-legacy-proposal`)
2. New gov proposal (`submit-proposal`)

This guide uses the new gov proposal format.

## Prerequisites

Verify the current parameter values:

```shell
$ celestia-appd query blob params
params:
  gas_per_blob_byte: 8
  gov_max_square_size: "128"

$ celestia-appd query consensus params
params:
  block:
    max_bytes: "8388608"
    max_gas: "-1"
  evidence:
    max_age_duration: 337h0m0s
    max_age_num_blocks: "242640"
    max_bytes: "1048576"
  validator:
    pub_key_types:
    - ed25519
  version:
    app: "6"
```

## Steps

1. Create a proposal.json file with contents:

```json
{
  "messages": [
    {
      "@type": "/celestia.blob.v1.MsgUpdateBlobParams",
      "authority": "celestia10d07y265gmmuvt4z0w9aw880jnsr700jtgz4v7",
      "params": {
        "gas_per_blob_byte": 8,
        "gov_max_square_size": 512
      }
    },
    {
      "@type": "/cosmos.consensus.v1.MsgUpdateParams",
      "authority": "celestia10d07y265gmmuvt4z0w9aw880jnsr700jtgz4v7",
      "block": {
        "max_bytes": "134217728",
        "max_gas": "-1"
      },
      "evidence": {
        "max_age_num_blocks": "242640",
        "max_age_duration": "337h0m0s",
        "max_bytes": "1048576"
      },
      "validator": {
        "pub_key_types": ["ed25519"]
      }
    }
  ],
  "metadata": "",
  "deposit": "10000000000utia",
  "title": "Increase Max Square Size to 512 and Block Size to 128 MiB",
  "summary": "Increase Max Square Size to 512 and Block Size to 128 MiB",
  "expedited": false
}
```

1. Submit the proposal and vote on it:

    ```shell
    # Export a variable for the key that will be used to submit the proposal
    export FROM="validator"
    export FEES="210000utia"
    export GAS="auto"
    export GAS_ADJUSTMENT="1.5"

    # Submit the proposal
    celestia-appd tx gov submit-proposal proposal.json --from $FROM --fees $FEES --gas $GAS --gas-adjustment $GAS_ADJUSTMENT

    # Query the proposals
    celestia-appd query gov proposals --output json | jq .

    # Export a variable for the relevant proposal ID based on the output from the previous command
    export PROPOSAL_ID=1

    # Vote yes on the proposal
    celestia-appd tx gov vote $PROPOSAL_ID yes --from $FROM --fees $FEES --gas $GAS --gas-adjustment $GAS_ADJUSTMENT --yes
    ```

## After the proposal passes

Repeat the prerequisite steps to verify that the parameter values have been updated.

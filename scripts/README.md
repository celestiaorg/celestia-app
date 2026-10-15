# Scripts

This directory contains a handful of scripts that may be helpful for contributors.

## gov-cancel-refund-proposal.sh

Generate a governance proposal that sets only `proposal_cancel_ratio` to `0.05`.
This refunds 95% of each deposit on voluntary cancellation and retains a 5%
charge during both deposit and voting periods.
The script requires `jq` and writes JSON without submitting a transaction.

The intended rollout is alongside the v11 network upgrade. This parameter change
is activated by governance execution, not by the upgrade itself: coordinate the
proposal timing with v11 and re-query parameters at that time. The generated
proposal has no scheduled activation height or app-version condition.

Query a trusted mainnet node with a mainnet-compatible `celestia-appd` binary:

```sh
celestia-appd query gov params --node "$NODE" --output json > gov-params.json
sh scripts/gov-cancel-refund-proposal.sh gov-params.json > gov-cancel-refund.json
jq '.messages[0].params' gov-cancel-refund.json
```

`MsgUpdateParams` replaces the entire parameter set. The script copies all queried
parameters, changes only the cancellation ratio, and uses the current minimum
deposit for an ordinary proposal. Re-query immediately before submission and
compare the full parameter set with the generated proposal to avoid reverting
intervening governance changes. Add the final discussion link to `metadata`.

After reviewing the payload, an operator can submit it for governance approval:

```sh
celestia-appd tx gov submit-proposal gov-cancel-refund.json \
  --chain-id celestia --node "$NODE" --from "$KEY" \
  --gas auto --gas-adjustment 1.5 --fees "$FEES"
```

The change activates only if the proposal passes and executes. Afterwards, query
`gov params` again and verify `proposal_cancel_ratio` is `0.050000000000000000` and all other
parameters retain their expected values. Cancellation transaction fees still
apply. The separate quorum, prevote and veto burn settings are preserved.

This uses the existing `/cosmos.gov.v1.MsgUpdateParams` handler. On
[v9.0.4](https://github.com/celestiaorg/celestia-app/blob/v9.0.4/app/param_filters.go),
governance parameter updates are not among the restricted parameter messages.
The [SDK parameter validation](https://github.com/celestiaorg/cosmos-sdk/blob/v0.52.8/x/gov/types/v1/params.go)
accepts a 5% cancellation ratio, and its
[deposit handler](https://github.com/celestiaorg/cosmos-sdk/blob/v0.52.8/x/gov/keeper/deposit.go)
then refunds 95% of each deposit. With an empty `proposal_cancel_dest`, the
remaining 5% is burned. A binary upgrade or v11 migration is unnecessary.
This does not reimburse deposits burned before the parameter change.

A 1,000 TIA deposit would incur a 50 TIA charge instead of 500 TIA; a 10,000 TIA
deposit would incur a 500 TIA charge instead of 5,000 TIA. This retains a cost
for recycling deposits or cancelling before a veto result, though cancelling
can still be cheaper than losing the entire deposit to a veto.

## test_cover.sh

Run `make test-coverage` from the repository root to generate `coverage.txt`.
The script runs the full test suite serially, excluding `test/util` packages
from test selection. Cross-package tests contribute coverage to the packages
they exercise. Packages without tests remain in scope.
Duplicate profile blocks are combined so their statements are counted once.

Only production code is in scope. The script does not instrument `test/`,
`tools/`, `scripts/`, `local_devnet/` or `pkg/rsema1d/cmd/testvectors`, and drops
generated `*.pb.go` and `*.pb.gw.go` files. `.github/codecov.yml` ignores the
same paths, so local and Codecov reports agree.

The report is replaced only after all tests pass. A failed run leaves an existing
report unchanged, so check the command's exit status before using the report.
Run `bash scripts/test_cover_test.sh` for a small regression check of the script.

## build-run-single-node.sh

This script will build the project and run a single node devnet. After running this script, the text output will contain a "Home directory" that you can use as a parameter for subsequent commands.

```bash
./scripts/build-run-single-node.sh
Home directory: /var/folders/_8/ljj6hspn0kn09qf9fy8kdyh40000gn/T/celestia_app_XXXXXXXXXXXXX.XV92a3qx
--> Updating go.mod
...
```

In a new terminal tab, export the home directory:

```bash
export CELESTIA_APP_HOME=/var/folders/_8/ljj6hspn0kn09qf9fy8kdyh40000gn/T/celestia_app_XXXXXXXXXXXXX.XV92a3qx
```

In subsequent commands, pass the `--home $CELESTIA_APP_HOME` flag:

```bash
./build/celestia-appd keys list validator --home $CELESTIA_APP_HOME
- address: celestia1grvklux2yjsln7ztk6slv538396qatckqhs86z
  name: validator
  pubkey: '{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"A5R27GO4uGtzu7LVOxneiA3i59Bi7SlDr6FHaGfy47mI"}'
  type: local
```

Note: this script is used in <https://github.com/celestiaorg/docs> so please update the docs repo if you make breaking changes to this script.

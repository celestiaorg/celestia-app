#!/bin/bash

go run ./tools/chainbuilder --num-blocks 100000 --block-size 32000000 --chain-id 32mb-100k

systemctl daemon-reload
systemctl start txsim

celestia-appd start  --minimum-gas-prices 0.000001utia --home ~/chainbuilder/generated/celestia-app/testnode-32mb-100k --grpc.enable

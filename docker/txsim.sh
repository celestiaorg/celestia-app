#!/bin/bash

CREATE_KEY=0
KEY_PATH="/home/celestia"
GRPC_ENDPOINT=""
POLL_TIME=""
BLOB=0
BLOB_AMOUNTS="1"
BLOB_SIZES="100-1000"
KEY_MNEMONIC=""
SEED=0
SEND=0
SEND_AMOUNT=1000
SEND_ITERATIONS=1000
STAKE=0
STAKE_VALUE=1000

while getopts "k:p:f:g:t:b:a:s:m:d:e:i:v:u:w:" opt; do
  case ${opt} in
    k )
      CREATE_KEY=$OPTARG
      ;;
    p )
      KEY_PATH=$OPTARG
      ;;
    g )
      GRPC_ENDPOINT=$OPTARG
      ;;
    t )
      POLL_TIME=$OPTARG
      ;;
    b )
      BLOB=$OPTARG
      ;;
    a )
      BLOB_AMOUNTS=$OPTARG
      ;;
    s )
      BLOB_SIZES=$OPTARG
      ;;
    m )
      KEY_MNEMONIC=$OPTARG
      ;;
    d )
      SEED=$OPTARG
      ;;
    e )
      SEND=$OPTARG
      ;;
    i )
      SEND_AMOUNT=$OPTARG
      ;;
    v )
      SEND_ITERATIONS=$OPTARG
      ;;
    u )
      STAKE=$OPTARG
      ;;
    w )
      STAKE_VALUE=$OPTARG
      ;;
    \? )
      echo "Invalid option: $OPTARG" 1>&2
      exit 1
      ;;
    : )
      echo "Invalid option: $OPTARG requires an argument" 1>&2
      exit 1
      ;;
  esac
done
shift $((OPTIND -1))

if [ "$CREATE_KEY" -eq 1 ]; then
  echo "Creating a new keyring-test for the txsim"
  /bin/celestia-appd keys add sim --keyring-backend test --home $KEY_PATH
  sleep 5
fi

# Running a tx simulator
txsim --key-path $KEY_PATH \
 --grpc-endpoint $GRPC_ENDPOINT \
 --poll-time $POLL_TIME \
 --blob $BLOB \
 --blob-amounts $BLOB_AMOUNTS \
 --blob-sizes $BLOB_SIZES \
 --key-mnemonic "$KEY_MNEMONIC" \
 --seed $SEED \
 --send $SEND \
 --send-amount $SEND_AMOUNT \
 --send-iterations $SEND_ITERATIONS \
 --stake $STAKE \
 --stake-value $STAKE_VALUE \
 --ignore-failures

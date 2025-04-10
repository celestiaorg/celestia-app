#!/bin/bash

start_time=$(date +%s)

# Trap Ctrl+C (SIGINT) to print elapsed time
trap ' 
  end_time=$(date +%s)
  elapsed=$((end_time - start_time))
  echo ""
  echo "⏱️  Script ran for $elapsed seconds"
  exit 0
' SIGINT

while true; do
    echo "Querying bob balance"
    echo ""
    celestia-appd q bank balances celestia10hftwtnxr3zn7k0c2mfhzq58xllv946qmuufeu
    
    echo "Sending bank tx from alice -> bob"
    echo ""
    celestia-appd tx bank send alice celestia10hftwtnxr3zn7k0c2mfhzq58xllv946qmuufeu 10utia --fees 400utia --from alice -y

    echo ""
    echo ""
    sleep 0.5
done
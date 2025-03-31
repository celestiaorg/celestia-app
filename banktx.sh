#!/bin/bash

while true; do
    echo "querying bob balance\n"
    celestia-appd q bank balances celestia10hftwtnxr3zn7k0c2mfhzq58xllv946qmuufeu
    echo "sending bank tx from alice -> bob\n"
    celestia-appd tx bank send alice celestia10hftwtnxr3zn7k0c2mfhzq58xllv946qmuufeu 10utia --fees 400utia --from alice -y
    sleep 0.5
done
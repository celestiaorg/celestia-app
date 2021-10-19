FROM golang:1.17-alpine AS builder
RUN apk update && \
    apk upgrade && \
    apk --no-cache add make \
    git libc-dev bash gcc linux-headers eudev-dev python3

WORKDIR /go/src/github.com/celestiaorg/celestia-app

# Add source files
COPY . .

RUN make install

FROM golang:1.17-alpine

ENV APP /capp

RUN apk update && \
    apk upgrade && \
    apk --no-cache add curl jq bash && \
    addgroup cappuser && \
    adduser -S -G cappuser cappuser -h "$APP"

WORKDIR $APP

# Run the container with cappuser by default
USER cappuser

WORKDIR $APP

COPY --from=builder /go/bin/celestia-appd /usr/bin/celestia-appd

EXPOSE 26656 26657 1317 9090
ADD entry.sh /capp/entry.sh
USER root
RUN chmod +x /capp/entry.sh
USER cappuser
ENTRYPOINT [ "/capp/entry.sh" ]
# maybe hive commands below
# RUN celestia-appd keys add my_duck --keyring-backend test
# RUN MY_ADDR=$(celestia-appd keys show my_duck -a --keyring-backend test)
# RUN celestia-appd init dockthemoose --chain-id celes-net 
# a bash script that changes the genesis file to whatever is needed down below

# RUN celestia-appd add-genesis-account $MY_ADDR 10000000stake,1000token

# Create a gentx.
# RUN celestia-appd gentx $MY_ADDR 100000stake --chain-id test --keyring-backend celes-net 

# Add the gentx to the genesis file.
# RUN celestia-appd collect-gentxspwd

# port to expose 26656 26657



# THIS WORKS! BELOW! 
# celestia-appd init test --chain-id test
# celestia-appd keys add user1 --keyring-backend test
# celestia-appd add-genesis-account <address from above command> 10000000stake,1000token
# celestia-appd gentx user1 100000stake --chain-id test
# celestia-appd collect-gentxs
# celestia-appd start
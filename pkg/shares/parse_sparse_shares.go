package shares

import (
	"bytes"
	"encoding/binary"

	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
)

// parseSparseShares iterates through raw shares and parses out individual messages.
func parseSparseShares(shares [][]byte) ([]coretypes.Message, error) {
	if len(shares) == 0 {
		return nil, nil
	}
	// msgs returned
	msgs := []coretypes.Message{}
	currentMsgLen := 0
	currentMsg := coretypes.Message{}
	// whether the current share contains the start of a new message
	isNewMessage := true
	// the len in bytes of the current chunk of data that will eventually become
	// a message. This is identical to len(currentMsg.Data) + appconsts.MsgShareSize
	// but we cache it here for readability
	dataLen := 0
	saveMessage := func() {
		msgs = append(msgs, currentMsg)
		dataLen = 0
		isNewMessage = true
	}
	// iterate through all the shares and parse out each msg
	for i := 0; i < len(shares); i++ {
		dataLen = len(currentMsg.Data) + appconsts.MsgShareSize
		switch {
		case isNewMessage:
			nextMsgChunk, nextMsgLen, err := ParseDelimiter(shares[i][appconsts.NamespaceSize:])
			if err != nil {
				return nil, err
			}
			// the current share is namespaced padding so we ignore it
			if bytes.Equal(shares[i][appconsts.NamespaceSize:], appconsts.NameSpacedPaddedShareBytes) {
				continue
			}
			currentMsgLen = int(nextMsgLen)
			nid := shares[i][:appconsts.NamespaceSize]
			currentMsg = coretypes.Message{
				NamespaceID: nid,
				Data:        nextMsgChunk,
			}
			// the current share contains the entire msg so we save it and
			// progress
			if currentMsgLen <= len(nextMsgChunk) {
				currentMsg.Data = currentMsg.Data[:currentMsgLen]
				saveMessage()
				continue
			}
			isNewMessage = false
		// this entire share contains a chunk of message that we need to save
		case currentMsgLen > dataLen:
			currentMsg.Data = append(currentMsg.Data, shares[i][appconsts.NamespaceSize:]...)
		// this share contains the last chunk of data needed to complete the
		// message
		case currentMsgLen <= dataLen:
			remaining := currentMsgLen - len(currentMsg.Data) + appconsts.NamespaceSize
			currentMsg.Data = append(currentMsg.Data, shares[i][appconsts.NamespaceSize:remaining]...)
			saveMessage()
		}
	}
	return msgs, nil
}

// ParseDelimiter finds and returns the length delimiter of the message provided
// while also removing the delimiter bytes from the input
func ParseDelimiter(input []byte) ([]byte, uint64, error) {
	if len(input) == 0 {
		return input, 0, nil
	}

	l := binary.MaxVarintLen64
	if len(input) < binary.MaxVarintLen64 {
		l = len(input)
	}

	delimiter := zeroPadIfNecessary(input[:l], binary.MaxVarintLen64)

	// read the length of the message
	r := bytes.NewBuffer(delimiter)
	msgLen, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, 0, err
	}

	// calculate the number of bytes used by the delimiter
	lenBuf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(lenBuf, msgLen)

	// return the input without the length delimiter
	return input[n:], msgLen, nil
}

func zeroPadIfNecessary(share []byte, width int) []byte {
	oldLen := len(share)
	if oldLen >= width {
		return share
	}

	missingBytes := width - oldLen
	padByte := []byte{0}
	padding := bytes.Repeat(padByte, missingBytes)
	share = append(share, padding...)
	return share
}

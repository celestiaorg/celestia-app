package shares

import (
	"bytes"
	"encoding/binary"
	"fmt"

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
		dataLen = len(currentMsg.Data) + appconsts.SparseShareContentSize
		switch {
		case isNewMessage:
			nextMsgChunk, nextMsgLen, err := ParseDelimiter(shares[i][appconsts.NamespaceSize+appconsts.ShareInfoBytes:])
			if err != nil {
				return nil, err
			}
			// the current share is namespaced padding so we ignore it
			if bytes.Equal(shares[i][appconsts.NamespaceSize+appconsts.ShareInfoBytes:], appconsts.NameSpacedPaddedShareBytes) {
				continue
			}
			currentMsgLen = int(nextMsgLen)
			nid := shares[i][:appconsts.NamespaceSize]
			infoByte, err := ParseInfoReservedByte(shares[i][appconsts.NamespaceSize : appconsts.NamespaceSize+appconsts.ShareInfoBytes][0])
			if err != nil {
				panic(err)
			}
			if infoByte.IsMessageStart() != isNewMessage {
				return nil, fmt.Errorf("expected message start indicator to be %t but got %t", isNewMessage, infoByte.IsMessageStart())
			}
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
			currentMsg.Data = append(currentMsg.Data, shares[i][appconsts.NamespaceSize+appconsts.ShareInfoBytes:]...)
		// this share contains the last chunk of data needed to complete the
		// message
		case currentMsgLen <= dataLen:
			remaining := currentMsgLen - len(currentMsg.Data) + appconsts.NamespaceSize + appconsts.ShareInfoBytes
			currentMsg.Data = append(currentMsg.Data, shares[i][appconsts.NamespaceSize+appconsts.ShareInfoBytes:remaining]...)
			saveMessage()
		}
	}
	return msgs, nil
}

// ParseDelimiter finds and returns the length delimiter of the share provided
// while also removing the delimiter bytes from the input. ParseDelimiter
// applies to both compact and sparse shares. Input should not contain the
// namespace ID or info byte of a share.
func ParseDelimiter(input []byte) (inputWithoutLengthDelimiter []byte, dataLength uint64, err error) {
	if len(input) == 0 {
		return input, 0, nil
	}

	l := binary.MaxVarintLen64
	if len(input) < binary.MaxVarintLen64 {
		l = len(input)
	}

	delimiter := zeroPadIfNecessary(input[:l], binary.MaxVarintLen64)

	// read the length of the data
	r := bytes.NewBuffer(delimiter)
	dataLen, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, 0, err
	}

	// calculate the number of bytes used by the delimiter
	lenBuf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(lenBuf, dataLen)

	// return the input without the length delimiter
	return input[n:], dataLen, nil
}

// zeroPadIfNecessary pads the share with trailing zero bytes if the provided
// share has fewer bytes than width. Returns the share unmodified if the
// len(share) is greater than or equal to width.
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

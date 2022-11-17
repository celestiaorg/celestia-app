package shares

import (
	"bytes"
	"fmt"

	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
)

// parseSparseShares iterates through rawShares and parses out individual
// blobs. It returns an error if a rawShare contains a share version that
// isn't present in supportedShareVersions.
func parseSparseShares(rawShares [][]byte, supportedShareVersions []uint8) ([]coretypes.Blob, error) {
	if len(rawShares) == 0 {
		return nil, nil
	}
	shares := FromBytes(rawShares)
	for _, share := range shares {
		infoByte, err := share.InfoByte()
		if err != nil {
			return nil, err
		}
		if !bytes.Contains(supportedShareVersions, []byte{infoByte.Version()}) {
			return nil, fmt.Errorf("unsupported share version %v is not present in the list of supported share versions %v", infoByte.Version(), supportedShareVersions)
		}
	}

	// msgs returned
	msgs := []coretypes.Blob{}
	currentMsgLen := 0
	currentMsg := coretypes.Blob{}
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
	for i := 0; i < len(rawShares); i++ {
		dataLen = len(currentMsg.Data) + appconsts.SparseShareContentSize
		switch {
		case isNewMessage:
			nextMsgChunk, nextMsgLen, err := ParseDelimiter(rawShares[i][appconsts.NamespaceSize+appconsts.ShareInfoBytes:])
			if err != nil {
				return nil, err
			}
			// the current share is namespaced padding so we ignore it
			if bytes.Equal(rawShares[i][appconsts.NamespaceSize+appconsts.ShareInfoBytes:], appconsts.NameSpacedPaddedShareBytes) {
				continue
			}
			currentMsgLen = int(nextMsgLen)
			nid := rawShares[i][:appconsts.NamespaceSize]
			infoByte, err := ParseInfoByte(rawShares[i][appconsts.NamespaceSize : appconsts.NamespaceSize+appconsts.ShareInfoBytes][0])
			if err != nil {
				panic(err)
			}
			if infoByte.IsSequenceStart() != isNewMessage {
				return nil, fmt.Errorf("expected sequence start indicator to be %t but got %t", isNewMessage, infoByte.IsSequenceStart())
			}
			currentMsg = coretypes.Blob{
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
			currentMsg.Data = append(currentMsg.Data, rawShares[i][appconsts.NamespaceSize+appconsts.ShareInfoBytes:]...)
		// this share contains the last chunk of data needed to complete the
		// message
		case currentMsgLen <= dataLen:
			remaining := currentMsgLen - len(currentMsg.Data) + appconsts.NamespaceSize + appconsts.ShareInfoBytes
			currentMsg.Data = append(currentMsg.Data, rawShares[i][appconsts.NamespaceSize+appconsts.ShareInfoBytes:remaining]...)
			saveMessage()
		}
	}
	return msgs, nil
}

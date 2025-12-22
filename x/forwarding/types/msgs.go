package types

import "encoding/binary"

func (msg *MsgWarpForward) DerivationKeys() [][]byte {
	var keys [][]byte

	domainBz := make([]byte, 4)
	binary.BigEndian.PutUint32(domainBz, msg.DestinationDomain)
	keys = append(keys, domainBz)

	recipientBz := msg.Recipient.Bytes()
	keys = append(keys, recipientBz)

	return keys
}

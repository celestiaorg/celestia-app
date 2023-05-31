package ssz

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
)

func PrettyEncode(data interface{}, out io.Writer) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "    ")
	return enc.Encode(data)
}

func PrettyPrint(data interface{}) {
	var buffer bytes.Buffer
	_ = PrettyEncode(data, &buffer)
	fmt.Println(buffer.String())
}

func EncodeVarintProto(l int) []byte {
	// avoid multiple allocs for normal case
	res := make([]byte, 0, 8)
	for l >= 1<<7 {
		res = append(res, uint8(l&0x7f|0x80))
		l >>= 7
	}
	res = append(res, uint8(l))
	return res
}

func Hash(x []byte) []byte {
	h := sha256.New()
	h.Write(x)
	// fmt.Printf("%x", h.Sum(nil))
	return h.Sum(nil)
}

package proof

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

func Hash(x []byte) []byte {
	h := sha256.New()
	h.Write(x)
	return h.Sum(nil)
}

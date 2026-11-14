package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	pfmtypes "github.com/cosmos/ibc-apps/middleware/packet-forward-middleware/v8/packetforward/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v8/modules/core/04-channel/types"
)

// validateDeterministicMsgs returns an error if tx carries a message in a form
// this node does not admit (see checkDeterministicMsg), either at the top level or one level down in a MsgExec or MsgSubmitProposal.
// Deeper nesting is not inspected because the ante handler rejects it.
//
// This is a local mempool and proposal policy, not a validity rule:
// ProcessProposal and FinalizeBlock still accept such a tx, so blocks built by
// nodes without this check stay valid.
func validateDeterministicMsgs(tx sdk.Tx) error {
	for _, msg := range tx.GetMsgs() {
		if err := checkDeterministicMsg(msg); err != nil {
			return err
		}
		var (
			nested []sdk.Msg
			err    error
		)
		switch m := msg.(type) {
		case *authz.MsgExec:
			nested, err = m.GetMessages()
		case *govv1.MsgSubmitProposal:
			nested, err = m.GetMsgs()
		}
		if err != nil {
			// Undecodable inner messages are rejected by the ante handler.
			continue
		}
		for _, nestedMsg := range nested {
			if err := checkDeterministicMsg(nestedMsg); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkDeterministicMsg(msg sdk.Msg) error {
	switch m := msg.(type) {
	case *channeltypes.MsgRecvPacket:
		var data transfertypes.FungibleTokenPacketData
		if err := transfertypes.ModuleCdc.UnmarshalJSON(m.Packet.GetData(), &data); err != nil {
			// Packet forward middleware passes such packets on unchanged.
			return nil
		}
		if err := validateForwardMemo(data.Memo); err != nil {
			return sdkerrors.ErrInvalidRequest.Wrapf("unsupported packet-forward memo: %s", err)
		}
	case *govv1.MsgUpdateParams:
		if p := m.Params.MaxDepositPeriod; p != nil && *p <= 0 {
			return sdkerrors.ErrInvalidRequest.Wrap("gov max_deposit_period must be positive")
		}
	}
	return nil
}

var (
	forwardFields = []string{"receiver", "port", "channel", "timeout", "retries", "next"}
	// surrogateEscape matches a \u escape of a UTF-16 surrogate.
	surrogateEscape = regexp.MustCompile(`\\u[dD][89a-fA-F]`)
)

// validateForwardMemo returns an error if the packet-forward middleware would
// treat memo as a forward request and memo is not in the supported form: it
// must decode, validate and re-encode without error, and spell each field once
// in plain form.
func validateForwardMemo(memo string) error {
	bz := []byte(memo)
	if !json.Valid(bz) {
		// The middleware passes memos that are not JSON on unchanged.
		return nil
	}
	top, isObject, err := jsonObjectMembers(bz, []string{"forward"})
	if !isObject {
		return nil
	}
	if err != nil {
		return err
	}
	fwd, ok := top["forward"]
	if !ok || bytes.Equal(fwd, []byte("null")) {
		return nil
	}

	if len(memo) > transfertypes.MaximumMemoLength {
		return fmt.Errorf("memo is longer than %d bytes", transfertypes.MaximumMemoLength)
	}
	if !utf8.Valid(bz) || surrogateEscape.Match(bz) {
		return fmt.Errorf("memo must be valid UTF-8 without surrogate escapes")
	}
	fields, isObject, err := jsonObjectMembers(fwd, forwardFields)
	if !isObject {
		return fmt.Errorf("forward must be a JSON object")
	}
	if err != nil {
		return err
	}
	for _, name := range []string{"receiver", "port", "channel"} {
		if v, ok := fields[name]; ok && !isPlainJSONString(v) {
			return fmt.Errorf("forward.%s must be a string without escapes", name)
		}
	}
	timeout, hasTimeout := fields["timeout"]
	numericTimeout := hasTimeout && len(timeout) > 0 && (timeout[0] == '-' || (timeout[0] >= '0' && timeout[0] <= '9'))
	if hasTimeout && !numericTimeout && !isPlainJSONString(timeout) {
		return fmt.Errorf("forward.timeout must be a string or a number")
	}
	if v, ok := fields["retries"]; ok && !isDigits(v) {
		return fmt.Errorf("forward.retries must be a non-negative integer")
	}
	if v, ok := fields["next"]; ok && bytes.Equal(v, []byte("null")) {
		return fmt.Errorf("forward.next must not be null")
	}

	// Decode, validate and re-encode exactly as the middleware does.
	var metadata pfmtypes.PacketMetadata
	if err := json.Unmarshal(bz, &metadata); err != nil {
		return fmt.Errorf("invalid forward metadata")
	}
	if metadata.Forward == nil {
		return fmt.Errorf("missing forward metadata")
	}
	if err := metadata.Forward.Validate(); err != nil {
		return fmt.Errorf("invalid forward metadata")
	}
	if metadata.Forward.Next != nil {
		if _, err := json.Marshal(metadata.Forward.Next); err != nil {
			return fmt.Errorf("invalid forward.next")
		}
	}

	// Accept a numeric timeout only as an integer in the int64 range.
	if numericTimeout {
		var v float64
		if err := json.Unmarshal(timeout, &v); err != nil {
			return fmt.Errorf("invalid forward.timeout")
		}
		if v < -0x1p63 || v >= 0x1p63 || v != math.Trunc(v) {
			return fmt.Errorf("forward.timeout must be an integer in the int64 range")
		}
	}
	return nil
}

// jsonObjectMembers returns the raw values of the members of the JSON object
// bz whose keys are in names. isObject is false if bz is not a JSON object. It
// returns an error for a duplicate key, or for a key that differs from a name
// only in case, '_' or '-', since JSON decoders may match those to the field.
func jsonObjectMembers(bz []byte, names []string) (members map[string]json.RawMessage, isObject bool, err error) {
	dec := json.NewDecoder(bytes.NewReader(bz))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, false, nil
	}
	members = make(map[string]json.RawMessage, len(names))
	var skip json.RawMessage
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, true, err
		}
		key, _ := tok.(string)
		name, matched := matchFieldName(key, names)
		if !matched {
			if err := dec.Decode(&skip); err != nil {
				return nil, true, err
			}
			continue
		}
		if key != name {
			return nil, true, fmt.Errorf("key %q must be spelled %q", key, name)
		}
		if _, dup := members[name]; dup {
			return nil, true, fmt.Errorf("duplicate key %q", name)
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, true, err
		}
		members[name] = v
	}
	return members, true, nil
}

func matchFieldName(key string, names []string) (string, bool) {
	folded := strings.NewReplacer("_", "", "-", "").Replace(key)
	for _, name := range names {
		if strings.EqualFold(folded, name) {
			return name, true
		}
	}
	return "", false
}

func isPlainJSONString(v json.RawMessage) bool {
	return len(v) >= 2 && v[0] == '"' && !bytes.ContainsRune(v, '\\')
}

func isDigits(v json.RawMessage) bool {
	if len(v) == 0 {
		return false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

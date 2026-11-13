package encoding

import (
	"errors"
	"fmt"
	"math/bits"
	"strings"
	"sync"
	"unicode/utf8"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/encoding/protowire"
	protov2 "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// maxCanonicalDepth bounds how deep checkMessage nests. Real txs nest far
// less; the SDK's own limits are much higher.
const maxCanonicalDepth = 100

const anyFullName = "google.protobuf.Any"

// canonicalTxDecoder wraps the SDK tx decoder so that a tx decodes only if
// every message in it (TxRaw, TxBody, AuthInfo and the message in every Any,
// at any depth) uses an encoding that all protobuf decoders read the same way.
// It rejects unknown fields (non-critical ones included), fields that are out
// of ascending field-number order or repeated when singular, two members of
// one oneof, non-minimal varints, tags and length prefixes, the wrong wire
// type (including packed versus unpacked repeated scalars), integers out of
// range for their type, strings that are not valid UTF-8, and math.Int or
// LegacyDec values that are left out (when not nullable), empty or not in
// canonical decimal form (no sign but a leading minus, no leading zeros, no
// "0x" or "_"). It does not require the bytes to equal what
// gogoproto would write: proto3 default values may be written out and empty
// messages may be left out.
//
// The decoder runs in CheckTx, PrepareProposal, ProcessProposal and
// FinalizeBlock. Only this binary (app version 11 and later) runs it; the
// multiplexer replays earlier versions with their own binaries.
func canonicalTxDecoder(decoder sdk.TxDecoder) sdk.TxDecoder {
	return func(txBytes []byte) (sdk.Tx, error) {
		sdkTx, err := decoder(txBytes)
		if err != nil {
			return nil, err
		}
		if err := checkCanonicalTx(txBytes); err != nil {
			return nil, errorsmod.Wrap(sdkerrors.ErrTxDecode, err.Error())
		}
		return sdkTx, nil
	}
}

// checkCanonicalTx checks the encoding of TxRaw bytes and of the TxBody and
// AuthInfo they hold. It returns an error, never panics, for any input.
func checkCanonicalTx(txBytes []byte) error {
	if err := checkMessage(txBytes, txRawInfo(), 0); err != nil {
		return err
	}
	// checkMessage accepted the bytes, so each field below is well formed and
	// appears at most once.
	if err := checkMessage(fieldValue(txBytes, 1), txBodyInfo(), 1); err != nil {
		return err
	}
	return checkMessage(fieldValue(txBytes, 2), authInfoInfo(), 1)
}

// fieldInfo is what checkMessage needs to know about one field.
type fieldInfo struct {
	kind protoreflect.Kind
	// repeats is true for fields that may occur more than once in a row:
	// unpacked repeated fields and maps.
	repeats bool
	packed  bool
	// oneof is the index of the field's oneof, or -1.
	oneof int
	// msg is the type of a message field, resolved on first use.
	msgName protoreflect.FullName
	// decimal marks a math.Int or LegacyDec custom type field.
	decimal bool
	// required is the bit of a non-nullable singular decimal field in
	// messageInfo.required, or 0.
	required uint64
}

// messageInfo describes a message type. Instances are cached and immutable.
type messageInfo struct {
	name   protoreflect.FullName
	fields map[protowire.Number]*fieldInfo
	// required has a bit for each field that must be present.
	required uint64
}

var messageInfos sync.Map // protoreflect.FullName -> *messageInfo

var (
	txRawInfo    = sync.OnceValue(func() *messageInfo { return mustInfo("cosmos.tx.v1beta1.TxRaw") })
	txBodyInfo   = sync.OnceValue(func() *messageInfo { return mustInfo("cosmos.tx.v1beta1.TxBody") })
	authInfoInfo = sync.OnceValue(func() *messageInfo { return mustInfo("cosmos.tx.v1beta1.AuthInfo") })
)

func mustInfo(name protoreflect.FullName) *messageInfo {
	info, err := infoFor(name)
	if err != nil {
		panic(err)
	}
	return info
}

// infoFor returns the message info for name, from the gogoproto registry
// whose types the SDK decoder fills.
func infoFor(name protoreflect.FullName) (*messageInfo, error) {
	if v, ok := messageInfos.Load(name); ok {
		return v.(*messageInfo), nil
	}
	desc, err := gogoproto.GogoResolver.FindDescriptorByName(name)
	if err != nil {
		return nil, fmt.Errorf("unknown message type %s", name)
	}
	md, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		return nil, fmt.Errorf("%s is not a message type", name)
	}
	info := &messageInfo{name: name, fields: make(map[protowire.Number]*fieldInfo, md.Fields().Len())}
	for i := range md.Fields().Len() {
		fd := md.Fields().Get(i)
		f := &fieldInfo{
			kind:    fd.Kind(),
			repeats: fd.IsMap() || (fd.IsList() && !fd.IsPacked()),
			packed:  fd.IsList() && fd.IsPacked(),
			oneof:   -1,
		}
		if od := fd.ContainingOneof(); od != nil && !od.IsSynthetic() {
			f.oneof = od.Index()
		}
		if f.kind == protoreflect.MessageKind {
			f.msgName = fd.Message().FullName()
		}
		customType, nullable := gogoOptions(fd)
		f.decimal = strings.HasSuffix(customType, "math.Int") || strings.HasSuffix(customType, "math.LegacyDec")
		if f.decimal && !nullable && !fd.IsList() {
			n := bits.OnesCount64(info.required)
			if n == 64 {
				return nil, fmt.Errorf("%s has too many required fields", name)
			}
			f.required = 1 << n
			info.required |= f.required
		}
		info.fields[fd.Number()] = f
	}
	v, _ := messageInfos.LoadOrStore(name, info)
	return v.(*messageInfo), nil
}

var errNotCanonical = errors.New("tx is not canonically encoded")

func notCanonical(info *messageInfo, format string, args ...any) error {
	return fmt.Errorf("%w: %s: %s", errNotCanonical, info.name, fmt.Sprintf(format, args...))
}

// checkMessage checks one encoded message of type info.
func checkMessage(b []byte, info *messageInfo, depth int) error {
	if depth > maxCanonicalDepth {
		return notCanonical(info, "nested deeper than %d", maxCanonicalDepth)
	}
	var (
		prev       protowire.Number
		oneofsSeen uint64
		present    uint64
		typeURL    []byte
		value      []byte
	)
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return notCanonical(info, "invalid tag")
		}
		if n != protowire.SizeTag(num) {
			return notCanonical(info, "field %d tag is not minimal", num)
		}
		b = b[n:]
		f := info.fields[num]
		if f == nil {
			return notCanonical(info, "unknown field %d", num)
		}
		if num < prev || (num == prev && !f.repeats) {
			return notCanonical(info, "field %d is out of order or repeated", num)
		}
		if f.oneof >= 0 && num != prev {
			if f.oneof >= 64 || oneofsSeen&(1<<f.oneof) != 0 {
				return notCanonical(info, "field %d sets a oneof twice", num)
			}
			oneofsSeen |= 1 << f.oneof
		}
		prev = num
		present |= f.required

		if f.packed {
			if typ != protowire.BytesType {
				return notCanonical(info, "field %d is not packed", num)
			}
			v, m, err := consumeBytes(b)
			if err != nil {
				return notCanonical(info, "field %d: %v", num, err)
			}
			if err := checkPacked(v, f.kind); err != nil {
				return notCanonical(info, "field %d: %v", num, err)
			}
			b = b[m:]
			continue
		}
		if typ != wireType(f.kind) {
			return notCanonical(info, "field %d has wire type %d", num, typ)
		}
		var m int
		switch typ {
		case protowire.VarintType:
			v, k := protowire.ConsumeVarint(b)
			if k < 0 || k != protowire.SizeVarint(v) {
				return notCanonical(info, "field %d varint is not minimal", num)
			}
			if !varintInRange(v, f.kind) {
				return notCanonical(info, "field %d value is out of range", num)
			}
			m = k
		case protowire.Fixed32Type:
			if _, m = protowire.ConsumeFixed32(b); m < 0 {
				return notCanonical(info, "field %d is truncated", num)
			}
		case protowire.Fixed64Type:
			if _, m = protowire.ConsumeFixed64(b); m < 0 {
				return notCanonical(info, "field %d is truncated", num)
			}
		case protowire.BytesType:
			v, k, err := consumeBytes(b)
			if err != nil {
				return notCanonical(info, "field %d: %v", num, err)
			}
			m = k
			if f.kind == protoreflect.StringKind && !utf8.Valid(v) {
				return notCanonical(info, "field %d is not valid UTF-8", num)
			}
			if f.decimal && !canonicalDecimal(v) {
				return notCanonical(info, "field %d is not a canonical decimal", num)
			}
			if f.kind == protoreflect.MessageKind {
				sub, err := infoFor(f.msgName)
				if err != nil {
					return notCanonical(info, "field %d: %v", num, err)
				}
				if err := checkMessage(v, sub, depth+1); err != nil {
					return err
				}
			}
			if info.name == anyFullName {
				switch num {
				case 1:
					typeURL = v
				case 2:
					value = v
				}
			}
		}
		b = b[m:]
	}
	if present != info.required {
		return notCanonical(info, "a non-nullable decimal field is missing")
	}
	if info.name == anyFullName {
		return checkAnyValue(typeURL, value, depth)
	}
	return nil
}

// checkAnyValue checks the message packed in an Any. An Any without a type
// URL must be empty.
func checkAnyValue(typeURL, value []byte, depth int) error {
	if len(typeURL) == 0 {
		if len(value) > 0 {
			return fmt.Errorf("%w: Any has a value but no type URL", errNotCanonical)
		}
		return nil
	}
	url := string(typeURL)
	info, err := infoFor(protoreflect.FullName(url[strings.LastIndexByte(url, '/')+1:]))
	if err != nil {
		return fmt.Errorf("%w: Any %s: %v", errNotCanonical, url, err)
	}
	return checkMessage(value, info, depth+1)
}

// consumeBytes reads a length-prefixed value with a minimal length prefix.
func consumeBytes(b []byte) (v []byte, n int, err error) {
	v, n = protowire.ConsumeBytes(b)
	if n < 0 {
		return nil, 0, errors.New("invalid length")
	}
	if n-len(v) != protowire.SizeVarint(uint64(len(v))) {
		return nil, 0, errors.New("length prefix is not minimal")
	}
	return v, n, nil
}

// checkPacked checks the elements of a packed repeated scalar field.
func checkPacked(b []byte, kind protoreflect.Kind) error {
	switch wireType(kind) {
	case protowire.Fixed32Type:
		if len(b)%4 != 0 {
			return errors.New("packed fixed32 length")
		}
	case protowire.Fixed64Type:
		if len(b)%8 != 0 {
			return errors.New("packed fixed64 length")
		}
	case protowire.VarintType:
		for len(b) > 0 {
			v, n := protowire.ConsumeVarint(b)
			if n < 0 || n != protowire.SizeVarint(v) {
				return errors.New("packed varint is not minimal")
			}
			if !varintInRange(v, kind) {
				return errors.New("packed value is out of range")
			}
			b = b[n:]
		}
	default:
		return errors.New("field cannot be packed")
	}
	return nil
}

// wireType returns the wire type of a non-packed field of kind. Groups get
// an invalid wire type, so they never match.
func wireType(kind protoreflect.Kind) protowire.Type {
	switch kind {
	case protoreflect.BoolKind, protoreflect.EnumKind,
		protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Uint32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Uint64Kind:
		return protowire.VarintType
	case protoreflect.Fixed32Kind, protoreflect.Sfixed32Kind, protoreflect.FloatKind:
		return protowire.Fixed32Type
	case protoreflect.Fixed64Kind, protoreflect.Sfixed64Kind, protoreflect.DoubleKind:
		return protowire.Fixed64Type
	case protoreflect.StringKind, protoreflect.BytesKind, protoreflect.MessageKind:
		return protowire.BytesType
	default:
		return -1
	}
}

// varintInRange reports whether v is a value of kind that decoders cannot
// truncate differently.
func varintInRange(v uint64, kind protoreflect.Kind) bool {
	switch kind {
	case protoreflect.BoolKind:
		return v <= 1
	case protoreflect.Int32Kind, protoreflect.EnumKind:
		return int64(v) == int64(int32(v))
	case protoreflect.Uint32Kind, protoreflect.Sint32Kind:
		return v <= 0xFFFF_FFFF
	default:
		return true
	}
}

// fieldValue returns the value of the length-delimited field num in b, which
// checkMessage accepted.
func fieldValue(b []byte, num protowire.Number) []byte {
	for len(b) > 0 {
		n, typ, k := protowire.ConsumeTag(b)
		if k < 0 {
			return nil
		}
		b = b[k:]
		m := protowire.ConsumeFieldValue(n, typ, b)
		if m < 0 {
			return nil
		}
		if n == num && typ == protowire.BytesType {
			v, _ := protowire.ConsumeBytes(b)
			return v
		}
		b = b[m:]
	}
	return nil
}

// canonicalDecimal reports whether v is an integer in the form math.Int and
// LegacyDec write: an optional minus, then digits without leading zeros, and
// not "-0".
func canonicalDecimal(v []byte) bool {
	if len(v) > 0 && v[0] == '-' {
		v = v[1:]
		if len(v) == 1 && v[0] == '0' {
			return false
		}
	}
	if len(v) == 0 || (v[0] == '0' && len(v) > 1) {
		return false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// gogoOptions returns a field's gogoproto customtype (extension 65003) and
// nullable (extension 65001, true unless set to false) options.
func gogoOptions(fd protoreflect.FieldDescriptor) (customType string, nullable bool) {
	nullable = true
	b, err := protov2.Marshal(fd.Options())
	if err != nil {
		return "", true
	}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			break
		}
		b = b[n:]
		switch {
		case num == 65003 && typ == protowire.BytesType:
			v, _ := protowire.ConsumeBytes(b)
			customType = string(v)
		case num == 65001 && typ == protowire.VarintType:
			v, _ := protowire.ConsumeVarint(b)
			nullable = v != 0
		}
		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			break
		}
		b = b[m:]
	}
	return customType, nullable
}

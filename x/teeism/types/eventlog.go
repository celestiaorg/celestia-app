package types

import (
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	// DstackRuntimeEventType is dstack's own runtime event type, outside the
	// TCG-defined range.
	DstackRuntimeEventType = 0x0800_0001
	// ApplicationIMR is the measurement register dstack records application
	// identity in.
	ApplicationIMR = 3
	// RtmrCount is the number of runtime measurement registers a TDX report has.
	RtmrCount = 4
	// DigestLen is the length of a SHA-384 digest.
	DigestLen = 48
)

// dstack event-log keys that identify the software stack.
const (
	EventComposeHash = "compose-hash"
	EventOsImageHash = "os-image-hash"
	EventMrKms       = "mr-kms"
	EventKeyProvider = "key-provider"
)

// EventLog is one entry of the dstack event log.
type EventLog struct {
	IMR          uint32 `json:"imr"`
	EventType    uint32 `json:"event_type"`
	Digest       Digest `json:"digest"`
	Event        string `json:"event"`
	EventPayload HexBz  `json:"event_payload"`
}

// Digest is a SHA-384 digest that decodes from a hex string, treating the empty
// string as all zeros because that is how dstack writes an absent digest.
type Digest [DigestLen]byte

func (d *Digest) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		*d = Digest{}
		return nil
	}
	raw, err := hex.DecodeString(s)
	if err != nil {
		return err
	}
	if len(raw) != DigestLen {
		return fmt.Errorf("digest must be %d bytes, got %d", DigestLen, len(raw))
	}
	copy(d[:], raw)
	return nil
}

func (d Digest) MarshalJSON() ([]byte, error) { return json.Marshal(hex.EncodeToString(d[:])) }

// IsZero reports whether no digest was recorded.
func (d Digest) IsZero() bool { return d == Digest{} }

// HexBz is a byte slice carried as a hex string in JSON.
type HexBz []byte

func (h *HexBz) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		return err
	}
	*h = raw
	return nil
}

func (h HexBz) MarshalJSON() ([]byte, error) { return json.Marshal(hex.EncodeToString(h)) }

// ParseEventLog decodes the dstack event log as dstack serialized it.
func ParseEventLog(raw []byte) ([]EventLog, error) {
	var log []EventLog
	if err := json.Unmarshal(raw, &log); err != nil {
		return nil, ErrMalformedEventLog.Wrap(err.Error())
	}
	return log, nil
}

func sha384(data []byte) Digest {
	return Digest(sha512.Sum384(data))
}

// EventPreimageV1 is dstack's v1 preimage: event_type_le || ":" || name || ":" || payload.
func EventPreimageV1(eventType uint32, event string, payload []byte) []byte {
	buf := make([]byte, 0, 6+len(event)+len(payload))
	buf = binary.LittleEndian.AppendUint32(buf, eventType)
	buf = append(buf, ':')
	buf = append(buf, event...)
	buf = append(buf, ':')
	return append(buf, payload...)
}

// EventPreimageV2 is dstack's v2 preimage: RFC 8785 canonical JSON of
// {name, payload, type}.
func EventPreimageV2(eventType uint32, event string, payload []byte) []byte {
	var b strings.Builder
	b.WriteString(`{"name":`)
	writeJSONString(&b, event)
	b.WriteString(`,"payload":`)
	writeJSONString(&b, hex.EncodeToString(payload))
	b.WriteString(`,"type":`)
	b.WriteString(strconv.FormatUint(uint64(eventType), 10))
	b.WriteByte('}')
	return []byte(b.String())
}

// EventDigestMatches reports whether an entry's digest really commits to its own
// (event, payload) under either dstack event-log version.
//
// Accepting both keeps this correct across dstack image upgrades. Each is a
// collision-resistant commitment to the same pair, so allowing either loses
// nothing.
func EventDigestMatches(e *EventLog) bool {
	return e.Digest == sha384(EventPreimageV1(e.EventType, e.Event, e.EventPayload)) ||
		e.Digest == sha384(EventPreimageV2(e.EventType, e.Event, e.EventPayload))
}

// ReplayEventLogs folds the log into the four runtime measurement registers.
//
// Entries with an explicit digest contribute it directly. IMR 3 entries that omit
// one (dstack writes those with an empty digest field) have it recomputed.
// Entries in IMR 0-2 without a digest contribute nothing, matching dstack's own
// replay.
func ReplayEventLogs(eventlog []EventLog) [RtmrCount]Digest {
	var rtmrs [RtmrCount]Digest
	for idx := range rtmrs {
		for i := range eventlog {
			e := &eventlog[i]
			if int(e.IMR) != idx {
				continue
			}
			var digest Digest
			switch {
			case !e.Digest.IsZero():
				digest = e.Digest
			case e.IMR == ApplicationIMR:
				digest = sha384(EventPreimageV1(e.EventType, e.Event, e.EventPayload))
			default:
				continue
			}
			rtmrs[idx] = sha384(append(rtmrs[idx][:], digest[:]...))
		}
	}
	return rtmrs
}

// GetEventValue reads a pinned value out of the log, refusing any entry whose
// digest does not commit to the text being read.
//
// Without that refusal an attacker keeps every genuine digest, so the RTMR replay
// still matches, and merely relabels the surrounding text to impersonate a pinned
// key. A duplicated key never resolves, for the same reason.
func GetEventValue(eventlog []EventLog, name string) ([]byte, bool) {
	var found *EventLog
	for i := range eventlog {
		if eventlog[i].Event != name {
			continue
		}
		if found != nil {
			return nil, false
		}
		found = &eventlog[i]
	}
	if found == nil {
		return nil, false
	}

	var selfConsistent bool
	if found.Digest.IsZero() {
		// No digest was recorded, so the replay used our recomputation, which
		// already binds (event, payload) into the RTMR.
		selfConsistent = found.IMR == ApplicationIMR
	} else {
		selfConsistent = EventDigestMatches(found)
	}
	if !selfConsistent {
		return nil, false
	}
	return found.EventPayload, true
}

func writeJSONString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, c := range s {
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				fmt.Fprintf(b, `\u%04x`, c)
			} else {
				b.WriteRune(c)
			}
		}
	}
	b.WriteByte('"')
}

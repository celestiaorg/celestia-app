package types

// NoField discards protobuf field payloads. A []NoField can count entries
// without allocating per entry.
type NoField struct{}

func (NoField) Marshal() ([]byte, error)                 { return nil, nil }
func (NoField) MarshalTo([]byte) (int, error)            { return 0, nil }
func (NoField) MarshalToSizedBuffer([]byte) (int, error) { return 0, nil }
func (NoField) Size() int                                { return 0 }

func (*NoField) Unmarshal([]byte) error { return nil }

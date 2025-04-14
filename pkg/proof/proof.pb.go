// Code generated by protoc-gen-gogo. DO NOT EDIT.
// source: celestia/core/v1/proof/proof.proto

package proof

import (
	fmt "fmt"
	proto "github.com/cosmos/gogoproto/proto"
	io "io"
	math "math"
	math_bits "math/bits"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.GoGoProtoPackageIsVersion3 // please upgrade the proto package

// ShareProof is an NMT proof that a set of shares exist in a set of rows and a
// Merkle proof that those rows exist in a Merkle tree with a given data root.
type ShareProof struct {
	Data             [][]byte    `protobuf:"bytes,1,rep,name=data,proto3" json:"data,omitempty"`
	ShareProofs      []*NMTProof `protobuf:"bytes,2,rep,name=share_proofs,json=shareProofs,proto3" json:"share_proofs,omitempty"`
	NamespaceId      []byte      `protobuf:"bytes,3,opt,name=namespace_id,json=namespaceId,proto3" json:"namespace_id,omitempty"`
	RowProof         *RowProof   `protobuf:"bytes,4,opt,name=row_proof,json=rowProof,proto3" json:"row_proof,omitempty"`
	NamespaceVersion uint32      `protobuf:"varint,5,opt,name=namespace_version,json=namespaceVersion,proto3" json:"namespace_version,omitempty"`
}

func (m *ShareProof) Reset()         { *m = ShareProof{} }
func (m *ShareProof) String() string { return proto.CompactTextString(m) }
func (*ShareProof) ProtoMessage()    {}
func (*ShareProof) Descriptor() ([]byte, []int) {
	return fileDescriptor_e53d87d8fb5ec353, []int{0}
}
func (m *ShareProof) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *ShareProof) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_ShareProof.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalToSizedBuffer(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (m *ShareProof) XXX_Merge(src proto.Message) {
	xxx_messageInfo_ShareProof.Merge(m, src)
}
func (m *ShareProof) XXX_Size() int {
	return m.Size()
}
func (m *ShareProof) XXX_DiscardUnknown() {
	xxx_messageInfo_ShareProof.DiscardUnknown(m)
}

var xxx_messageInfo_ShareProof proto.InternalMessageInfo

func (m *ShareProof) GetData() [][]byte {
	if m != nil {
		return m.Data
	}
	return nil
}

func (m *ShareProof) GetShareProofs() []*NMTProof {
	if m != nil {
		return m.ShareProofs
	}
	return nil
}

func (m *ShareProof) GetNamespaceId() []byte {
	if m != nil {
		return m.NamespaceId
	}
	return nil
}

func (m *ShareProof) GetRowProof() *RowProof {
	if m != nil {
		return m.RowProof
	}
	return nil
}

func (m *ShareProof) GetNamespaceVersion() uint32 {
	if m != nil {
		return m.NamespaceVersion
	}
	return 0
}

// RowProof is a Merkle proof that a set of rows exist in a Merkle tree with a
// given data root.
type RowProof struct {
	RowRoots [][]byte `protobuf:"bytes,1,rep,name=row_roots,json=rowRoots,proto3" json:"row_roots,omitempty"`
	Proofs   []*Proof `protobuf:"bytes,2,rep,name=proofs,proto3" json:"proofs,omitempty"`
	Root     []byte   `protobuf:"bytes,3,opt,name=root,proto3" json:"root,omitempty"`
	StartRow uint32   `protobuf:"varint,4,opt,name=start_row,json=startRow,proto3" json:"start_row,omitempty"`
	EndRow   uint32   `protobuf:"varint,5,opt,name=end_row,json=endRow,proto3" json:"end_row,omitempty"`
}

func (m *RowProof) Reset()         { *m = RowProof{} }
func (m *RowProof) String() string { return proto.CompactTextString(m) }
func (*RowProof) ProtoMessage()    {}
func (*RowProof) Descriptor() ([]byte, []int) {
	return fileDescriptor_e53d87d8fb5ec353, []int{1}
}
func (m *RowProof) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *RowProof) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_RowProof.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalToSizedBuffer(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (m *RowProof) XXX_Merge(src proto.Message) {
	xxx_messageInfo_RowProof.Merge(m, src)
}
func (m *RowProof) XXX_Size() int {
	return m.Size()
}
func (m *RowProof) XXX_DiscardUnknown() {
	xxx_messageInfo_RowProof.DiscardUnknown(m)
}

var xxx_messageInfo_RowProof proto.InternalMessageInfo

func (m *RowProof) GetRowRoots() [][]byte {
	if m != nil {
		return m.RowRoots
	}
	return nil
}

func (m *RowProof) GetProofs() []*Proof {
	if m != nil {
		return m.Proofs
	}
	return nil
}

func (m *RowProof) GetRoot() []byte {
	if m != nil {
		return m.Root
	}
	return nil
}

func (m *RowProof) GetStartRow() uint32 {
	if m != nil {
		return m.StartRow
	}
	return 0
}

func (m *RowProof) GetEndRow() uint32 {
	if m != nil {
		return m.EndRow
	}
	return 0
}

// NMTProof is a proof of a namespace.ID in an NMT.
// In case this proof proves the absence of a namespace.ID
// in a tree it also contains the leaf hashes of the range
// where that namespace would be.
type NMTProof struct {
	// Start index of this proof.
	Start int32 `protobuf:"varint,1,opt,name=start,proto3" json:"start,omitempty"`
	// End index of this proof.
	End int32 `protobuf:"varint,2,opt,name=end,proto3" json:"end,omitempty"`
	// Nodes that together with the corresponding leaf values can be used to
	// recompute the root and verify this proof. Nodes should consist of the max
	// and min namespaces along with the actual hash, resulting in each being 48
	// bytes each
	Nodes [][]byte `protobuf:"bytes,3,rep,name=nodes,proto3" json:"nodes,omitempty"`
	// leafHash are nil if the namespace is present in the NMT. In case the
	// namespace to be proved is in the min/max range of the tree but absent, this
	// will contain the leaf hash necessary to verify the proof of absence. Leaf
	// hashes should consist of the namespace along with the actual hash,
	// resulting 40 bytes total.
	LeafHash []byte `protobuf:"bytes,4,opt,name=leaf_hash,json=leafHash,proto3" json:"leaf_hash,omitempty"`
}

func (m *NMTProof) Reset()         { *m = NMTProof{} }
func (m *NMTProof) String() string { return proto.CompactTextString(m) }
func (*NMTProof) ProtoMessage()    {}
func (*NMTProof) Descriptor() ([]byte, []int) {
	return fileDescriptor_e53d87d8fb5ec353, []int{2}
}
func (m *NMTProof) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *NMTProof) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_NMTProof.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalToSizedBuffer(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (m *NMTProof) XXX_Merge(src proto.Message) {
	xxx_messageInfo_NMTProof.Merge(m, src)
}
func (m *NMTProof) XXX_Size() int {
	return m.Size()
}
func (m *NMTProof) XXX_DiscardUnknown() {
	xxx_messageInfo_NMTProof.DiscardUnknown(m)
}

var xxx_messageInfo_NMTProof proto.InternalMessageInfo

func (m *NMTProof) GetStart() int32 {
	if m != nil {
		return m.Start
	}
	return 0
}

func (m *NMTProof) GetEnd() int32 {
	if m != nil {
		return m.End
	}
	return 0
}

func (m *NMTProof) GetNodes() [][]byte {
	if m != nil {
		return m.Nodes
	}
	return nil
}

func (m *NMTProof) GetLeafHash() []byte {
	if m != nil {
		return m.LeafHash
	}
	return nil
}

// Proof is taken from the merkle package
type Proof struct {
	Total    int64    `protobuf:"varint,1,opt,name=total,proto3" json:"total,omitempty"`
	Index    int64    `protobuf:"varint,2,opt,name=index,proto3" json:"index,omitempty"`
	LeafHash []byte   `protobuf:"bytes,3,opt,name=leaf_hash,json=leafHash,proto3" json:"leaf_hash,omitempty"`
	Aunts    [][]byte `protobuf:"bytes,4,rep,name=aunts,proto3" json:"aunts,omitempty"`
}

func (m *Proof) Reset()         { *m = Proof{} }
func (m *Proof) String() string { return proto.CompactTextString(m) }
func (*Proof) ProtoMessage()    {}
func (*Proof) Descriptor() ([]byte, []int) {
	return fileDescriptor_e53d87d8fb5ec353, []int{3}
}
func (m *Proof) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *Proof) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_Proof.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalToSizedBuffer(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (m *Proof) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Proof.Merge(m, src)
}
func (m *Proof) XXX_Size() int {
	return m.Size()
}
func (m *Proof) XXX_DiscardUnknown() {
	xxx_messageInfo_Proof.DiscardUnknown(m)
}

var xxx_messageInfo_Proof proto.InternalMessageInfo

func (m *Proof) GetTotal() int64 {
	if m != nil {
		return m.Total
	}
	return 0
}

func (m *Proof) GetIndex() int64 {
	if m != nil {
		return m.Index
	}
	return 0
}

func (m *Proof) GetLeafHash() []byte {
	if m != nil {
		return m.LeafHash
	}
	return nil
}

func (m *Proof) GetAunts() [][]byte {
	if m != nil {
		return m.Aunts
	}
	return nil
}

func init() {
	proto.RegisterType((*ShareProof)(nil), "celestia.core.v1.proof.ShareProof")
	proto.RegisterType((*RowProof)(nil), "celestia.core.v1.proof.RowProof")
	proto.RegisterType((*NMTProof)(nil), "celestia.core.v1.proof.NMTProof")
	proto.RegisterType((*Proof)(nil), "celestia.core.v1.proof.Proof")
}

func init() {
	proto.RegisterFile("celestia/core/v1/proof/proof.proto", fileDescriptor_e53d87d8fb5ec353)
}

var fileDescriptor_e53d87d8fb5ec353 = []byte{
	// 446 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x7c, 0x52, 0xc1, 0x8e, 0xd3, 0x30,
	0x10, 0xad, 0x37, 0x6d, 0x09, 0x6e, 0x56, 0x5a, 0x2c, 0x04, 0x96, 0x10, 0x51, 0xc8, 0x29, 0x12,
	0xda, 0x44, 0x0b, 0xe2, 0xc8, 0x05, 0x0e, 0xc0, 0x01, 0x84, 0x0c, 0xe2, 0xc0, 0xa5, 0xf2, 0x36,
	0x6e, 0x13, 0xd1, 0xcd, 0x44, 0xb6, 0xb7, 0xe1, 0x33, 0xf8, 0x0c, 0x3e, 0x85, 0x63, 0x8f, 0x1c,
	0x51, 0xfb, 0x0b, 0x7c, 0x00, 0xf2, 0xb8, 0x09, 0xaa, 0x04, 0x7b, 0xb1, 0xe6, 0xbd, 0x99, 0x79,
	0x6f, 0x66, 0x64, 0x9a, 0x2e, 0xd4, 0x5a, 0x19, 0x5b, 0xcb, 0x62, 0x01, 0x5a, 0x15, 0x9b, 0x8b,
	0xa2, 0xd5, 0x00, 0x4b, 0xff, 0xe6, 0xad, 0x06, 0x0b, 0xec, 0x5e, 0x5f, 0x93, 0xbb, 0x9a, 0x7c,
	0x73, 0x91, 0x63, 0x36, 0xfd, 0x4d, 0x28, 0xfd, 0x50, 0x49, 0xad, 0xde, 0x3b, 0xc8, 0x18, 0x1d,
	0x97, 0xd2, 0x4a, 0x4e, 0x92, 0x20, 0x8b, 0x04, 0xc6, 0xec, 0x25, 0x8d, 0x8c, 0xab, 0x98, 0x63,
	0x87, 0xe1, 0x27, 0x49, 0x90, 0xcd, 0x9e, 0x24, 0xf9, 0xbf, 0x15, 0xf3, 0x77, 0x6f, 0x3f, 0xa2,
	0x96, 0x98, 0x99, 0x41, 0xd7, 0xb0, 0x47, 0x34, 0x6a, 0xe4, 0x95, 0x32, 0xad, 0x5c, 0xa8, 0x79,
	0x5d, 0xf2, 0x20, 0x21, 0x59, 0x24, 0x66, 0x03, 0xf7, 0xa6, 0x64, 0xcf, 0xe9, 0x6d, 0x0d, 0x9d,
	0x77, 0xe1, 0xe3, 0x84, 0xdc, 0x64, 0x22, 0xa0, 0xf3, 0x26, 0xa1, 0x3e, 0x44, 0xec, 0x31, 0xbd,
	0xf3, 0xd7, 0x61, 0xa3, 0xb4, 0xa9, 0xa1, 0xe1, 0x93, 0x84, 0x64, 0xa7, 0xe2, 0x6c, 0x48, 0x7c,
	0xf2, 0x7c, 0xfa, 0x9d, 0xd0, 0xb0, 0xd7, 0x60, 0x0f, 0xbc, 0xb1, 0x06, 0xb0, 0xe6, 0xb0, 0xb9,
	0x93, 0x15, 0x0e, 0xb3, 0x67, 0x74, 0x7a, 0xb4, 0xf7, 0xc3, 0xff, 0x8d, 0xe4, 0xe7, 0x39, 0x14,
	0xbb, 0x43, 0x3a, 0xbd, 0xc3, 0x9e, 0x18, 0x3b, 0x1f, 0x63, 0xa5, 0xb6, 0x73, 0x0d, 0x1d, 0x2e,
	0x78, 0x2a, 0x42, 0x24, 0x04, 0x74, 0xec, 0x3e, 0xbd, 0xa5, 0x9a, 0x12, 0x53, 0x7e, 0xe8, 0xa9,
	0x6a, 0x4a, 0x01, 0x5d, 0xaa, 0x68, 0xd8, 0x9f, 0x94, 0xdd, 0xa5, 0x13, 0x6c, 0xe0, 0x24, 0x21,
	0xd9, 0x44, 0x78, 0xc0, 0xce, 0x68, 0xa0, 0x9a, 0x92, 0x9f, 0x20, 0xe7, 0x42, 0x57, 0xd7, 0x40,
	0xa9, 0x0c, 0x0f, 0x70, 0x1b, 0x0f, 0x9c, 0xff, 0x5a, 0xc9, 0xe5, 0xbc, 0x92, 0xa6, 0x42, 0xff,
	0x48, 0x84, 0x8e, 0x78, 0x2d, 0x4d, 0x95, 0x2e, 0xe9, 0x64, 0xf0, 0xb0, 0x60, 0xe5, 0x1a, 0x3d,
	0x02, 0xe1, 0x81, 0x63, 0xeb, 0xa6, 0x54, 0x5f, 0xd1, 0x25, 0x10, 0x1e, 0x1c, 0x2b, 0x06, 0xc7,
	0x8a, 0xae, 0x45, 0x5e, 0x37, 0xd6, 0xf0, 0xb1, 0x1f, 0x02, 0xc1, 0x8b, 0x57, 0x3f, 0x76, 0x31,
	0xd9, 0xee, 0x62, 0xf2, 0x6b, 0x17, 0x93, 0x6f, 0xfb, 0x78, 0xb4, 0xdd, 0xc7, 0xa3, 0x9f, 0xfb,
	0x78, 0xf4, 0xf9, 0x7c, 0x55, 0xdb, 0xea, 0xfa, 0x32, 0x5f, 0xc0, 0x55, 0xd1, 0xdf, 0x18, 0xf4,
	0x6a, 0x88, 0xcf, 0x65, 0xdb, 0x16, 0xed, 0x97, 0x95, 0xff, 0xd7, 0x97, 0x53, 0xfc, 0xd8, 0x4f,
	0xff, 0x04, 0x00, 0x00, 0xff, 0xff, 0x1f, 0x27, 0x8a, 0x77, 0xfe, 0x02, 0x00, 0x00,
}

func (m *ShareProof) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *ShareProof) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *ShareProof) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if m.NamespaceVersion != 0 {
		i = encodeVarintProof(dAtA, i, uint64(m.NamespaceVersion))
		i--
		dAtA[i] = 0x28
	}
	if m.RowProof != nil {
		{
			size, err := m.RowProof.MarshalToSizedBuffer(dAtA[:i])
			if err != nil {
				return 0, err
			}
			i -= size
			i = encodeVarintProof(dAtA, i, uint64(size))
		}
		i--
		dAtA[i] = 0x22
	}
	if len(m.NamespaceId) > 0 {
		i -= len(m.NamespaceId)
		copy(dAtA[i:], m.NamespaceId)
		i = encodeVarintProof(dAtA, i, uint64(len(m.NamespaceId)))
		i--
		dAtA[i] = 0x1a
	}
	if len(m.ShareProofs) > 0 {
		for iNdEx := len(m.ShareProofs) - 1; iNdEx >= 0; iNdEx-- {
			{
				size, err := m.ShareProofs[iNdEx].MarshalToSizedBuffer(dAtA[:i])
				if err != nil {
					return 0, err
				}
				i -= size
				i = encodeVarintProof(dAtA, i, uint64(size))
			}
			i--
			dAtA[i] = 0x12
		}
	}
	if len(m.Data) > 0 {
		for iNdEx := len(m.Data) - 1; iNdEx >= 0; iNdEx-- {
			i -= len(m.Data[iNdEx])
			copy(dAtA[i:], m.Data[iNdEx])
			i = encodeVarintProof(dAtA, i, uint64(len(m.Data[iNdEx])))
			i--
			dAtA[i] = 0xa
		}
	}
	return len(dAtA) - i, nil
}

func (m *RowProof) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *RowProof) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *RowProof) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if m.EndRow != 0 {
		i = encodeVarintProof(dAtA, i, uint64(m.EndRow))
		i--
		dAtA[i] = 0x28
	}
	if m.StartRow != 0 {
		i = encodeVarintProof(dAtA, i, uint64(m.StartRow))
		i--
		dAtA[i] = 0x20
	}
	if len(m.Root) > 0 {
		i -= len(m.Root)
		copy(dAtA[i:], m.Root)
		i = encodeVarintProof(dAtA, i, uint64(len(m.Root)))
		i--
		dAtA[i] = 0x1a
	}
	if len(m.Proofs) > 0 {
		for iNdEx := len(m.Proofs) - 1; iNdEx >= 0; iNdEx-- {
			{
				size, err := m.Proofs[iNdEx].MarshalToSizedBuffer(dAtA[:i])
				if err != nil {
					return 0, err
				}
				i -= size
				i = encodeVarintProof(dAtA, i, uint64(size))
			}
			i--
			dAtA[i] = 0x12
		}
	}
	if len(m.RowRoots) > 0 {
		for iNdEx := len(m.RowRoots) - 1; iNdEx >= 0; iNdEx-- {
			i -= len(m.RowRoots[iNdEx])
			copy(dAtA[i:], m.RowRoots[iNdEx])
			i = encodeVarintProof(dAtA, i, uint64(len(m.RowRoots[iNdEx])))
			i--
			dAtA[i] = 0xa
		}
	}
	return len(dAtA) - i, nil
}

func (m *NMTProof) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *NMTProof) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *NMTProof) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.LeafHash) > 0 {
		i -= len(m.LeafHash)
		copy(dAtA[i:], m.LeafHash)
		i = encodeVarintProof(dAtA, i, uint64(len(m.LeafHash)))
		i--
		dAtA[i] = 0x22
	}
	if len(m.Nodes) > 0 {
		for iNdEx := len(m.Nodes) - 1; iNdEx >= 0; iNdEx-- {
			i -= len(m.Nodes[iNdEx])
			copy(dAtA[i:], m.Nodes[iNdEx])
			i = encodeVarintProof(dAtA, i, uint64(len(m.Nodes[iNdEx])))
			i--
			dAtA[i] = 0x1a
		}
	}
	if m.End != 0 {
		i = encodeVarintProof(dAtA, i, uint64(m.End))
		i--
		dAtA[i] = 0x10
	}
	if m.Start != 0 {
		i = encodeVarintProof(dAtA, i, uint64(m.Start))
		i--
		dAtA[i] = 0x8
	}
	return len(dAtA) - i, nil
}

func (m *Proof) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *Proof) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *Proof) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.Aunts) > 0 {
		for iNdEx := len(m.Aunts) - 1; iNdEx >= 0; iNdEx-- {
			i -= len(m.Aunts[iNdEx])
			copy(dAtA[i:], m.Aunts[iNdEx])
			i = encodeVarintProof(dAtA, i, uint64(len(m.Aunts[iNdEx])))
			i--
			dAtA[i] = 0x22
		}
	}
	if len(m.LeafHash) > 0 {
		i -= len(m.LeafHash)
		copy(dAtA[i:], m.LeafHash)
		i = encodeVarintProof(dAtA, i, uint64(len(m.LeafHash)))
		i--
		dAtA[i] = 0x1a
	}
	if m.Index != 0 {
		i = encodeVarintProof(dAtA, i, uint64(m.Index))
		i--
		dAtA[i] = 0x10
	}
	if m.Total != 0 {
		i = encodeVarintProof(dAtA, i, uint64(m.Total))
		i--
		dAtA[i] = 0x8
	}
	return len(dAtA) - i, nil
}

func encodeVarintProof(dAtA []byte, offset int, v uint64) int {
	offset -= sovProof(v)
	base := offset
	for v >= 1<<7 {
		dAtA[offset] = uint8(v&0x7f | 0x80)
		v >>= 7
		offset++
	}
	dAtA[offset] = uint8(v)
	return base
}
func (m *ShareProof) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	if len(m.Data) > 0 {
		for _, b := range m.Data {
			l = len(b)
			n += 1 + l + sovProof(uint64(l))
		}
	}
	if len(m.ShareProofs) > 0 {
		for _, e := range m.ShareProofs {
			l = e.Size()
			n += 1 + l + sovProof(uint64(l))
		}
	}
	l = len(m.NamespaceId)
	if l > 0 {
		n += 1 + l + sovProof(uint64(l))
	}
	if m.RowProof != nil {
		l = m.RowProof.Size()
		n += 1 + l + sovProof(uint64(l))
	}
	if m.NamespaceVersion != 0 {
		n += 1 + sovProof(uint64(m.NamespaceVersion))
	}
	return n
}

func (m *RowProof) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	if len(m.RowRoots) > 0 {
		for _, b := range m.RowRoots {
			l = len(b)
			n += 1 + l + sovProof(uint64(l))
		}
	}
	if len(m.Proofs) > 0 {
		for _, e := range m.Proofs {
			l = e.Size()
			n += 1 + l + sovProof(uint64(l))
		}
	}
	l = len(m.Root)
	if l > 0 {
		n += 1 + l + sovProof(uint64(l))
	}
	if m.StartRow != 0 {
		n += 1 + sovProof(uint64(m.StartRow))
	}
	if m.EndRow != 0 {
		n += 1 + sovProof(uint64(m.EndRow))
	}
	return n
}

func (m *NMTProof) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	if m.Start != 0 {
		n += 1 + sovProof(uint64(m.Start))
	}
	if m.End != 0 {
		n += 1 + sovProof(uint64(m.End))
	}
	if len(m.Nodes) > 0 {
		for _, b := range m.Nodes {
			l = len(b)
			n += 1 + l + sovProof(uint64(l))
		}
	}
	l = len(m.LeafHash)
	if l > 0 {
		n += 1 + l + sovProof(uint64(l))
	}
	return n
}

func (m *Proof) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	if m.Total != 0 {
		n += 1 + sovProof(uint64(m.Total))
	}
	if m.Index != 0 {
		n += 1 + sovProof(uint64(m.Index))
	}
	l = len(m.LeafHash)
	if l > 0 {
		n += 1 + l + sovProof(uint64(l))
	}
	if len(m.Aunts) > 0 {
		for _, b := range m.Aunts {
			l = len(b)
			n += 1 + l + sovProof(uint64(l))
		}
	}
	return n
}

func sovProof(x uint64) (n int) {
	return (math_bits.Len64(x|1) + 6) / 7
}
func sozProof(x uint64) (n int) {
	return sovProof(uint64((x << 1) ^ uint64((int64(x) >> 63))))
}
func (m *ShareProof) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowProof
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= uint64(b&0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: ShareProof: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: ShareProof: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Data", wireType)
			}
			var byteLen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				byteLen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if byteLen < 0 {
				return ErrInvalidLengthProof
			}
			postIndex := iNdEx + byteLen
			if postIndex < 0 {
				return ErrInvalidLengthProof
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Data = append(m.Data, make([]byte, postIndex-iNdEx))
			copy(m.Data[len(m.Data)-1], dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		case 2:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field ShareProofs", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthProof
			}
			postIndex := iNdEx + msglen
			if postIndex < 0 {
				return ErrInvalidLengthProof
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.ShareProofs = append(m.ShareProofs, &NMTProof{})
			if err := m.ShareProofs[len(m.ShareProofs)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 3:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field NamespaceId", wireType)
			}
			var byteLen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				byteLen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if byteLen < 0 {
				return ErrInvalidLengthProof
			}
			postIndex := iNdEx + byteLen
			if postIndex < 0 {
				return ErrInvalidLengthProof
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.NamespaceId = append(m.NamespaceId[:0], dAtA[iNdEx:postIndex]...)
			if m.NamespaceId == nil {
				m.NamespaceId = []byte{}
			}
			iNdEx = postIndex
		case 4:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field RowProof", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthProof
			}
			postIndex := iNdEx + msglen
			if postIndex < 0 {
				return ErrInvalidLengthProof
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			if m.RowProof == nil {
				m.RowProof = &RowProof{}
			}
			if err := m.RowProof.Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 5:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field NamespaceVersion", wireType)
			}
			m.NamespaceVersion = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.NamespaceVersion |= uint32(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		default:
			iNdEx = preIndex
			skippy, err := skipProof(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if (skippy < 0) || (iNdEx+skippy) < 0 {
				return ErrInvalidLengthProof
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func (m *RowProof) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowProof
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= uint64(b&0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: RowProof: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: RowProof: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field RowRoots", wireType)
			}
			var byteLen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				byteLen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if byteLen < 0 {
				return ErrInvalidLengthProof
			}
			postIndex := iNdEx + byteLen
			if postIndex < 0 {
				return ErrInvalidLengthProof
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.RowRoots = append(m.RowRoots, make([]byte, postIndex-iNdEx))
			copy(m.RowRoots[len(m.RowRoots)-1], dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		case 2:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Proofs", wireType)
			}
			var msglen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				msglen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if msglen < 0 {
				return ErrInvalidLengthProof
			}
			postIndex := iNdEx + msglen
			if postIndex < 0 {
				return ErrInvalidLengthProof
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Proofs = append(m.Proofs, &Proof{})
			if err := m.Proofs[len(m.Proofs)-1].Unmarshal(dAtA[iNdEx:postIndex]); err != nil {
				return err
			}
			iNdEx = postIndex
		case 3:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Root", wireType)
			}
			var byteLen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				byteLen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if byteLen < 0 {
				return ErrInvalidLengthProof
			}
			postIndex := iNdEx + byteLen
			if postIndex < 0 {
				return ErrInvalidLengthProof
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Root = append(m.Root[:0], dAtA[iNdEx:postIndex]...)
			if m.Root == nil {
				m.Root = []byte{}
			}
			iNdEx = postIndex
		case 4:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field StartRow", wireType)
			}
			m.StartRow = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.StartRow |= uint32(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		case 5:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field EndRow", wireType)
			}
			m.EndRow = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.EndRow |= uint32(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		default:
			iNdEx = preIndex
			skippy, err := skipProof(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if (skippy < 0) || (iNdEx+skippy) < 0 {
				return ErrInvalidLengthProof
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func (m *NMTProof) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowProof
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= uint64(b&0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: NMTProof: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: NMTProof: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field Start", wireType)
			}
			m.Start = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.Start |= int32(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		case 2:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field End", wireType)
			}
			m.End = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.End |= int32(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		case 3:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Nodes", wireType)
			}
			var byteLen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				byteLen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if byteLen < 0 {
				return ErrInvalidLengthProof
			}
			postIndex := iNdEx + byteLen
			if postIndex < 0 {
				return ErrInvalidLengthProof
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Nodes = append(m.Nodes, make([]byte, postIndex-iNdEx))
			copy(m.Nodes[len(m.Nodes)-1], dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		case 4:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field LeafHash", wireType)
			}
			var byteLen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				byteLen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if byteLen < 0 {
				return ErrInvalidLengthProof
			}
			postIndex := iNdEx + byteLen
			if postIndex < 0 {
				return ErrInvalidLengthProof
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.LeafHash = append(m.LeafHash[:0], dAtA[iNdEx:postIndex]...)
			if m.LeafHash == nil {
				m.LeafHash = []byte{}
			}
			iNdEx = postIndex
		default:
			iNdEx = preIndex
			skippy, err := skipProof(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if (skippy < 0) || (iNdEx+skippy) < 0 {
				return ErrInvalidLengthProof
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func (m *Proof) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowProof
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= uint64(b&0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		if wireType == 4 {
			return fmt.Errorf("proto: Proof: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: Proof: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field Total", wireType)
			}
			m.Total = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.Total |= int64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		case 2:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field Index", wireType)
			}
			m.Index = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.Index |= int64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		case 3:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field LeafHash", wireType)
			}
			var byteLen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				byteLen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if byteLen < 0 {
				return ErrInvalidLengthProof
			}
			postIndex := iNdEx + byteLen
			if postIndex < 0 {
				return ErrInvalidLengthProof
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.LeafHash = append(m.LeafHash[:0], dAtA[iNdEx:postIndex]...)
			if m.LeafHash == nil {
				m.LeafHash = []byte{}
			}
			iNdEx = postIndex
		case 4:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Aunts", wireType)
			}
			var byteLen int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowProof
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				byteLen |= int(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if byteLen < 0 {
				return ErrInvalidLengthProof
			}
			postIndex := iNdEx + byteLen
			if postIndex < 0 {
				return ErrInvalidLengthProof
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Aunts = append(m.Aunts, make([]byte, postIndex-iNdEx))
			copy(m.Aunts[len(m.Aunts)-1], dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		default:
			iNdEx = preIndex
			skippy, err := skipProof(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if (skippy < 0) || (iNdEx+skippy) < 0 {
				return ErrInvalidLengthProof
			}
			if (iNdEx + skippy) > l {
				return io.ErrUnexpectedEOF
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func skipProof(dAtA []byte) (n int, err error) {
	l := len(dAtA)
	iNdEx := 0
	depth := 0
	for iNdEx < l {
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return 0, ErrIntOverflowProof
			}
			if iNdEx >= l {
				return 0, io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= (uint64(b) & 0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		wireType := int(wire & 0x7)
		switch wireType {
		case 0:
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return 0, ErrIntOverflowProof
				}
				if iNdEx >= l {
					return 0, io.ErrUnexpectedEOF
				}
				iNdEx++
				if dAtA[iNdEx-1] < 0x80 {
					break
				}
			}
		case 1:
			iNdEx += 8
		case 2:
			var length int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return 0, ErrIntOverflowProof
				}
				if iNdEx >= l {
					return 0, io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				length |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if length < 0 {
				return 0, ErrInvalidLengthProof
			}
			iNdEx += length
		case 3:
			depth++
		case 4:
			if depth == 0 {
				return 0, ErrUnexpectedEndOfGroupProof
			}
			depth--
		case 5:
			iNdEx += 4
		default:
			return 0, fmt.Errorf("proto: illegal wireType %d", wireType)
		}
		if iNdEx < 0 {
			return 0, ErrInvalidLengthProof
		}
		if depth == 0 {
			return iNdEx, nil
		}
	}
	return 0, io.ErrUnexpectedEOF
}

var (
	ErrInvalidLengthProof        = fmt.Errorf("proto: negative length found during unmarshaling")
	ErrIntOverflowProof          = fmt.Errorf("proto: integer overflow")
	ErrUnexpectedEndOfGroupProof = fmt.Errorf("proto: unexpected end of group")
)

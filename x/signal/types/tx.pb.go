// Code generated by protoc-gen-gogo. DO NOT EDIT.
// source: celestia/signal/v2/tx.proto

package types

import (
	context "context"
	fmt "fmt"
	grpc1 "github.com/gogo/protobuf/grpc"
	proto "github.com/gogo/protobuf/proto"
	_ "google.golang.org/genproto/googleapis/api/annotations"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
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

// MsgSignalVersion signals for an upgrade.
type MsgSignalVersion struct {
	ValidatorAddress string `protobuf:"bytes,1,opt,name=validator_address,json=validatorAddress,proto3" json:"validator_address,omitempty"`
	Version          uint64 `protobuf:"varint,2,opt,name=version,proto3" json:"version,omitempty"`
}

func (m *MsgSignalVersion) Reset()         { *m = MsgSignalVersion{} }
func (m *MsgSignalVersion) String() string { return proto.CompactTextString(m) }
func (*MsgSignalVersion) ProtoMessage()    {}
func (*MsgSignalVersion) Descriptor() ([]byte, []int) {
	return fileDescriptor_152fd796bcafe44c, []int{0}
}
func (m *MsgSignalVersion) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *MsgSignalVersion) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_MsgSignalVersion.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalToSizedBuffer(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (m *MsgSignalVersion) XXX_Merge(src proto.Message) {
	xxx_messageInfo_MsgSignalVersion.Merge(m, src)
}
func (m *MsgSignalVersion) XXX_Size() int {
	return m.Size()
}
func (m *MsgSignalVersion) XXX_DiscardUnknown() {
	xxx_messageInfo_MsgSignalVersion.DiscardUnknown(m)
}

var xxx_messageInfo_MsgSignalVersion proto.InternalMessageInfo

func (m *MsgSignalVersion) GetValidatorAddress() string {
	if m != nil {
		return m.ValidatorAddress
	}
	return ""
}

func (m *MsgSignalVersion) GetVersion() uint64 {
	if m != nil {
		return m.Version
	}
	return 0
}

// MsgSignalVersionResponse is the response type for the SignalVersion method.
type MsgSignalVersionResponse struct {
}

func (m *MsgSignalVersionResponse) Reset()         { *m = MsgSignalVersionResponse{} }
func (m *MsgSignalVersionResponse) String() string { return proto.CompactTextString(m) }
func (*MsgSignalVersionResponse) ProtoMessage()    {}
func (*MsgSignalVersionResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_152fd796bcafe44c, []int{1}
}
func (m *MsgSignalVersionResponse) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *MsgSignalVersionResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_MsgSignalVersionResponse.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalToSizedBuffer(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (m *MsgSignalVersionResponse) XXX_Merge(src proto.Message) {
	xxx_messageInfo_MsgSignalVersionResponse.Merge(m, src)
}
func (m *MsgSignalVersionResponse) XXX_Size() int {
	return m.Size()
}
func (m *MsgSignalVersionResponse) XXX_DiscardUnknown() {
	xxx_messageInfo_MsgSignalVersionResponse.DiscardUnknown(m)
}

var xxx_messageInfo_MsgSignalVersionResponse proto.InternalMessageInfo

// MsgTryUpgrade tries to upgrade the chain.
type MsgTryUpgrade struct {
	Signer string `protobuf:"bytes,1,opt,name=signer,proto3" json:"signer,omitempty"`
}

func (m *MsgTryUpgrade) Reset()         { *m = MsgTryUpgrade{} }
func (m *MsgTryUpgrade) String() string { return proto.CompactTextString(m) }
func (*MsgTryUpgrade) ProtoMessage()    {}
func (*MsgTryUpgrade) Descriptor() ([]byte, []int) {
	return fileDescriptor_152fd796bcafe44c, []int{2}
}
func (m *MsgTryUpgrade) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *MsgTryUpgrade) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_MsgTryUpgrade.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalToSizedBuffer(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (m *MsgTryUpgrade) XXX_Merge(src proto.Message) {
	xxx_messageInfo_MsgTryUpgrade.Merge(m, src)
}
func (m *MsgTryUpgrade) XXX_Size() int {
	return m.Size()
}
func (m *MsgTryUpgrade) XXX_DiscardUnknown() {
	xxx_messageInfo_MsgTryUpgrade.DiscardUnknown(m)
}

var xxx_messageInfo_MsgTryUpgrade proto.InternalMessageInfo

func (m *MsgTryUpgrade) GetSigner() string {
	if m != nil {
		return m.Signer
	}
	return ""
}

// MsgTryUpgradeResponse is the response type for the TryUpgrade method.
type MsgTryUpgradeResponse struct {
}

func (m *MsgTryUpgradeResponse) Reset()         { *m = MsgTryUpgradeResponse{} }
func (m *MsgTryUpgradeResponse) String() string { return proto.CompactTextString(m) }
func (*MsgTryUpgradeResponse) ProtoMessage()    {}
func (*MsgTryUpgradeResponse) Descriptor() ([]byte, []int) {
	return fileDescriptor_152fd796bcafe44c, []int{3}
}
func (m *MsgTryUpgradeResponse) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *MsgTryUpgradeResponse) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_MsgTryUpgradeResponse.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalToSizedBuffer(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (m *MsgTryUpgradeResponse) XXX_Merge(src proto.Message) {
	xxx_messageInfo_MsgTryUpgradeResponse.Merge(m, src)
}
func (m *MsgTryUpgradeResponse) XXX_Size() int {
	return m.Size()
}
func (m *MsgTryUpgradeResponse) XXX_DiscardUnknown() {
	xxx_messageInfo_MsgTryUpgradeResponse.DiscardUnknown(m)
}

var xxx_messageInfo_MsgTryUpgradeResponse proto.InternalMessageInfo

func init() {
	proto.RegisterType((*MsgSignalVersion)(nil), "celestia.signal.v2.MsgSignalVersion")
	proto.RegisterType((*MsgSignalVersionResponse)(nil), "celestia.signal.v2.MsgSignalVersionResponse")
	proto.RegisterType((*MsgTryUpgrade)(nil), "celestia.signal.v2.MsgTryUpgrade")
	proto.RegisterType((*MsgTryUpgradeResponse)(nil), "celestia.signal.v2.MsgTryUpgradeResponse")
}

func init() { proto.RegisterFile("celestia/signal/v2/tx.proto", fileDescriptor_152fd796bcafe44c) }

var fileDescriptor_152fd796bcafe44c = []byte{
	// 344 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0x8c, 0x92, 0xcd, 0x4a, 0x03, 0x31,
	0x14, 0x85, 0x9b, 0x2a, 0x15, 0x03, 0x85, 0x36, 0xfe, 0x8d, 0xa3, 0x0c, 0x75, 0x10, 0xac, 0xa8,
	0x13, 0x1c, 0x9f, 0x40, 0xd7, 0x76, 0x53, 0x7f, 0x40, 0x37, 0x92, 0x76, 0x42, 0x0c, 0x8c, 0x49,
	0x48, 0xd2, 0xa1, 0xdd, 0xb8, 0xf0, 0x09, 0x04, 0x5f, 0xca, 0x65, 0xc1, 0x8d, 0x4b, 0x69, 0x7d,
	0x0d, 0x41, 0xec, 0x74, 0xc6, 0xb6, 0x22, 0xba, 0xbb, 0x77, 0xee, 0x77, 0xcf, 0x39, 0x73, 0x09,
	0xdc, 0x68, 0xd3, 0x98, 0x1a, 0xcb, 0x09, 0x36, 0x9c, 0x09, 0x12, 0xe3, 0x24, 0xc4, 0xb6, 0x1b,
	0x28, 0x2d, 0xad, 0x44, 0x28, 0x1b, 0x06, 0xe9, 0x30, 0x48, 0x42, 0x77, 0x93, 0x49, 0xc9, 0x62,
	0x8a, 0x89, 0xe2, 0x98, 0x08, 0x21, 0x2d, 0xb1, 0x5c, 0x0a, 0x93, 0x6e, 0xf8, 0x57, 0xb0, 0xd2,
	0x30, 0xec, 0x6c, 0x44, 0x5f, 0x52, 0x6d, 0xb8, 0x14, 0x68, 0x0f, 0x56, 0x13, 0x12, 0xf3, 0x88,
	0x58, 0xa9, 0x6f, 0x48, 0x14, 0x69, 0x6a, 0x8c, 0x03, 0x6a, 0xa0, 0xbe, 0xd8, 0xac, 0xe4, 0x83,
	0xe3, 0xf4, 0x3b, 0x72, 0xe0, 0x42, 0x92, 0xee, 0x39, 0xc5, 0x1a, 0xa8, 0xcf, 0x37, 0xb3, 0xd6,
	0x77, 0xa1, 0x33, 0x2b, 0xdd, 0xa4, 0x46, 0x49, 0x61, 0xa8, 0xbf, 0x03, 0xcb, 0x0d, 0xc3, 0xce,
	0x75, 0xef, 0x42, 0x31, 0x4d, 0x22, 0x8a, 0x56, 0x61, 0xe9, 0x2b, 0x32, 0xd5, 0x63, 0xa3, 0x71,
	0xe7, 0xaf, 0xc1, 0x95, 0x29, 0x30, 0x53, 0x08, 0x3f, 0x00, 0x9c, 0x6b, 0x18, 0x86, 0xee, 0x61,
	0x79, 0x3a, 0xfd, 0x76, 0xf0, 0xf3, 0x08, 0xc1, 0x6c, 0x10, 0x77, 0xff, 0x3f, 0x54, 0x1e, 0x77,
	0xfd, 0xe1, 0xe5, 0xfd, 0xa9, 0xb8, 0xe4, 0x57, 0xf3, 0xa3, 0x1f, 0x8e, 0x2b, 0x94, 0x40, 0x38,
	0xf1, 0x1b, 0x5b, 0xbf, 0xc8, 0x7e, 0x23, 0xee, 0xee, 0x9f, 0x48, 0x6e, 0xeb, 0x8e, 0x6c, 0x97,
	0x7d, 0x34, 0x61, 0xdb, 0x49, 0x99, 0x93, 0xd3, 0xe7, 0x81, 0x07, 0xfa, 0x03, 0x0f, 0xbc, 0x0d,
	0x3c, 0xf0, 0x38, 0xf4, 0x0a, 0xfd, 0xa1, 0x57, 0x78, 0x1d, 0x7a, 0x85, 0xeb, 0x90, 0x71, 0x7b,
	0xdb, 0x69, 0x05, 0x6d, 0x79, 0x87, 0x33, 0x2b, 0xa9, 0x59, 0x5e, 0x1f, 0x10, 0xa5, 0x70, 0x37,
	0x93, 0xb4, 0x3d, 0x45, 0x4d, 0xab, 0x34, 0x7a, 0x0d, 0x47, 0x9f, 0x01, 0x00, 0x00, 0xff, 0xff,
	0xb1, 0xbb, 0x3b, 0x16, 0x5e, 0x02, 0x00, 0x00,
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// MsgClient is the client API for Msg service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type MsgClient interface {
	// SignalVersion allows a validator to signal for a version.
	SignalVersion(ctx context.Context, in *MsgSignalVersion, opts ...grpc.CallOption) (*MsgSignalVersionResponse, error)
	// TryUpgrade tallies all the votes for all the versions to determine if a
	// quorum has been reached for a version.
	TryUpgrade(ctx context.Context, in *MsgTryUpgrade, opts ...grpc.CallOption) (*MsgTryUpgradeResponse, error)
}

type msgClient struct {
	cc grpc1.ClientConn
}

func NewMsgClient(cc grpc1.ClientConn) MsgClient {
	return &msgClient{cc}
}

func (c *msgClient) SignalVersion(ctx context.Context, in *MsgSignalVersion, opts ...grpc.CallOption) (*MsgSignalVersionResponse, error) {
	out := new(MsgSignalVersionResponse)
	err := c.cc.Invoke(ctx, "/celestia.signal.v2.Msg/SignalVersion", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *msgClient) TryUpgrade(ctx context.Context, in *MsgTryUpgrade, opts ...grpc.CallOption) (*MsgTryUpgradeResponse, error) {
	out := new(MsgTryUpgradeResponse)
	err := c.cc.Invoke(ctx, "/celestia.signal.v2.Msg/TryUpgrade", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// MsgServer is the server API for Msg service.
type MsgServer interface {
	// SignalVersion allows a validator to signal for a version.
	SignalVersion(context.Context, *MsgSignalVersion) (*MsgSignalVersionResponse, error)
	// TryUpgrade tallies all the votes for all the versions to determine if a
	// quorum has been reached for a version.
	TryUpgrade(context.Context, *MsgTryUpgrade) (*MsgTryUpgradeResponse, error)
}

// UnimplementedMsgServer can be embedded to have forward compatible implementations.
type UnimplementedMsgServer struct {
}

func (*UnimplementedMsgServer) SignalVersion(ctx context.Context, req *MsgSignalVersion) (*MsgSignalVersionResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SignalVersion not implemented")
}
func (*UnimplementedMsgServer) TryUpgrade(ctx context.Context, req *MsgTryUpgrade) (*MsgTryUpgradeResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method TryUpgrade not implemented")
}

func RegisterMsgServer(s grpc1.Server, srv MsgServer) {
	s.RegisterService(&_Msg_serviceDesc, srv)
}

func _Msg_SignalVersion_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgSignalVersion)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).SignalVersion(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/celestia.signal.v2.Msg/SignalVersion",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).SignalVersion(ctx, req.(*MsgSignalVersion))
	}
	return interceptor(ctx, in, info, handler)
}

func _Msg_TryUpgrade_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MsgTryUpgrade)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MsgServer).TryUpgrade(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/celestia.signal.v2.Msg/TryUpgrade",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MsgServer).TryUpgrade(ctx, req.(*MsgTryUpgrade))
	}
	return interceptor(ctx, in, info, handler)
}

var _Msg_serviceDesc = grpc.ServiceDesc{
	ServiceName: "celestia.signal.v2.Msg",
	HandlerType: (*MsgServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "SignalVersion",
			Handler:    _Msg_SignalVersion_Handler,
		},
		{
			MethodName: "TryUpgrade",
			Handler:    _Msg_TryUpgrade_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "celestia/signal/v2/tx.proto",
}

func (m *MsgSignalVersion) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *MsgSignalVersion) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *MsgSignalVersion) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if m.Version != 0 {
		i = encodeVarintTx(dAtA, i, uint64(m.Version))
		i--
		dAtA[i] = 0x10
	}
	if len(m.ValidatorAddress) > 0 {
		i -= len(m.ValidatorAddress)
		copy(dAtA[i:], m.ValidatorAddress)
		i = encodeVarintTx(dAtA, i, uint64(len(m.ValidatorAddress)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

func (m *MsgSignalVersionResponse) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *MsgSignalVersionResponse) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *MsgSignalVersionResponse) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	return len(dAtA) - i, nil
}

func (m *MsgTryUpgrade) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *MsgTryUpgrade) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *MsgTryUpgrade) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if len(m.Signer) > 0 {
		i -= len(m.Signer)
		copy(dAtA[i:], m.Signer)
		i = encodeVarintTx(dAtA, i, uint64(len(m.Signer)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

func (m *MsgTryUpgradeResponse) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *MsgTryUpgradeResponse) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *MsgTryUpgradeResponse) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	return len(dAtA) - i, nil
}

func encodeVarintTx(dAtA []byte, offset int, v uint64) int {
	offset -= sovTx(v)
	base := offset
	for v >= 1<<7 {
		dAtA[offset] = uint8(v&0x7f | 0x80)
		v >>= 7
		offset++
	}
	dAtA[offset] = uint8(v)
	return base
}
func (m *MsgSignalVersion) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.ValidatorAddress)
	if l > 0 {
		n += 1 + l + sovTx(uint64(l))
	}
	if m.Version != 0 {
		n += 1 + sovTx(uint64(m.Version))
	}
	return n
}

func (m *MsgSignalVersionResponse) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	return n
}

func (m *MsgTryUpgrade) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.Signer)
	if l > 0 {
		n += 1 + l + sovTx(uint64(l))
	}
	return n
}

func (m *MsgTryUpgradeResponse) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	return n
}

func sovTx(x uint64) (n int) {
	return (math_bits.Len64(x|1) + 6) / 7
}
func sozTx(x uint64) (n int) {
	return sovTx(uint64((x << 1) ^ uint64((int64(x) >> 63))))
}
func (m *MsgSignalVersion) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowTx
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
			return fmt.Errorf("proto: MsgSignalVersion: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: MsgSignalVersion: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field ValidatorAddress", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowTx
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				stringLen |= uint64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			intStringLen := int(stringLen)
			if intStringLen < 0 {
				return ErrInvalidLengthTx
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return ErrInvalidLengthTx
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.ValidatorAddress = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		case 2:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field Version", wireType)
			}
			m.Version = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowTx
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.Version |= uint64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		default:
			iNdEx = preIndex
			skippy, err := skipTx(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if (skippy < 0) || (iNdEx+skippy) < 0 {
				return ErrInvalidLengthTx
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
func (m *MsgSignalVersionResponse) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowTx
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
			return fmt.Errorf("proto: MsgSignalVersionResponse: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: MsgSignalVersionResponse: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		default:
			iNdEx = preIndex
			skippy, err := skipTx(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if (skippy < 0) || (iNdEx+skippy) < 0 {
				return ErrInvalidLengthTx
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
func (m *MsgTryUpgrade) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowTx
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
			return fmt.Errorf("proto: MsgTryUpgrade: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: MsgTryUpgrade: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field Signer", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowTx
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				stringLen |= uint64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			intStringLen := int(stringLen)
			if intStringLen < 0 {
				return ErrInvalidLengthTx
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return ErrInvalidLengthTx
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.Signer = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		default:
			iNdEx = preIndex
			skippy, err := skipTx(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if (skippy < 0) || (iNdEx+skippy) < 0 {
				return ErrInvalidLengthTx
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
func (m *MsgTryUpgradeResponse) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowTx
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
			return fmt.Errorf("proto: MsgTryUpgradeResponse: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: MsgTryUpgradeResponse: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		default:
			iNdEx = preIndex
			skippy, err := skipTx(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if (skippy < 0) || (iNdEx+skippy) < 0 {
				return ErrInvalidLengthTx
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
func skipTx(dAtA []byte) (n int, err error) {
	l := len(dAtA)
	iNdEx := 0
	depth := 0
	for iNdEx < l {
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return 0, ErrIntOverflowTx
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
					return 0, ErrIntOverflowTx
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
					return 0, ErrIntOverflowTx
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
				return 0, ErrInvalidLengthTx
			}
			iNdEx += length
		case 3:
			depth++
		case 4:
			if depth == 0 {
				return 0, ErrUnexpectedEndOfGroupTx
			}
			depth--
		case 5:
			iNdEx += 4
		default:
			return 0, fmt.Errorf("proto: illegal wireType %d", wireType)
		}
		if iNdEx < 0 {
			return 0, ErrInvalidLengthTx
		}
		if depth == 0 {
			return iNdEx, nil
		}
	}
	return 0, io.ErrUnexpectedEOF
}

var (
	ErrInvalidLengthTx        = fmt.Errorf("proto: negative length found during unmarshaling")
	ErrIntOverflowTx          = fmt.Errorf("proto: integer overflow")
	ErrUnexpectedEndOfGroupTx = fmt.Errorf("proto: unexpected end of group")
)

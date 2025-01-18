// Code generated by protoc-gen-gogo. DO NOT EDIT.
// source: celestia/signal/v1/upgrade.proto

package types

import (
	fmt "fmt"
	proto "github.com/gogo/protobuf/proto"
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

// Upgrade is a type that represents a network upgrade.
type Upgrade struct {
	// AppVersion is the app version that has received a quorum of validators to
	// signal for it.
	AppVersion uint64 `protobuf:"varint,1,opt,name=app_version,json=appVersion,proto3" json:"app_version,omitempty"`
	// UpgradeHeight is the height at which the network should upgrade to the
	// AppVersion.
	UpgradeHeight int64 `protobuf:"varint,2,opt,name=upgrade_height,json=upgradeHeight,proto3" json:"upgrade_height,omitempty"`
}

func (m *Upgrade) Reset()         { *m = Upgrade{} }
func (m *Upgrade) String() string { return proto.CompactTextString(m) }
func (*Upgrade) ProtoMessage()    {}
func (*Upgrade) Descriptor() ([]byte, []int) {
	return fileDescriptor_7872d1b4aca9f179, []int{0}
}
func (m *Upgrade) XXX_Unmarshal(b []byte) error {
	return m.Unmarshal(b)
}
func (m *Upgrade) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	if deterministic {
		return xxx_messageInfo_Upgrade.Marshal(b, m, deterministic)
	} else {
		b = b[:cap(b)]
		n, err := m.MarshalToSizedBuffer(b)
		if err != nil {
			return nil, err
		}
		return b[:n], nil
	}
}
func (m *Upgrade) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Upgrade.Merge(m, src)
}
func (m *Upgrade) XXX_Size() int {
	return m.Size()
}
func (m *Upgrade) XXX_DiscardUnknown() {
	xxx_messageInfo_Upgrade.DiscardUnknown(m)
}

var xxx_messageInfo_Upgrade proto.InternalMessageInfo

func (m *Upgrade) GetAppVersion() uint64 {
	if m != nil {
		return m.AppVersion
	}
	return 0
}

func (m *Upgrade) GetUpgradeHeight() int64 {
	if m != nil {
		return m.UpgradeHeight
	}
	return 0
}

func init() {
	proto.RegisterType((*Upgrade)(nil), "celestia.signal.v1.Upgrade")
}

func init() { proto.RegisterFile("celestia/signal/v1/upgrade.proto", fileDescriptor_7872d1b4aca9f179) }

var fileDescriptor_7872d1b4aca9f179 = []byte{
	// 196 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xe2, 0x52, 0x48, 0x4e, 0xcd, 0x49,
	0x2d, 0x2e, 0xc9, 0x4c, 0xd4, 0x2f, 0xce, 0x4c, 0xcf, 0x4b, 0xcc, 0xd1, 0x2f, 0x33, 0xd4, 0x2f,
	0x2d, 0x48, 0x2f, 0x4a, 0x4c, 0x49, 0xd5, 0x2b, 0x28, 0xca, 0x2f, 0xc9, 0x17, 0x12, 0x82, 0xa9,
	0xd0, 0x83, 0xa8, 0xd0, 0x2b, 0x33, 0x54, 0x0a, 0xe4, 0x62, 0x0f, 0x85, 0x28, 0x12, 0x92, 0xe7,
	0xe2, 0x4e, 0x2c, 0x28, 0x88, 0x2f, 0x4b, 0x2d, 0x2a, 0xce, 0xcc, 0xcf, 0x93, 0x60, 0x54, 0x60,
	0xd4, 0x60, 0x09, 0xe2, 0x4a, 0x2c, 0x28, 0x08, 0x83, 0x88, 0x08, 0xa9, 0x72, 0xf1, 0x41, 0x0d,
	0x8c, 0xcf, 0x48, 0xcd, 0x4c, 0xcf, 0x28, 0x91, 0x60, 0x52, 0x60, 0xd4, 0x60, 0x0e, 0xe2, 0x85,
	0x8a, 0x7a, 0x80, 0x05, 0x9d, 0x7c, 0x4e, 0x3c, 0x92, 0x63, 0xbc, 0xf0, 0x48, 0x8e, 0xf1, 0xc1,
	0x23, 0x39, 0xc6, 0x09, 0x8f, 0xe5, 0x18, 0x2e, 0x3c, 0x96, 0x63, 0xb8, 0xf1, 0x58, 0x8e, 0x21,
	0xca, 0x28, 0x3d, 0xb3, 0x24, 0xa3, 0x34, 0x49, 0x2f, 0x39, 0x3f, 0x57, 0x1f, 0xe6, 0x96, 0xfc,
	0xa2, 0x74, 0x38, 0x5b, 0x37, 0xb1, 0xa0, 0x40, 0xbf, 0x02, 0xe6, 0xfe, 0x92, 0xca, 0x82, 0xd4,
	0xe2, 0x24, 0x36, 0xb0, 0xdb, 0x8d, 0x01, 0x01, 0x00, 0x00, 0xff, 0xff, 0x63, 0x22, 0x2e, 0x9d,
	0xdf, 0x00, 0x00, 0x00,
}

func (m *Upgrade) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *Upgrade) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *Upgrade) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	var l int
	_ = l
	if m.UpgradeHeight != 0 {
		i = encodeVarintUpgrade(dAtA, i, uint64(m.UpgradeHeight))
		i--
		dAtA[i] = 0x10
	}
	if m.AppVersion != 0 {
		i = encodeVarintUpgrade(dAtA, i, uint64(m.AppVersion))
		i--
		dAtA[i] = 0x8
	}
	return len(dAtA) - i, nil
}

func encodeVarintUpgrade(dAtA []byte, offset int, v uint64) int {
	offset -= sovUpgrade(v)
	base := offset
	for v >= 1<<7 {
		dAtA[offset] = uint8(v&0x7f | 0x80)
		v >>= 7
		offset++
	}
	dAtA[offset] = uint8(v)
	return base
}
func (m *Upgrade) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	if m.AppVersion != 0 {
		n += 1 + sovUpgrade(uint64(m.AppVersion))
	}
	if m.UpgradeHeight != 0 {
		n += 1 + sovUpgrade(uint64(m.UpgradeHeight))
	}
	return n
}

func sovUpgrade(x uint64) (n int) {
	return (math_bits.Len64(x|1) + 6) / 7
}
func sozUpgrade(x uint64) (n int) {
	return sovUpgrade(uint64((x << 1) ^ uint64((int64(x) >> 63))))
}
func (m *Upgrade) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return ErrIntOverflowUpgrade
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
			return fmt.Errorf("proto: Upgrade: wiretype end group for non-group")
		}
		if fieldNum <= 0 {
			return fmt.Errorf("proto: Upgrade: illegal tag %d (wire type %d)", fieldNum, wire)
		}
		switch fieldNum {
		case 1:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field AppVersion", wireType)
			}
			m.AppVersion = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowUpgrade
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.AppVersion |= uint64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		case 2:
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field UpgradeHeight", wireType)
			}
			m.UpgradeHeight = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return ErrIntOverflowUpgrade
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.UpgradeHeight |= int64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		default:
			iNdEx = preIndex
			skippy, err := skipUpgrade(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if (skippy < 0) || (iNdEx+skippy) < 0 {
				return ErrInvalidLengthUpgrade
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
func skipUpgrade(dAtA []byte) (n int, err error) {
	l := len(dAtA)
	iNdEx := 0
	depth := 0
	for iNdEx < l {
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return 0, ErrIntOverflowUpgrade
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
					return 0, ErrIntOverflowUpgrade
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
					return 0, ErrIntOverflowUpgrade
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
				return 0, ErrInvalidLengthUpgrade
			}
			iNdEx += length
		case 3:
			depth++
		case 4:
			if depth == 0 {
				return 0, ErrUnexpectedEndOfGroupUpgrade
			}
			depth--
		case 5:
			iNdEx += 4
		default:
			return 0, fmt.Errorf("proto: illegal wireType %d", wireType)
		}
		if iNdEx < 0 {
			return 0, ErrInvalidLengthUpgrade
		}
		if depth == 0 {
			return iNdEx, nil
		}
	}
	return 0, io.ErrUnexpectedEOF
}

var (
	ErrInvalidLengthUpgrade        = fmt.Errorf("proto: negative length found during unmarshalling")
	ErrIntOverflowUpgrade          = fmt.Errorf("proto: integer overflow")
	ErrUnexpectedEndOfGroupUpgrade = fmt.Errorf("proto: unexpected end of group")
)

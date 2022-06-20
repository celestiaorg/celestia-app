package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/gogo/protobuf/proto"
)

//var _ AttestationRequestI = &AttestationRequest{}

type AttestationType int64

const (
	DataCommitmentRequestType AttestationType = iota
	ValsetRequestType
)

// AttestationRequestI is either a DataCommitment or a Valset.
// This was decided as part of the universal nonce approach under:
// https://github.com/celestiaorg/celestia-app/issues/468#issuecomment-1156887715
type AttestationRequestI interface {
	proto.Message
	codec.ProtoMarshaler
	Type() AttestationType
	GetNonce() uint64
}

//type AttestationRequest struct {
//	valset         *Valset
//	dataCommitment *DataCommitment
//	// using the name kind because `type` is a reserved keyword
//	kind AttestationType
//}
//
//func NewValsetAttestation(vs *Valset) *AttestationRequest {
//	return &AttestationRequest{
//		valset: vs,
//		kind:   ValsetRequestType,
//	}
//}
//
//func NewDataCommitmentAttestation(dc *DataCommitment) *AttestationRequest {
//	return &AttestationRequest{
//		dataCommitment: dc,
//		kind:           DataCommitmentRequestType,
//	}
//}
//
//func (at AttestationRequest) IsValsetRequest() bool {
//	return at.kind == ValsetRequestType
//}
//
//func (at AttestationRequest) IsDataCommitmentRequest() bool {
//	return at.kind == DataCommitmentRequestType
//}
//
//func (at AttestationRequest) GetValsetRequest() (*Valset, error) {
//	if at.IsValsetRequest() {
//		return at.valset, nil
//	}
//	return nil, ErrAttestationNotValsetRequest
//}
//
//func (at AttestationRequest) GetDataCommitmentRequest() (*DataCommitment, error) {
//	if at.IsDataCommitmentRequest() {
//		return at.dataCommitment, nil
//	}
//	return nil, ErrAttestationNotDataCommitmentRequest
//}
//
//func (at *AttestationRequest) SetValsetRequest(vs *Valset) error {
//	if vs == nil {
//		return ErrNilValsetRequest
//	}
//	at.kind = ValsetRequestType
//	at.valset = vs
//	return nil
//}
//
//func (at *AttestationRequest) SetDataCommitmentRequest(dc *DataCommitment) error {
//	if dc == nil {
//		return ErrNilDataCommitmentRequest
//	}
//	at.kind = DataCommitmentRequestType
//	at.dataCommitment = dc
//	return nil
//}
//
//func (at AttestationRequest) GetNonce() (uint64, error) {
//	// we assume the AttestationRequest was created correctly and only
//	// is a valset or data commitment.
//	if at.IsValsetRequest() {
//		vs, err := at.GetValsetRequest()
//		if err != nil {
//			return 0, err
//		}
//		return vs.Nonce, nil
//	}
//	dc, err := at.GetDataCommitmentRequest()
//	if err != nil {
//		return 0, err
//	}
//	return dc.Nonce, nil
//}

package types

// NewUpdateMinfeeParamsEvent returns a new EventUpdateMinfeeParams
func NewUpdateMinfeeParamsEvent(authority string, params Params) *EventUpdateMinfeeParams {
	return &EventUpdateMinfeeParams{
		Signer: authority,
		Params: params,
	}
}

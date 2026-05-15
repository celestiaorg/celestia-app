package types

// NewEventUpdateParams constructs an EventUpdateParams emitted when the
// module's parameters are updated by the authority.
func NewEventUpdateParams(authority string, params Params) *EventUpdateParams {
	return &EventUpdateParams{Authority: authority, Params: params}
}

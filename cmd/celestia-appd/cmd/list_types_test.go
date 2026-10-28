package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestListTypesCmd(t *testing.T) {
	cmd := listTypesCmd()

	// Test that the command can be created
	require.NotNil(t, cmd)
	assert.Equal(t, "list-types", cmd.Use)
	assert.Equal(t, "List all registered SDK messages, events, and proto types", cmd.Short)

	// Test that the command definition is correct
	assert.NotNil(t, cmd.RunE)
	assert.Contains(t, cmd.Long, "Lists all registered protobuf types")
	assert.Contains(t, cmd.Long, "interface registry")
}

func TestIsTypedEvent(t *testing.T) {
	msgPackages := msgProtoPackages([]string{
		"/celestia.blob.v1.MsgPayForBlobs",
		"/cosmos.authz.v1beta1.MsgGrant",
		"/hyperlane.core.v1.MsgCreateMailbox",
		"/ibc.core.client.v1.MsgUpdateClient",
		"not-a-valid-name",
	})
	assert.Equal(t, map[protoreflect.FullName]bool{
		"celestia.blob.v1":     true,
		"cosmos.authz.v1beta1": true,
		"hyperlane.core.v1":    true,
		"ibc.core.client.v1":   true,
	}, msgPackages)

	testCases := []struct {
		name string
		want bool
	}{
		// typed events of wired modules
		{name: "celestia.blob.v1.EventPayForBlobs", want: true},
		{name: "cosmos.authz.v1beta1.EventGrant", want: true},
		{name: "hyperlane.core.v1.EventDispatch", want: true},
		// event in a child package of a package that registers a Msg
		{name: "ibc.core.client.v1.events.EventUpdateClient", want: true},
		// "Event" is not followed by an upper-case letter
		{name: "tendermint.abci.Event", want: false},
		{name: "celestia.blob.v1.Eventual", want: false},
		// name merely contains "Event"
		{name: "cosmos.base.abci.v1beta1.StringEvent", want: false},
		{name: "cosmos.tx.v1beta1.GetTxsEventRequest", want: false},
		{name: "cosmos.distribution.v1beta1.ValidatorSlashEvent", want: false},
		{name: "envoy.config.core.v3.EventServiceConfig", want: false},
		// typed event of a module that is not wired into the app
		{name: "cosmos.group.v1.EventCreateGroup", want: false},
		// sibling package of a wired module
		{name: "celestia.blob.v2.EventPayForBlobs", want: false},
		// not an event at all
		{name: "celestia.blob.v1.MsgPayForBlobs", want: false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isTypedEvent(protoreflect.FullName(tc.name), msgPackages))
		})
	}
}

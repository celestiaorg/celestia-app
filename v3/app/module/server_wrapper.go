package module

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	pbgrpc "github.com/gogo/protobuf/grpc"
	"google.golang.org/grpc"
)

// serverWrapper wraps the pbgrpc.Server for registering a service but includes
// logic to extract all the sdk.Msg types that the service declares in its
// methods and fires a callback to add them to the configurator. This allows us
// to create a map of which messages are accepted across which versions.
type serverWrapper struct {
	addMessages func(msgs []string)
	msgServer   pbgrpc.Server
}

func (s *serverWrapper) RegisterService(sd *grpc.ServiceDesc, v interface{}) {
	msgs := make([]string, len(sd.Methods))
	for idx, method := range sd.Methods {
		// we execute the handler to extract the message type
		_, _ = method.Handler(nil, context.Background(), func(i interface{}) error {
			msg, ok := i.(sdk.Msg)
			if !ok {
				panic(fmt.Errorf("unable to register service method %s/%s: %T does not implement sdk.Msg", sd.ServiceName, method.MethodName, i))
			}
			msgs[idx] = sdk.MsgTypeURL(msg)
			return nil
		}, noopInterceptor)
	}
	s.addMessages(msgs)
	// call the underlying msg server to actually register the grpc server
	s.msgServer.RegisterService(sd, v)
}

func noopInterceptor(_ context.Context, _ interface{}, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (interface{}, error) {
	return nil, nil
}

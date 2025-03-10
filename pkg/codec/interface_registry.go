package codec

import (
	"errors"
	"fmt"
	"reflect"

	v3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	cosmostypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/gogo/protobuf/proto"
)

// VersionedInterfaceRegistry extends the cosmos-sdk interface registry to conditionally
// apply recursion and call limits based on the app version.
type VersionedInterfaceRegistry struct {
	cosmostypes.InterfaceRegistry
	appVersion uint64
}

// sharedCounter is a copy of the private type from cosmos-sdk to track call count
type sharedCounter struct {
	count int
}

// statefulUnpacker is a copy of the private type from cosmos-sdk to handle unpacking
type statefulUnpacker struct {
	registry cosmostypes.InterfaceRegistry
	maxDepth int
	maxCalls *sharedCounter
}

// cloneForRecursion returns a new statefulUnpacker instance with maxDepth reduced by one
func (r statefulUnpacker) cloneForRecursion() *statefulUnpacker {
	return &statefulUnpacker{
		registry: r.registry,
		maxDepth: r.maxDepth - 1,
		maxCalls: r.maxCalls,
	}
}

// NewVersionedInterfaceRegistry creates a new VersionedInterfaceRegistry that wraps
// a cosmos-sdk InterfaceRegistry with version awareness
func NewVersionedInterfaceRegistry(registry cosmostypes.InterfaceRegistry, appVersion uint64) *VersionedInterfaceRegistry {
	return &VersionedInterfaceRegistry{
		InterfaceRegistry: registry,
		appVersion:        appVersion,
	}
}

// UnpackAny overrides the cosmos-sdk UnpackAny method to conditionally apply recursion
// and call limits based on the app version. Recursion limits are only applied for v3+.
func (r *VersionedInterfaceRegistry) UnpackAny(any *cosmostypes.Any, iface interface{}) error {
	// For v3+, apply the recursion depth and call limits
	if r.appVersion >= v3.Version {
		// Use the standard implementation with limits
		return r.InterfaceRegistry.UnpackAny(any, iface)
	}

	// For v1 and v2, we'll implement a version without the recursion depth and call limits
	// This is similar to the cosmos-sdk implementation but without the constraints

	// handle nil case
	if any == nil {
		return nil
	}

	if any.TypeUrl == "" {
		// if TypeUrl is empty return nil because without it we can't actually unpack anything
		return nil
	}

	rv := reflect.ValueOf(iface)
	if rv.Kind() != reflect.Ptr {
		return errors.New("UnpackAny expects a pointer")
	}

	rt := rv.Elem().Type()

	cachedValue := any.GetCachedValue()
	if cachedValue != nil {
		if reflect.TypeOf(cachedValue).AssignableTo(rt) {
			rv.Elem().Set(reflect.ValueOf(cachedValue))
			return nil
		}
	}

	// Get the concrete type from the registry
	impl, err := r.resolveTypeURL(any.TypeUrl, rt)
	if err != nil {
		return err
	}

	// Unmarshal the protobuf data into the concrete type
	err = proto.Unmarshal(any.Value, impl)
	if err != nil {
		return err
	}

	// Recursively unpack any interfaces in the message
	// but without depth limits for v1 and v2
	err = cosmostypes.UnpackInterfaces(impl, r)
	if err != nil {
		return err
	}

	// Set the result
	rv.Elem().Set(reflect.ValueOf(impl))

	// Set the cached value if possible
	// Note: This field is unexported in cosmos-sdk, so we can't set it directly
	// We'll rely on the standard unpacking mechanism to handle caching

	return nil
}

// resolveTypeURL resolves the type URL against the expected interface type
func (r *VersionedInterfaceRegistry) resolveTypeURL(typeURL string, expected reflect.Type) (proto.Message, error) {
	// Resolve the type URL to a concrete implementation
	concreteType, err := r.getTypeFromURL(typeURL, expected)
	if err != nil {
		return nil, err
	}

	// Create a new instance of the concrete type
	msg, ok := reflect.New(concreteType.Elem()).Interface().(proto.Message)
	if !ok {
		return nil, fmt.Errorf("can't proto unmarshal %T", concreteType)
	}

	return msg, nil
}

// getTypeFromURL gets the concrete type for a type URL and expected interface
func (r *VersionedInterfaceRegistry) getTypeFromURL(typeURL string, expected reflect.Type) (reflect.Type, error) {
	// This part is implementation specific based on how the registry stores mappings
	// Here we rely on our parent InterfaceRegistry to resolve the type
	msg, err := r.InterfaceRegistry.Resolve(typeURL)
	if err != nil {
		return nil, fmt.Errorf("no concrete type registered for type URL %s against interface %s",
			typeURL, expected.String())
	}

	concreteType := reflect.TypeOf(msg)
	if !concreteType.AssignableTo(expected) {
		return nil, fmt.Errorf("resolved type %s is not assignable to %s",
			concreteType.String(), expected.String())
	}

	return concreteType, nil
}

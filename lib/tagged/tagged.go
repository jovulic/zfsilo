// Package tagged provides a serializable tagged-union implementation.
//
// The usage is very similar to a wrapper. The idea is that you, within your
// app, use an interface-based tagged-union pattern to identify types, and then
// just map to the wrapper type here when marshaling/unmarshaling tagged types.
//
// The behavior revolves around transparently adding a `kind` field on the
// struct to identify the type when marshaling/unmarshaling.
package tagged

import (
	"encoding/json"
	"fmt"
	"reflect"
)

const kindFieldName = "kind"

func NewCodec[T any]() *Codec[T] {
	var t T
	typ := reflect.TypeOf(&t).Elem()
	if typ != nil && typ.Kind() != reflect.Interface {
		panic("tagged: generic type T must be an interface")
	}
	return &Codec[T]{
		kindToType: make(map[string]reflect.Type),
		typeToKind: make(map[reflect.Type]string),
	}
}

// Codec handles the mapping between tag and type for registered tagged-union
// interfaces.
type Codec[T any] struct {
	kindToType map[string]reflect.Type
	typeToKind map[reflect.Type]string
}

func (c *Codec[T]) Register(kind string, value T) {
	typ := reflect.TypeOf(value)
	if _, exists := c.typeToKind[typ]; exists {
		message := fmt.Sprintf("tagged: type '%v' is already registered", typ)
		panic(message)
	}
	if _, exists := c.kindToType[kind]; exists {
		message := fmt.Sprintf("tagged: kind '%s' is already registered", kind)
		panic(message)
	}
	c.kindToType[kind] = typ
	c.typeToKind[typ] = kind
}

func (c *Codec[T]) Wrap(value T) *Union[T] {
	return &Union[T]{Value: value, codec: c}
}

func NewUnion[T any](codec *Codec[T]) *Union[T] {
	var t T
	return &Union[T]{
		Value: t,
		codec: codec,
	}
}

// Union wraps a tagged-union interface providing marshal/unmarshal capability.
type Union[T any] struct {
	Value T
	codec *Codec[T]
}

func (u *Union[T]) MarshalJSON() ([]byte, error) {
	valueType := reflect.TypeOf(u.Value)
	kind, ok := u.codec.typeToKind[valueType]
	if !ok {
		return nil, fmt.Errorf("type '%v' is not registered", valueType)
	}

	// We marshal and then unmarshal the type in order to get a generic struct of
	// the object in order to add the extra kind field.
	//
	// TODO: There is likely a nicer way to do this rather than marshaling just
	// to unmarshal to a generic struct type.
	rawValue, err := json.Marshal(u.Value)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal underlying value: %w", err)
	}
	var rawValueMap map[string]any
	if err := json.Unmarshal(rawValue, &rawValueMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal raw value: %w", err)
	}

	// We check if the property already has the special `kind`, and fail it if does.
	if _, exists := rawValueMap[kindFieldName]; exists {
		return nil, fmt.Errorf("field '%s' already exists on type '%v'", kindFieldName, valueType)
	}
	rawValueMap[kindFieldName] = kind

	return json.Marshal(rawValueMap)
}

func (u *Union[T]) UnmarshalJSON(data []byte) error {
	var kindExtractor struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &kindExtractor); err != nil {
		return fmt.Errorf("failed to extract '%s' field", kindFieldName)
	}
	if kindExtractor.Kind == "" {
		return fmt.Errorf("data missing '%s' field", kindFieldName)
	}

	concreteType, ok := u.codec.kindToType[kindExtractor.Kind]
	if !ok {
		return fmt.Errorf("unregistered kind '%s'", kindExtractor.Kind)
	}

	valuePtr := reflect.New(concreteType)
	if err := json.Unmarshal(data, valuePtr.Interface()); err != nil {
		return fmt.Errorf("failed to unmarshal into '%v': %w", concreteType, err)
	}

	// We also check that the resulting value satisfies the interface T.
	result, ok := valuePtr.Elem().Interface().(T)
	if !ok {
		var zero T
		return fmt.Errorf("type '%v' does not satisfies interface '%T'", concreteType, zero)
	}

	u.Value = result

	return nil
}

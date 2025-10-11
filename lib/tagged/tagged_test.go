package tagged_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jovulic/zfsilo/lib/tagged"
)

type Animal interface {
	Speak() string
}

type Dog struct {
	Name  string `json:"name"`
	Breed string `json:"breed"`
}

func (d *Dog) Speak() string {
	return "Woof!"
}

type Cat struct {
	Name  string `json:"name"`
	Claws bool   `json:"claws"`
}

func (c *Cat) Speak() string {
	return "Meow!"
}

type Bird struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

func (b *Bird) Speak() string {
	return "Tweet!"
}

func TestNewCodec(t *testing.T) {
	t.Run("should panic for non-interface type", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("NewCodec did not panic for a non-interface type")
			}
		}()
		// This should panic because Dog is a struct, not an interface.
		tagged.NewCodec[*Dog]()
	})

	t.Run("should not panic for interface type", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("NewCodec panicked for an interface type: %v", r)
			}
		}()
		// This should be successful.
		tagged.NewCodec[Animal]()
	})
}

func TestRegister(t *testing.T) {
	codec := tagged.NewCodec[Animal]()

	t.Run("should panic on duplicate kind", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("Register did not panic for duplicate kind")
			}
		}()
		codec.Register("dog", &Dog{})
		codec.Register("dog", &Cat{}) // "dog" is already registered
	})

	t.Run("should panic on duplicate type", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("Register did not panic for duplicate type")
			}
		}()
		codec.Register("dog", &Dog{})
		codec.Register("another_dog", &Dog{}) // *Dog is already registered
	})
}

func TestMarshalJSON(t *testing.T) {
	codec := tagged.NewCodec[Animal]()
	codec.Register("dog", &Dog{})
	codec.Register("cat", &Cat{})

	t.Run("should marshal correctly", func(t *testing.T) {
		dog := &Dog{Name: "Buddy", Breed: "Golden Retriever"}
		wrapped := codec.Wrap(dog)

		data, err := json.Marshal(wrapped)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var result map[string]any
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("failed to unmarshal for verification: %v", err)
		}

		if result["kind"] != "dog" {
			t.Errorf("expected kind to be 'dog', got '%v'", result["kind"])
		}
		if result["name"] != "Buddy" {
			t.Errorf("expected name to be 'Buddy', got '%v'", result["name"])
		}
	})

	t.Run("should error for unregistered type", func(t *testing.T) {
		// Create a new codec without registering Cat
		unregisteredCodec := tagged.NewCodec[Animal]()
		cat := &Cat{Name: "Whiskers", Claws: true}
		wrapped := unregisteredCodec.Wrap(cat)

		_, err := json.Marshal(wrapped)
		if err == nil {
			t.Error("expected an error for unregistered type, but got nil")
		}
	})

	t.Run("should error if 'kind' field already exists", func(t *testing.T) {
		codecWithBird := tagged.NewCodec[Animal]()
		codecWithBird.Register("bird", &Bird{})
		bird := &Bird{Name: "Polly", Kind: "Parrot"}
		wrapped := codecWithBird.Wrap(bird)

		_, err := json.Marshal(wrapped)
		if err == nil {
			t.Error("expected an error when 'kind' field exists, but got nil")
		}
	})
}

func TestUnmarshalJSON(t *testing.T) {
	codec := tagged.NewCodec[Animal]()
	codec.Register("dog", &Dog{})
	codec.Register("cat", &Cat{})

	t.Run("should unmarshal dog correctly", func(t *testing.T) {
		jsonData := `{"kind": "dog", "name": "Rex", "breed": "German Shepherd"}`
		wrapped := tagged.NewUnion(codec)

		if err := json.Unmarshal([]byte(jsonData), &wrapped); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		dog, ok := wrapped.Value.(*Dog)
		if !ok {
			t.Fatal("unmarshaled type is not *Dog")
		}
		if dog.Name != "Rex" {
			t.Errorf("expected name to be 'Rex', got '%s'", dog.Name)
		}
		if dog.Speak() != "Woof!" {
			t.Error("method call on unmarshaled type failed")
		}
	})

	t.Run("should unmarshal cat correctly", func(t *testing.T) {
		jsonData := `{"kind": "cat", "name": "Misty", "claws": false}`
		wrapped := tagged.NewUnion(codec)

		if err := json.Unmarshal([]byte(jsonData), &wrapped); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		cat, ok := wrapped.Value.(*Cat)
		if !ok {
			t.Fatal("unmarshaled type is not *Cat")
		}
		if cat.Name != "Misty" {
			t.Errorf("expected name to be 'Misty', got '%s'", cat.Name)
		}
		if cat.Claws {
			t.Error("expected claws to be false, got true")
		}
	})

	t.Run("should error for missing kind field", func(t *testing.T) {
		jsonData := `{"name": "Mystery"}`
		wrapped := tagged.NewUnion(codec)

		err := json.Unmarshal([]byte(jsonData), &wrapped)
		if err == nil {
			t.Error("expected an error for missing 'kind' field, but got nil")
		}
	})

	t.Run("should error for unregistered kind", func(t *testing.T) {
		jsonData := `{"kind": "fish", "name": "Nemo"}`
		wrapped := tagged.NewUnion(codec)

		err := json.Unmarshal([]byte(jsonData), &wrapped)
		if err == nil {
			t.Error("expected an error for unregistered kind, but got nil")
		}
	})
}

func TestMarshalUnmarshal(t *testing.T) {
	codec := tagged.NewCodec[Animal]()
	codec.Register("dog", &Dog{})
	codec.Register("cat", &Cat{})

	t.Run("dog roundtrip", func(t *testing.T) {
		originalDog := &Dog{Name: "Fido", Breed: "Poodle"}
		wrapped := codec.Wrap(originalDog)

		// Marshal.
		data, err := json.Marshal(wrapped)
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}

		// Unmarshal.
		newWrapped := tagged.NewUnion(codec)
		if err := json.Unmarshal(data, &newWrapped); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}

		// Verify.
		newDog, ok := newWrapped.Value.(*Dog)
		if !ok {
			t.Fatal("unmarshaled value is not of type *Dog")
		}
		if !reflect.DeepEqual(originalDog, newDog) {
			t.Errorf("objects are not equal. got %+v, want %+v", newDog, originalDog)
		}
	})
}

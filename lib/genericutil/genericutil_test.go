package genericutil

import (
	"errors"
	"testing"
)

func TestMust(t *testing.T) {
	t.Run("no error", func(t *testing.T) {
		val := Must(10, nil)
		if val != 10 {
			t.Errorf("Must() got %v, want %v", val, 10)
		}
	})

	t.Run("with error", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("Must() did not panic with error")
			}
		}()
		Must(0, errors.New("test error"))
	})
}

func TestFirst(t *testing.T) {
	t.Run("returns first argument", func(t *testing.T) {
		val := First(1, "two", 3.0)
		if val != 1 {
			t.Errorf("First() got %v, want %v", val, 1)
		}
	})

	t.Run("returns first argument of different type", func(t *testing.T) {
		val := First("hello", 123, true)
		if val != "hello" {
			t.Errorf("First() got %v, want %v", val, "hello")
		}
	})
}

func TestSecond(t *testing.T) {
	t.Run("returns second argument", func(t *testing.T) {
		val := Second(1, "two", 3.0)
		if val != "two" {
			t.Errorf("Second() got %v, want %v", val, "two")
		}
	})

	t.Run("returns second argument of different type", func(t *testing.T) {
		val := Second(true, 123, "hello")
		if val != 123 {
			t.Errorf("Second() got %v, want %v", val, 123)
		}
	})
}

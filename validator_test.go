package httpx

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- Structs ---

type ReflectStruct struct {
	Val string `validate:"required"`
}

type InterfaceStruct struct {
	Val string
}

func (i *InterfaceStruct) Validate(ctx context.Context) error {
	if i.Val == "bad" {
		return errors.New("interface error")
	}
	return nil
}

type BothStruct struct {
	Val string `validate:"required"`
}

func (b *BothStruct) Validate(ctx context.Context) error {
	// This should take precedence over tag
	return errors.New("interface precedence")
}

// --- Tests ---

func TestValidate(t *testing.T) {
	ctx := context.Background()

	t.Run("Reflection Valid", func(t *testing.T) {
		v := &ReflectStruct{Val: "ok"}
		assert.NoError(t, Validate(ctx, v))
	})

	t.Run("Reflection Invalid", func(t *testing.T) {
		v := &ReflectStruct{Val: ""}
		assert.Error(t, Validate(ctx, v))
	})

	t.Run("Interface Valid", func(t *testing.T) {
		v := &InterfaceStruct{Val: "good"}
		assert.NoError(t, Validate(ctx, v))
	})

	t.Run("Interface Invalid", func(t *testing.T) {
		v := &InterfaceStruct{Val: "bad"}
		err := Validate(ctx, v)
		assert.Error(t, err)
		assert.Equal(t, "interface error", err.Error())
	})

	t.Run("Precedence (Interface > Reflection)", func(t *testing.T) {
		// Even though Val is present (satisfies tag), Validate() returns error
		v := &BothStruct{Val: "ok"}
		err := Validate(ctx, v)
		assert.Error(t, err)
		assert.Equal(t, "interface precedence", err.Error())
	})
}

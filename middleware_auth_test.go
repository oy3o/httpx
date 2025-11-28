package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAuthBearer(t *testing.T) {
	validator := func(ctx context.Context, token string) (any, error) {
		if token == "valid" {
			return "user1", nil
		}
		return nil, errors.New("bad token")
	}

	mw := AuthBearer(validator, "TestRealm")
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Context().Value(IdentityKey)
		assert.Equal(t, "user1", id)
		w.WriteHeader(200)
	}))

	t.Run("Success Header", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Bearer valid")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		assert.Equal(t, 200, w.Code)
	})

	t.Run("Success Query Param", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/?access_token=valid", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		assert.Equal(t, 200, w.Code)
	})

	t.Run("Missing Token", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		assert.Equal(t, 401, w.Code)
		assert.Contains(t, w.Header().Get("WWW-Authenticate"), `Bearer realm="TestRealm"`)
	})

	t.Run("Invalid Token", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Bearer invalid")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		assert.Equal(t, 401, w.Code)
		assert.Contains(t, w.Body.String(), "invalid token")
		assert.Contains(t, w.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
	})

	t.Run("Malformed Header", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Basic 123") // Wrong prefix
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		assert.Equal(t, 401, w.Code)
	})
}

func TestAuthBasic(t *testing.T) {
	validator := func(ctx context.Context, u, p string) (any, error) {
		if u == "admin" && p == "123" {
			return "admin", nil
		}
		return nil, errors.New("bad creds")
	}

	mw := AuthBasic(validator, "") // Default realm
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	t.Run("Success", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.SetBasicAuth("admin", "123")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		assert.Equal(t, 200, w.Code)
	})

	t.Run("Fail", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.SetBasicAuth("admin", "wrong")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		assert.Equal(t, 401, w.Code)
		assert.Contains(t, w.Header().Get("WWW-Authenticate"), `Basic realm="Restricted"`)
	})
}

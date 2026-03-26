package httpx

import (
	"errors"
	"net/http/httptest"
	"testing"
)

func TestErrorHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	Error(w, req, errors.New("boom"))

	if w.Header().Get("Content-Type") == "" {
		t.Errorf("Content-Type is empty!")
	}

	// Check the actual recorded headers
	if w.Result().Header.Get("Content-Type") == "" {
		t.Errorf("Result Content-Type is empty! Headers were not sent.")
	}
}

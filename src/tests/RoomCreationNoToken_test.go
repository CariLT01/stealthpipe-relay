package tests

import (
	"net/http/httptest"
	"testing"

	"github.com/CariLT01/stealthPipeGoRelay/src/core"
)

func TestRoomCreationNoToken(t *testing.T) {
	srv := core.NewServer(true, core.NewEmptyExtraConfig())

	req := httptest.NewRequest("GET", "/create", nil)
	w := httptest.NewRecorder()

	srv.ServeHTTP(w, req)

	if w.Code == 200 {
		t.Errorf("Expected status code not 200, got %d", w.Code)
	}

}

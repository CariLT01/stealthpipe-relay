package tests

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/CariLT01/stealthPipeGoRelay/src/core"
)

func TestRoomCreationFull(t *testing.T) {
	app := core.NewServer(true, core.NewEmptyExtraConfig())

	powReq := httptest.NewRequest("GET", "/pow", nil)
	powW := httptest.NewRecorder()

	app.ServeHTTP(powW, powReq)

	var proofOfWorkResponse ProofOfWorkResponse
	if err := json.NewDecoder(powW.Body).Decode(&proofOfWorkResponse); err != nil {
		t.Error("An error occurred while decoding proof of work response", "error", err)
	}

	if proofOfWorkResponse.Token == "" {
		t.Error("Proof of work token is empty")
	}

	// parse JWT

	payload := DecodeJwtPayload(proofOfWorkResponse.Token, t)

	nonce := SolveProofOfWork(proofOfWorkResponse.Salt, payload.Diff)

	// create room
	createReq := httptest.NewRequest("GET", "/create?token="+proofOfWorkResponse.Token+"&nonce="+strconv.Itoa(nonce), nil)
	createW := httptest.NewRecorder()

	app.CallHttp(createW, createReq)
	if createW.Code != 200 {
		t.Errorf("Expected status code 200, got %d", createW.Code)
	}
}

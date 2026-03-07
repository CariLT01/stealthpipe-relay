package tests

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/CariLT01/stealthPipeGoRelay/src/core"
)

func TestProofOfWork(t *testing.T) {

	app := core.NewServer(true, core.NewEmptyExtraConfig())

	req := httptest.NewRequest("GET", "/pow", nil)
	w := httptest.NewRecorder()

	app.ServeHTTP(w, req)

	var proofOfWorkResponse ProofOfWorkResponse
	if err := json.NewDecoder(w.Body).Decode(&proofOfWorkResponse); err != nil {
		t.Error("An error occurred while decoding proof of work response", "error", err)
	}

	if proofOfWorkResponse.Token == "" {
		t.Error("Proof of work token is empty")
	}

	// parse JWT

	payload := DecodeJwtPayload(proofOfWorkResponse.Token, t)

	nonce := SolveProofOfWork(proofOfWorkResponse.Salt, payload.Diff)

	valid := core.IsProofOfWorkValid(app, proofOfWorkResponse.Token, int64(nonce))

	if !valid {
		t.Error("Proof of work is not valid")
	}

}

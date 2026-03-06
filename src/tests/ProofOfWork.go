package tests

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func checkProofOfWork(salt string, nonce int, difficulty int) bool {

	hash := sha256.Sum256([]byte(salt + strconv.Itoa(nonce)))

	fullBytesCheck := difficulty / 2
	for i := 0; i < fullBytesCheck; i++ {
		if hash[i] != 0 {
			return false
		}
	}

	if difficulty%2 != 0 {
		return (hash[fullBytesCheck] & 0xFF) <= 0x0F
	}

	return true
}

func SolveProofOfWork(salt string, difficulty int) int {
	nonce := 0

	for {
		valid := checkProofOfWork(salt, nonce, difficulty)

		if valid {
			break
		}
		nonce++
	}

	return nonce
}

type TokenPayload struct {
	Salt string `json:"salt"`
	Diff int    `json:"diff"`
	Exp  int    `json:"exp"`
}

type ProofOfWorkResponse struct {
	Ok    bool   `json:"ok"`
	Token string `json:"token"`
	Salt  string `json:"salt"`
}

func DecodeJwtPayload(token string, t *testing.T) TokenPayload {
	parts := strings.Split(token, ".")

	if len(parts) != 3 {
		t.Error("JWT token does not have 3 parts", "token", token)
		return TokenPayload{}
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Error("JWT token base64 decode failed", "error", err)
		return TokenPayload{}
	}
	var decodedTokenPayload TokenPayload

	err = json.Unmarshal(payload, &decodedTokenPayload)
	if err != nil {
		t.Error("JWT token unmarshal failed", "error", err)
		return TokenPayload{}
	}

	return decodedTokenPayload
}

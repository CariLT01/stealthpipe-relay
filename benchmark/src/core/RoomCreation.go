package core

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/CariLT01/stealthpipe-relay-benchmarking/src/sec"
)

type ProofOfWorkResponse struct {
	Ok    bool   `json:"ok"`
	Token string `json:"token"`
	Salt  string `json:"salt"`
}

type TokenPayload struct {
	Salt string `json:"salt"`
	Diff int    `json:"diff"`
	Exp  int    `json:"exp"`
}

type RoomCreationResponse struct {
	Ok         bool   `json:"ok"`
	Message    string `json:"message"`
	ReuseToken string `json:"reuseToken"`
}

func decodeJwtPayload(benchmarker *Benchmarker, token string) TokenPayload {
	parts := strings.Split(token, ".")

	if len(parts) != 3 {
		benchmarker.Logger.Error("JWT token does not have 3 parts", "token", token)
		return TokenPayload{}
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		benchmarker.Logger.Error("JWT token base64 decode failed", "error", err)
		return TokenPayload{}
	}
	var decodedTokenPayload TokenPayload

	err = json.Unmarshal(payload, &decodedTokenPayload)
	if err != nil {
		benchmarker.Logger.Error("JWT token unmarshal failed", "error", err)
		return TokenPayload{}
	}

	return decodedTokenPayload
}

func CreateRoomAndGetCode(benchmarker *Benchmarker, powToken string, nonce int) string {
	res, err := http.Get(benchmarker.RelayURL + "/create?token=" + powToken + "&nonce=" + strconv.Itoa(nonce))

	if err != nil {
		benchmarker.Logger.Error("An error occurred while creating a room", "error", err)
		return ""
	}

	defer res.Body.Close()

	var createRoomResponse RoomCreationResponse

	if err := json.NewDecoder(res.Body).Decode(&createRoomResponse); err != nil {
		benchmarker.Logger.Error("An error occurred while decoding room creation response", "error", err)
		return ""
	}

	return createRoomResponse.Message
}

func (benchmarker *Benchmarker) CreateRoom() string {
	res, err := http.Get(benchmarker.RelayURL + "/pow")

	var proofOfWorkResponse ProofOfWorkResponse

	if err != nil {
		benchmarker.Logger.Error("An error occurred while fetching proof of work", "error", err)
		return ""
	}

	defer res.Body.Close()

	if err := json.NewDecoder(res.Body).Decode(&proofOfWorkResponse); err != nil {
		benchmarker.Logger.Error("An error occurred while decoding proof of work response", "error", err)
		return ""
	}

	if proofOfWorkResponse.Token == "" {
		benchmarker.Logger.Error("Proof of work token is empty")
		return ""
	}

	tokenPayload := decodeJwtPayload(benchmarker, proofOfWorkResponse.Token)

	nonce := sec.SolveProofOfWork(proofOfWorkResponse.Salt, tokenPayload.Diff)

	roomCode := CreateRoomAndGetCode(benchmarker, proofOfWorkResponse.Token, nonce)

	if roomCode == "" {
		benchmarker.Logger.Error("Failed to create room")
		return ""
	}

	benchmarker.RoomsMu.Lock()
	benchmarker.Logger.Info("Created room", "roomCode", roomCode)
	benchmarker.Rooms[roomCode] = NewRoom(roomCode)
	benchmarker.RoomsMu.Unlock()

	return roomCode
}

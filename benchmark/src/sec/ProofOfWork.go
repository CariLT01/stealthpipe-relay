package sec

import (
	"crypto/sha256"
	"strconv"
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

package core

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

func generateRandomId() string {

	n, _ := rand.Int(rand.Reader, big.NewInt(1000000))

	// Format with leading zeros to ensure it is always 6 digits
	str := fmt.Sprintf("%06d", n)

	return str

}

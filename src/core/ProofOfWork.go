package core

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"math/big"

	"github.com/golang-jwt/jwt/v5"
)

type MyClaims struct {
	Salt                 string `json:"salt"`
	Diff                 int    `json:"diff"`
	jwt.RegisteredClaims        // This adds 'exp', 'iat', etc.
}

type ReuseJwtClaims struct {
	Code string `json:"code"`
	jwt.RegisteredClaims
}

func isReplay(app *ServerData, salt string, expiry int64) bool {
	app.UsedSaltsMutex.Lock()
	defer app.UsedSaltsMutex.Unlock()

	// 1. Check if it's already there
	if _, exists := app.UsedSalts[salt]; exists {
		return true
	}

	// 2. If not, add it with the expiry time from the JWT
	app.UsedSalts[salt] = expiry
	return false
}

func StartCleanupLoop(app *ServerData) {
	ticker := time.NewTicker(1 * time.Minute) // Check every minute
	go func() {
		for range ticker.C {
			now := time.Now().Unix()
			app.UsedSaltsMutex.Lock()
			for salt, expiry := range app.UsedSalts {
				if now > expiry {
					delete(app.UsedSalts, salt)
				}
			}
			app.UsedSaltsMutex.Unlock()
		}
	}()
}

func verifyAndGetPayload(tokenString string, secret []byte) (*MyClaims, error) {
	// 1. Parse the token
	token, err := jwt.ParseWithClaims(tokenString, &MyClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate the algorithm is what you expect
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	})

	// Check for errors (like expired or tampered tokens)
	if err != nil {
		return nil, err
	}

	// Extract the claims and check validity
	if claims, ok := token.Claims.(*MyClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

func verifyAndGetPayloadReuseToken(tokenString string, secret []byte) (*ReuseJwtClaims, error) {
	// 1. Parse the token
	token, err := jwt.ParseWithClaims(tokenString, &ReuseJwtClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate the algorithm is what you expect
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	})

	// Check for errors (like expired or tampered tokens)
	if err != nil {
		return nil, err
	}

	// Extract the claims and check validity
	if claims, ok := token.Claims.(*ReuseJwtClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

func IsProofOfWorkValid(app *ServerData, token string, nonce int64) bool {
	secretKey := []byte(os.Getenv("SECRET_KEY"))

	claims, err := verifyAndGetPayload(token, secretKey)

	if err != nil {
		app.Logger.Error("Invalid proof of work token: ", "error", err.Error())
		return false
	}

	if isReplay(app, claims.Salt, claims.RegisteredClaims.ExpiresAt.Time.Unix()) {
		app.Logger.Error("Invalid proof of work token: replay attack")
		return false
	}

	hashedInt := fmt.Sprintf("%s%d", claims.Salt, nonce)

	hash := sha256.Sum256([]byte(hashedInt))
	hashString := hex.EncodeToString(hash[:])

	prefix := strings.Repeat("0", claims.Diff)

	isValid := strings.HasPrefix(hashString, prefix)

	if !isValid {
		app.Logger.Error("Proof of work invalid: hash does not start with prefix")
	}

	return isValid

}

func IsReuseTokenValid(app *ServerData, token string) string {
	secretKey := []byte(os.Getenv("SECRET_KEY"))

	claims, err := verifyAndGetPayloadReuseToken(token, secretKey)

	if err != nil {
		app.Logger.Error("Invalid token")
		return ""
	}
	return claims.Code

}

func generateProofOfWork(app *ServerData) (string, string) {
	secretKey := []byte(os.Getenv("SECRET_KEY"))

	timeSalt := time.Now().UnixMilli()
	n, err := rand.Int(rand.Reader, big.NewInt(100000))
	if err != nil {
		panic(err)
	}

	salt := fmt.Sprintf("%d-%d", timeSalt, n.Int64())

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"salt": salt,
		"diff": app.CurrentDifficulty,
		"exp":  jwt.NewNumericDate(time.Now().Add(time.Minute * 5)),
	})

	signedToken, err := token.SignedString(secretKey)

	if err != nil {
		panic(err)
	}

	return signedToken, salt

}

func generateReuseToken(code string) string {
	secretKey := []byte(os.Getenv("SECRET_KEY"))

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"code": code,
		"exp":  jwt.NewNumericDate(time.Now().Add(time.Hour * 3)),
	})

	signedToken, err := token.SignedString(secretKey)

	if err != nil {
		panic(err)
	}

	return signedToken
}

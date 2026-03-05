package core

import (
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func GenerateReuseToken(app *ServerData, code string) string {
	secretKey := []byte(os.Getenv("SECRET_KEY"))

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"code": code,
		"exp":  jwt.NewNumericDate(time.Now().Add(time.Hour * time.Duration(app.Config.reuseTokenExpiryHours))),
	})

	signedToken, err := token.SignedString(secretKey)

	if err != nil {
		panic(err)
	}

	return signedToken
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

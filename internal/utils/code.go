package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

const bookingAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func GenerateBookingCode() (string, error) {
	value, err := RandomString(10, bookingAlphabet)
	if err != nil {
		return "", err
	}
	return "BK-" + value, nil
}

func RandomString(length int, alphabet string) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("length must be positive")
	}
	out := make([]byte, length)
	max := big.NewInt(int64(len(alphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out), nil
}

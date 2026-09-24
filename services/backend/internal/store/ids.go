package store

import (
	"crypto/rand"
	"encoding/hex"
)

func NewID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		panic("cryptographic random source unavailable: " + err.Error())
	}
	return hex.EncodeToString(value)
}

package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

func newDurableIncidentID(fingerprint string, now time.Time) string {
	entropy := make([]byte, 8)
	if _, err := rand.Read(entropy); err != nil {
		entropy = []byte(fmt.Sprintf("%d", now.UnixNano()))
	}
	hash := sha256.Sum256(append([]byte(fingerprint+"|"+now.UTC().Format(time.RFC3339Nano)+"|"), entropy...))
	return "INC-" + hex.EncodeToString(hash[:])[:12]
}

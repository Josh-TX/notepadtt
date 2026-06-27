package backend

import "math/rand/v2"

const alphanumChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func uniqueId(length int) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = alphanumChars[rand.IntN(62)]
	}
	return string(b)
}

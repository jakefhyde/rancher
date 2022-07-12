package namegenerator

import (
	"math/rand"
	"time"
)

const lowerLetterBytes = "abcdefghijklmnopqrstuvwxyz"
const upperLetterBytes = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
const numberBytes = "0123456789"

func init() {
	rand.Seed(time.Now().UnixNano())
}

// RandStringWithCharset returns a random string with specifc characters from the `charset` parameter
// with the length depending on `n`. Used for creating a random string for resource names, such as clusters.
func RandStringWithCharset(length int, charset string) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// RandStringLower returns a random string with lower case alpha
// chars with the length depending on `n`. Used for creating a random string for resource names, such as clusters.
func RandStringLower(n int) string {
	return RandStringWithCharset(n, lowerLetterBytes)
}

func RandStringAll(length int) string {
	return RandStringWithCharset(length, lowerLetterBytes+upperLetterBytes+numberBytes)
}

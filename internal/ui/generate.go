package ui

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"runtime"
)

// passwordAlphabet is the character set used by GeneratePassword. Ambiguous
// glyphs (Il1O0) are left out to keep generated passwords easy to retype.
const (
	lowerChars  = "abcdefghijkmnopqrstuvwxyz"
	upperChars  = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	digitChars  = "23456789"
	symbolChars = "!@#$%^&*()-_=+[]{}:,.?"
)

// GeneratePassword returns a random password of the requested length drawn
// from letters, digits and symbols, always containing at least one of each
// class.
func GeneratePassword(length int) (string, error) {
	if length < 8 {
		return "", fmt.Errorf("длина пароля должна быть не меньше 8 символов (запрошено %d)", length)
	}
	all := lowerChars + upperChars + digitChars + symbolChars

	out := make([]byte, 0, length)
	for _, class := range []string{lowerChars, upperChars, digitChars, symbolChars} {
		c, err := randomChar(class)
		if err != nil {
			return "", err
		}
		out = append(out, c)
	}
	for len(out) < length {
		c, err := randomChar(all)
		if err != nil {
			return "", err
		}
		out = append(out, c)
	}

	// Shuffle so the guaranteed classes are not always at the front.
	for i := len(out) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		out[i], out[j.Int64()] = out[j.Int64()], out[i]
	}
	return string(out), nil
}

func randomChar(alphabet string) (byte, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
	if err != nil {
		return 0, err
	}
	return alphabet[n.Int64()], nil
}

// isMac reports whether the application runs on macOS.
func isMac() bool {
	return runtime.GOOS == "darwin"
}

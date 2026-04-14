package util

import (
	"math/rand"
	"time"
)

func GenerateRandomLetters(length int) string {
	rand.Seed(time.Now().UnixNano())
	letters := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	var result string
	for i := 0; i < length; i++ {
		result += string(letters[rand.Intn(len(letters))])
	}
	return result
}

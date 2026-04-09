package util

import (
	"math/rand"
	"time"
)

func GenerateRandomLetters(length int) string {
	rand.Seed(time.Now().UnixNano())                                  // 使用当前时间戳作为随机数种子
	letters := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ" // 字母范围（大小写）
	var result string
	for i := 0; i < length; i++ {
		result += string(letters[rand.Intn(len(letters))]) // 随机选择一个字母
	}
	return result
}

package metrics_storage

import (
	"crypto/md5"
	"encoding/hex"
	"os"
)

func CalculateMD5(data []byte) string {
	hash := md5.New()
	hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}

func CalculateFileHash(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return CalculateMD5(data), nil
}

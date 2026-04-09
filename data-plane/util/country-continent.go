package util

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
)

// 全局映射表
var (
	countryToContinent map[string]string
)

// LoadCountryContinent 加载 JSON 文件（全局只加载一次）
func LoadCountryContinent(pre string, logger *slog.Logger) error {
	var loadErr error

	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath) // 程序所在目录
	filePath := filepath.Join(exeDir, "country-continent.json")

	data, err := os.ReadFile(filePath)
	if err != nil {
		loadErr = err
		return loadErr
	}

	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		loadErr = err
		return loadErr
	}

	logger.Info("LoadCountryContinent", slog.String("pre", pre))

	countryToContinent = m

	return loadErr
}

// GetContinentByCountry
// 输入：国家英文名  Malaysia / China / United States
// 输出：Asia / Europe / North America ...
func GetContinentByCountry(country string) string {
	if continent, ok := countryToContinent[country]; ok {
		return continent
	}
	return "Unknown"
}

package util

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
)

var (
	countryToContinent map[string]string
)

func LoadCountryContinent(pre string, logger *slog.Logger) error {
	var loadErr error

	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	filePath := filepath.Join(exeDir, "country-continent.json")

	data, err := os.ReadFile(filePath)
	if err != nil {
		loadErr = err
		return loadErr
	}

	var m map[string]string
	if err = json.Unmarshal(data, &m); err != nil {
		loadErr = err
		return loadErr
	}

	logger.Info("LoadCountryContinent", slog.String("pre", pre))

	countryToContinent = m

	return loadErr
}

func GetContinentByCountry(country string) string {
	if continent, ok := countryToContinent[country]; ok {
		return continent
	}
	return "Unknown"
}

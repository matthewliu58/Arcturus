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

	type CountryEntry struct {
		Name      string `json:"name"`
		Continent string `json:"continent"`
	}
	var raw map[string]CountryEntry
	if err = json.Unmarshal(data, &raw); err != nil {
		loadErr = err
		return loadErr
	}

	m := make(map[string]string, len(raw))
	for code, entry := range raw {
		m[code] = entry.Continent
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

package util

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ip2location/ip2location-go/v9"
)

type ipOverride struct {
	Country   string `json:"country"`
	Continent string `json:"continent"`
	Province  string `json:"province"`
	City      string `json:"city"`
}

var (
	ipDb        *ip2location.DB
	ipOverrides map[string]ipOverride
)

func ipOverrideConfigPath() string {
	exePath, _ := os.Executable()
	return filepath.Join(filepath.Dir(exePath), "ip-override.json")
}

func loadIPOverrides(pre string, logger *slog.Logger) {
	path := ipOverrideConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Info("no ip-override.json found, skipping", slog.String("pre", pre))
			return
		}
		logger.Error("failed to read ip-override.json", slog.String("pre", pre), slog.String("err", err.Error()))
		return
	}

	var raw map[string]ipOverride
	if err := json.Unmarshal(data, &raw); err != nil {
		logger.Error("failed to parse ip-override.json", slog.String("pre", pre), slog.String("err", err.Error()))
		return
	}

	ipOverrides = make(map[string]ipOverride, len(raw))
	for ip, ov := range raw {
		ipOverrides[ip] = ov
	}
	logger.Info("loaded ip overrides", slog.String("pre", pre), slog.Int("count", len(ipOverrides)))
}

func InitIPInfo(pre string, logger *slog.Logger) error {

	logger.Info("InitIPInfo", slog.String("pre", pre))

	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	dbPath := filepath.Join(exeDir, "IP2LOCATION-LITE-DB11.BIN")
	var err error
	ipDb, err = ip2location.OpenDB(dbPath)
	if err != nil {
		logger.Error("InitIPInfo", slog.String("pre", pre), slog.String("err", err.Error()))
	} else {
		logger.Info("InitIPInfo", slog.String("pre", pre), slog.String("dbPath", dbPath))
	}

	loadIPOverrides(pre, logger)

	return nil
}

type IPInfoResult struct {
	IP      string `json:"ip"`
	Country string `json:"country"`
	//CountryCode string `json:"country_code"`
	Continent string `json:"continent"`
	Province  string `json:"province"`
	City      string `json:"city"`
	ISP       string `json:"isp"`
	//Latitude    float64 `json:"latitude"`
	//Longitude   float64 `json:"longitude"`
}

func GetIPInfo(ip string, pre string, logger *slog.Logger) (IPInfoResult, error) {
	if ipDb == nil {
		logger.Error("GetIPInfo", slog.String("pre", pre), slog.String("err", "ipDb is nil"))
		return IPInfoResult{}, errors.New("ipDb is nil")
	}

	res, err := ipDb.Get_all(ip)
	if err != nil {
		return IPInfoResult{}, err
	}

	result := IPInfoResult{
		IP:        ip,
		Country:   res.Country_short,
		Continent: GetContinentByCountry(res.Country_short),
		Province:  res.Region,
		City:      res.City,
	}

	// apply ip-override.json overrides (support IP prefix matching)
	if ov := findIPOverride(ip); ov != nil {
		if ov.Country != "" {
			result.Country = ov.Country
		}
		if ov.Continent != "" {
			result.Continent = ov.Continent
		}
		if ov.Province != "" {
			result.Province = ov.Province
		}
		if ov.City != "" {
			result.City = ov.City
		}
		logger.Info("ip override applied", slog.String("pre", pre), slog.String("ip", ip))
	}

	return result, nil
}

// findIPOverride finds matching IP override by prefix matching
// Supports exact match (e.g., "47.86.234.131") and prefix match (e.g., "47.86")
func findIPOverride(ip string) *ipOverride {
	// First try exact match
	if ov, ok := ipOverrides[ip]; ok {
		return &ov
	}

	// Then try prefix matching (longest prefix first)
	for prefix, ov := range ipOverrides {
		if strings.HasPrefix(ip, prefix) {
			return &ov
		}
	}

	return nil
}

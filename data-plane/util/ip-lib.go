package util

import (
	"errors"
	"github.com/ip2location/ip2location-go/v9"
	"log/slog"
	"os"
	"path/filepath"
)

var (
	ipDb *ip2location.DB
)

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

	return IPInfoResult{
		IP:      ip,
		Country: res.Country_long,
		//CountryCode: res.Country_short,
		Continent: GetContinentByCountry(res.Country_long),
		Province:  res.Region,
		City:      res.City,
		//ISP:       res.Isp,
	}, nil
}

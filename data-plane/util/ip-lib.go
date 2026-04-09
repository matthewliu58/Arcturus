package util

import (
	"errors"
	"github.com/ip2location/ip2location-go/v9"
	"log/slog"
	"os"
	"path/filepath"
)

var (
	ipDb *ip2location.DB // 这里改成 DB 就对了！
)

// InitIPInfo 初始化IP库（程序启动时调用一次）
func InitIPInfo(pre string, logger *slog.Logger) error {

	logger.Info("InitIPInfo", slog.String("pre", pre))

	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath) // 程序所在目录
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

// IPInfoResult 结构化IP信息（给你离线分析用）
type IPInfoResult struct {
	IP          string  `json:"ip"`
	Country     string  `json:"country"`      // 国家
	CountryCode string  `json:"country_code"` // 国家码
	Province    string  `json:"province"`     // 省份
	City        string  `json:"city"`         // 城市
	ISP         string  `json:"isp"`          // 运营商
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

// GetIPInfo 输入IP → 输出地区+运营商信息
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
		IP:          ip,
		Country:     res.Country_long,
		CountryCode: res.Country_short,
		Province:    res.Region,
		City:        res.City,
		ISP:         res.Isp,
	}, nil
}

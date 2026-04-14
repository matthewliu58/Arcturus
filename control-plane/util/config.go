package util

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

var Config_ *Config

type Config struct {
	//Name       string   `yaml:"name"`
	ServerList []string   `yaml:"server_list"`
	ServerIP   string     `yaml:"server_ip"`
	DataDir    string     `yaml:"data_dir"`
	Node       NodeConfig `yaml:"node"`
}

type NodeConfig struct {
	Provider  string `yaml:"provider"` // gcp | aws | azure | vultr | digitalocean | onprem
	Continent string `yaml:"continent"`
	Country   string `yaml:"country"`
	City      string `yaml:"city"`
	IP        NodeIP `yaml:"ip"`
}

type NodeIP struct {
	Private string `yaml:"private"`
	Public  string `yaml:"public"`
}

func ReadYamlConfig(logger *slog.Logger) (*Config, error) {

	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("获取程序路径失败: %w", err)
	}
	exeDir := filepath.Dir(exePath)
	configPath := filepath.Join(exeDir, "config.yaml")

	if _, err = os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("配置文件不存在: %s（请确保config.yaml和程序同目录）", configPath)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	} else {
		logger.Info("读取配置文件成功", "path", configPath)
	}

	var config Config
	if err = yaml.Unmarshal(content, &config); err != nil {
		return nil, fmt.Errorf("解析yaml失败: %w", err)
	}

	return &config, nil
}

func GenerateRandomLetters(length int) string {
	rand.Seed(time.Now().UnixNano())
	letters := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	var result string
	for i := 0; i < length; i++ {
		result += string(letters[rand.Intn(len(letters))])
	}
	return result
}

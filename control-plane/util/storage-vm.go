package util

import (
	model "control-plane/receive-info"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	expireTime = 1
)

type Storage interface {
	Save(report *model.VMReport, pre string) (string, error)
	Put(report *model.VMReport, pre string) (string, error)
	Get(vmID, pre string) (*model.VMReport, error)
	GetAll(logPre string) ([]*model.VMReport, error)
	GetRecent(n int, logPre string) ([]*model.VMReport, error)
	Close()
}

type FileStorage struct {
	StorageDir     string
	mu             sync.RWMutex
	cleanupTicker  *time.Ticker
	expireDuration time.Duration
	l              *slog.Logger
}

func NewFileStorage(storageDir string, expireMinutes int, pre string, l *slog.Logger) (*FileStorage, error) {

	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("创建存储目录失败: %w", err)
	}

	expireDur := expireTime * time.Minute
	if expireMinutes > 0 {
		expireDur = time.Duration(expireMinutes) * time.Minute
	}

	fs := &FileStorage{
		StorageDir:     storageDir,
		expireDuration: expireDur,
		cleanupTicker:  time.NewTicker(1 * time.Minute),
		l:              l,
	}

	go fs.startCleanupWorker(pre)

	return fs, nil
}

func (fs *FileStorage) Put(report *model.VMReport, pre string) (string, error) {

	if report == nil || report.VMID == "" {
		return "", errors.New("VMReport不能为空且VMID必须非空")
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	fileName := fmt.Sprintf("%s_%s.json", report.VMID, timestamp)
	filePath := filepath.Join(fs.StorageDir, fileName)
	tmpFilePath := fmt.Sprintf("%s.tmp_%d", filePath, time.Now().UnixNano())

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("JSON序列化失败: %w", err)
	}
	fs.l.Info("put file data", slog.String("pre", pre), slog.String("data", string(data)))

	if err = os.WriteFile(tmpFilePath, data, 0644); err != nil {
		return "", fmt.Errorf("写入临时文件失败: %w", err)
	}

	if err = os.Rename(tmpFilePath, filePath); err != nil {
		_ = os.Remove(tmpFilePath) // 清理临时文件
		return "", fmt.Errorf("重命名文件失败: %w", err)
	}

	return report.ReportID, nil
}

func (fs *FileStorage) Get(vmID, pre string) (*model.VMReport, error) {
	if vmID == "" {
		return nil, errors.New("VMID不能为空")
	}

	fs.mu.RLock()
	defer fs.mu.RUnlock()

	files, err := os.ReadDir(fs.StorageDir)
	if err != nil {
		return nil, fmt.Errorf("读取存储目录失败: %w", err)
	}

	var latestFile os.DirEntry
	var latestTimestamp int64 = -1

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		name := file.Name()

		prefix := fmt.Sprintf("%s_", vmID)
		if !filepath.HasPrefix(name, prefix) {
			continue
		}

		timestampStr := name[len(prefix) : len(name)-5]
		timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
		if err != nil {
			continue
		}

		if timestamp > latestTimestamp {
			latestTimestamp = timestamp
			latestFile = file
		}
	}

	if latestFile == nil {
		return nil, fmt.Errorf("VM[%s]的上报文件不存在", vmID)
	}

	filePath := filepath.Join(fs.StorageDir, latestFile.Name())
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	var report model.VMReport
	if err = json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("JSON反序列化失败: %w", err)
	}

	return &report, nil
}

func (fs *FileStorage) Save(report *model.VMReport, pre string) (string, error) {
	return fs.Put(report, pre)
}

func (fs *FileStorage) Close() {
	if fs.cleanupTicker != nil {
		fs.cleanupTicker.Stop()
	}
}

func (fs *FileStorage) startCleanupWorker(pre string) {
	defer fs.cleanupTicker.Stop()

	for range fs.cleanupTicker.C {
		if err := fs.cleanupExpiredFiles(pre); err != nil {
			fs.l.Error("清理过期文件失败", slog.String("pre", pre), slog.Any("err", err))
		}
	}
}

func (fs *FileStorage) cleanupExpiredFiles(pre string) error {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	files, err := os.ReadDir(fs.StorageDir)
	if err != nil {
		return fmt.Errorf("读取目录失败: %w", err)
	}

	expireTime_ := time.Now().Add(-fs.expireDuration)

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileInfo, err := file.Info()
		if err != nil {
			fs.l.Error("获取文件信息失败", slog.String("pre", pre),
				slog.String("fileName", file.Name()), slog.Any("err", err))
			continue
		}

		if fileInfo.ModTime().Before(expireTime_) {
			filePath := filepath.Join(fs.StorageDir, file.Name())
			if err := os.Remove(filePath); err != nil {
				fs.l.Error("删除过期文件失败", slog.String("pre", pre),
					slog.String("filePath", filePath), slog.Any("err", err))
			} else {
				fs.l.Info("清理过期文件", slog.String("pre", pre),
					slog.String("filePath", filePath))
			}
		}
	}

	return nil
}

func (fs *FileStorage) GetAll(logPre string) ([]*model.VMReport, error) {

	fs.mu.RLock()
	defer fs.mu.RUnlock()

	files, err := os.ReadDir(fs.StorageDir)
	if err != nil {
		return nil, fmt.Errorf("读取目录失败: %w", err)
	}

	var reports []*model.VMReport

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileName := file.Name()

		filePath := filepath.Join(fs.StorageDir, fileName)
		data, err := os.ReadFile(filePath)
		if err != nil {
			fs.l.Warn("读取文件内容失败，跳过该文件", slog.String("pre", logPre),
				slog.String("file_name", fileName))
			continue
		}

		var report model.VMReport
		if err := json.Unmarshal(data, &report); err != nil {
			fs.l.Warn("JSON反序列化失败，跳过该文件", slog.String("pre", logPre),
				slog.String("file_name", fileName))
			continue
		}

		reports = append(reports, &report)
	}

	return reports, nil
}

func (fs *FileStorage) GetRecent(n int, logPre string) ([]*model.VMReport, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	files, err := os.ReadDir(fs.StorageDir)
	if err != nil {
		return nil, fmt.Errorf("读取目录失败: %w", err)
	}

	type fileInfo struct {
		entry    os.DirEntry
		fileName string
		modTime  time.Time
	}
	var fileInfos []fileInfo
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		info, err := file.Info()
		if err != nil {
			continue
		}
		fileInfos = append(fileInfos, fileInfo{
			entry:    file,
			fileName: file.Name(),
			modTime:  info.ModTime(),
		})
	}

	for i := 0; i < len(fileInfos)-1; i++ {
		for j := i + 1; j < len(fileInfos); j++ {
			if fileInfos[j].modTime.After(fileInfos[i].modTime) {
				fileInfos[i], fileInfos[j] = fileInfos[j], fileInfos[i]
			}
		}
	}

	if n > len(fileInfos) {
		n = len(fileInfos)
	}

	var reports []*model.VMReport
	for i := 0; i < n; i++ {
		filePath := filepath.Join(fs.StorageDir, fileInfos[i].fileName)
		data, err := os.ReadFile(filePath)
		if err != nil {
			fs.l.Warn("读取文件失败，跳过", slog.String("pre", logPre),
				slog.String("file", fileInfos[i].fileName))
			continue
		}
		var report model.VMReport
		if err = json.Unmarshal(data, &report); err != nil {
			fs.l.Warn("JSON反序列化失败，跳过", slog.String("pre", logPre),
				slog.String("file", fileInfos[i].fileName))
			continue
		}
		reports = append(reports, &report)
	}

	return reports, nil
}

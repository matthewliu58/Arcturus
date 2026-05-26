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
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	expireDur := expireTime * time.Minute
	if expireMinutes > 0 {
		expireDur = time.Duration(expireMinutes) * time.Minute
	}

	fs := &FileStorage{
		StorageDir:     storageDir,
		expireDuration: expireDur,
		cleanupTicker:  time.NewTicker(30 * time.Second),
		l:              l,
	}

	go fs.startCleanupWorker(pre)

	return fs, nil
}

func (fs *FileStorage) Put(report *model.VMReport, pre string) (string, error) {

	if report == nil || report.VMID == "" {
		return "", errors.New("VMReport cannot be empty and VMID must be non-empty")
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	fileName := fmt.Sprintf("%s_%s.json", report.VMID, timestamp)
	filePath := filepath.Join(fs.StorageDir, fileName)
	tmpFilePath := fmt.Sprintf("%s.tmp_%d", filePath, time.Now().UnixNano())

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("JSON serialization failed: %w", err)
	}
	fs.l.Info("put file data", slog.String("pre", pre), slog.String("data", string(data)))

	if err = os.WriteFile(tmpFilePath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write temporary file: %w", err)
	}

	if err = os.Rename(tmpFilePath, filePath); err != nil {
		_ = os.Remove(tmpFilePath)
		return "", fmt.Errorf("failed to rename file: %w", err)
	}

	return report.ReportID, nil
}

func (fs *FileStorage) Get(vmID, pre string) (*model.VMReport, error) {
	if vmID == "" {
		return nil, errors.New("VMID cannot be empty")
	}

	fs.mu.RLock()
	defer fs.mu.RUnlock()

	files, err := os.ReadDir(fs.StorageDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read storage directory: %w", err)
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
		return nil, fmt.Errorf("report file for VM[%s] does not exist", vmID)
	}

	filePath := filepath.Join(fs.StorageDir, latestFile.Name())
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var report model.VMReport
	if err = json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("JSON deserialization failed: %w", err)
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
			fs.l.Error("Failed to clean up expired files", slog.String("pre", pre), slog.Any("err", err))
		}
	}
}

func (fs *FileStorage) cleanupExpiredFiles(pre string) error {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	files, err := os.ReadDir(fs.StorageDir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	expireTime_ := time.Now().Add(-fs.expireDuration)

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		fileInfo, err := file.Info()
		if err != nil {
			fs.l.Error("Failed to get file info", slog.String("pre", pre),
				slog.String("fileName", file.Name()), slog.Any("err", err))
			continue
		}

		if fileInfo.ModTime().Before(expireTime_) {
			filePath := filepath.Join(fs.StorageDir, file.Name())
			if err := os.Remove(filePath); err != nil {
				fs.l.Error("Failed to delete expired file", slog.String("pre", pre),
					slog.String("filePath", filePath), slog.Any("err", err))
			} else {
				fs.l.Info("Cleaned up expired file", slog.String("pre", pre),
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
		return nil, fmt.Errorf("failed to read directory: %w", err)
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
			fs.l.Warn("Failed to read file content, skipping", slog.String("pre", logPre),
				slog.String("file_name", fileName))
			continue
		}

		var report model.VMReport
		if err := json.Unmarshal(data, &report); err != nil {
			fs.l.Warn("JSON deserialization failed, skipping", slog.String("pre", logPre),
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
		return nil, fmt.Errorf("failed to read directory: %w", err)
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
			fs.l.Warn("Failed to read file, skipping", slog.String("pre", logPre),
				slog.String("file", fileInfos[i].fileName))
			continue
		}
		var report model.VMReport
		if err = json.Unmarshal(data, &report); err != nil {
			fs.l.Warn("JSON deserialization failed, skipping", slog.String("pre", logPre),
				slog.String("file", fileInfos[i].fileName))
			continue
		}
		reports = append(reports, &report)
	}

	return reports, nil
}

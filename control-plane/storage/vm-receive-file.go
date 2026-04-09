package storage

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
	expireTime = 1 //小文件加速更看重performance 所以参考的周期更短
)

// -------------------------- 存储抽象接口（保持不变） --------------------------
type Storage interface {
	Save(report *model.VMReport, pre string) (string, error)
	Put(report *model.VMReport, pre string) (string, error)
	Get(vmID, pre string) (*model.VMReport, error)
	// 新增：获取所有存储的VM上报数据
	GetAll(logPre string) ([]*model.VMReport, error)
	// 新增：关闭存储（停止清理协程）
	Close()
}

// -------------------------- 文件存储实现（全量重写） --------------------------
type FileStorage struct {
	storageDir     string        // 存储根目录
	mu             sync.RWMutex  // 读写锁保证并发安全
	cleanupTicker  *time.Ticker  // 定时清理过期文件的Ticker
	expireDuration time.Duration // 文件过期时长（默认5分钟）
	l              *slog.Logger
}

func NewFileStorage(storageDir string, expireMinutes int, pre string, l *slog.Logger) (*FileStorage, error) {
	// 创建存储目录
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("创建存储目录失败: %w", err)
	}

	// 设置过期时长（默认5分钟）
	expireDur := expireTime * time.Minute
	if expireMinutes > 0 {
		expireDur = time.Duration(expireMinutes) * time.Minute
	}

	fs := &FileStorage{
		storageDir:     storageDir,
		expireDuration: expireDur,
		cleanupTicker:  time.NewTicker(1 * time.Minute), // 每分钟清理一次
		l:              l,
	}

	// 启动后台清理协程
	go fs.startCleanupWorker(pre)

	return fs, nil
}

// Put 存储VM上报数据（生成新文件，不覆盖旧文件）
// 文件名规则：VMID_时间戳(毫秒).json
func (fs *FileStorage) Put(report *model.VMReport, pre string) (string, error) {
	// 入参校验
	if report == nil || report.VMID == "" {
		return "", errors.New("VMReport不能为空且VMID必须非空")
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	// 生成唯一文件名（毫秒级时间戳避免冲突）
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	fileName := fmt.Sprintf("%s_%s.json", report.VMID, timestamp)
	filePath := filepath.Join(fs.storageDir, fileName)
	// 临时文件路径（防止写入失败损坏文件）
	tmpFilePath := fmt.Sprintf("%s.tmp_%d", filePath, time.Now().UnixNano())

	// 序列化JSON（格式化输出）
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("JSON序列化失败: %w", err)
	}
	fs.l.Info("put file data", slog.String("pre", pre), slog.String("data", string(data)))

	// 写入临时文件（权限0644）
	if err := os.WriteFile(tmpFilePath, data, 0644); err != nil {
		return "", fmt.Errorf("写入临时文件失败: %w", err)
	}

	// 原子重命名（保证文件完整性）
	if err := os.Rename(tmpFilePath, filePath); err != nil {
		_ = os.Remove(tmpFilePath) // 清理临时文件
		return "", fmt.Errorf("重命名文件失败: %w", err)
	}

	return report.ReportID, nil
}

// Get 根据VMID读取最新的上报数据
// 逻辑：遍历VMID相关文件，按时间戳排序取最新的一个
func (fs *FileStorage) Get(vmID, pre string) (*model.VMReport, error) {
	if vmID == "" {
		return nil, errors.New("VMID不能为空")
	}

	fs.mu.RLock()
	defer fs.mu.RUnlock()

	// 遍历目录下所有VMID相关的文件
	files, err := os.ReadDir(fs.storageDir)
	if err != nil {
		return nil, fmt.Errorf("读取存储目录失败: %w", err)
	}

	var latestFile os.DirEntry
	var latestTimestamp int64 = -1

	// 筛选VMID相关文件并找最新的
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		name := file.Name()
		// 匹配规则：VMID_数字.json
		//if len(name) < len(vmID)+2 || !filepath.HasSuffix(name, ".json") {
		//	continue
		//}

		// 拆分文件名：VMID_时间戳.json
		prefix := fmt.Sprintf("%s_", vmID)
		if !filepath.HasPrefix(name, prefix) {
			continue
		}

		// 提取时间戳
		timestampStr := name[len(prefix) : len(name)-5] // 去掉.json后缀
		timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
		if err != nil {
			continue // 跳过非法格式文件
		}

		// 记录最新文件
		if timestamp > latestTimestamp {
			latestTimestamp = timestamp
			latestFile = file
		}
	}

	// 无匹配文件
	if latestFile == nil {
		return nil, fmt.Errorf("VM[%s]的上报文件不存在", vmID)
	}

	// 读取最新文件
	filePath := filepath.Join(fs.storageDir, latestFile.Name())
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	// 反序列化
	var report model.VMReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("JSON反序列化失败: %w", err)
	}

	return &report, nil
}

// Save 兼容API层（内部调用Put）
func (fs *FileStorage) Save(report *model.VMReport, pre string) (string, error) {
	return fs.Put(report, pre)
}

// Close 关闭存储（停止定时清理）
func (fs *FileStorage) Close() {
	if fs.cleanupTicker != nil {
		fs.cleanupTicker.Stop()
	}
}

// -------------------------- 私有方法：过期文件清理 --------------------------
// startCleanupWorker 启动后台清理协程
func (fs *FileStorage) startCleanupWorker(pre string) {
	defer fs.cleanupTicker.Stop()

	for range fs.cleanupTicker.C {
		if err := fs.cleanupExpiredFiles(pre); err != nil {
			fs.l.Error("清理过期文件失败", slog.String("pre", pre), slog.Any("err", err))
		}
	}
}

// cleanupExpiredFiles 清理过期文件
func (fs *FileStorage) cleanupExpiredFiles(pre string) error {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	files, err := os.ReadDir(fs.storageDir)
	if err != nil {
		return fmt.Errorf("读取目录失败: %w", err)
	}

	expireTime := time.Now().Add(-fs.expireDuration)

	// 遍历并删除过期文件
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		// 只处理JSON文件
		//if !filepath.HasSuffix(file.Name(), ".json") {
		//	continue
		//}

		// 获取文件修改时间
		fileInfo, err := file.Info()
		if err != nil {
			fs.l.Error("获取文件信息失败", slog.String("pre", pre),
				slog.String("fileName", file.Name()), slog.Any("err", err))
			continue
		}

		// 判断是否过期
		if fileInfo.ModTime().Before(expireTime) {
			filePath := filepath.Join(fs.storageDir, file.Name())
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

// GetAll 读取存储目录下所有有效VM上报文件，直接返回原始VMReport切片
func (fs *FileStorage) GetAll(logPre string) ([]*model.VMReport, error) {
	// 复用你提供的：加读锁保证并发安全
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	// 复用你提供的：读取目录下所有文件
	files, err := os.ReadDir(fs.storageDir)
	if err != nil {
		return nil, fmt.Errorf("读取目录失败: %w", err)
	}

	// 初始化VMReport切片（避免返回nil，保持代码严谨性）
	var reports []*model.VMReport

	// 复用你提供的：遍历文件，跳过目录
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		// 补充：获取文件名
		fileName := file.Name()

		// 补充：读取文件内容
		filePath := filepath.Join(fs.storageDir, fileName)
		data, err := os.ReadFile(filePath)
		if err != nil {
			fs.l.Warn("读取文件内容失败，跳过该文件", slog.String("pre", logPre),
				slog.String("file_name", fileName))
			continue
		}

		// 补充：JSON反序列化为原始VMReport
		var report model.VMReport
		if err := json.Unmarshal(data, &report); err != nil {
			fs.l.Warn("JSON反序列化失败，跳过该文件", slog.String("pre", logPre),
				slog.String("file_name", fileName))
			continue
		}

		// 补充：添加到原始VMReport切片
		reports = append(reports, &report)
	}

	// 直接返回原始VMReport切片
	return reports, nil
}

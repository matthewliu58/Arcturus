package metrics_tasks

import (
	"context"
	"database/sql"
	"io/ioutil"
	"os"
	"path/filepath"
	"scheduling/controller/metrics_processing/metrics_storage"
	pb "scheduling/controller/metrics_processing/protocol"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTempDir(t *testing.T) string {
	tempDir, err := ioutil.TempDir("", "task_generator_test")
	require.NoError(t, err)
	return tempDir
}

func TestNewTaskGenerator(t *testing.T) {

	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tempDir := createTempDir(t)
	defer os.RemoveAll(tempDir)

	fileManager, _ := metrics_storage.NewFileManager(tempDir)

	interval := 1 * time.Hour
	taskGenerator := NewTaskGenerator(db, fileManager, interval)

	assert.NotNil(t, taskGenerator)
	assert.Equal(t, db, taskGenerator.db)
	assert.Equal(t, fileManager, taskGenerator.fileManager)
	assert.Equal(t, interval, taskGenerator.interval)
}

func TestGenerateTasksIfNeeded(t *testing.T) {

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tempDir := createTempDir(t)
	defer os.RemoveAll(tempDir)

	fileManager, _ := metrics_storage.NewFileManager(tempDir)

	interval := 1 * time.Hour
	taskGenerator := NewTaskGenerator(db, fileManager, interval)

	taskGenerator.lastGenTime = time.Now()
	result := taskGenerator.GenerateTasksPeriodically()
	assert.False(t, result, "，")

	taskGenerator.lastGenTime = time.Now().Add(-2 * time.Hour) // 2

	nodeRows := sqlmock.NewRows([]string{"ip", "region"}).
		AddRow("10.0.0.1", "region1").
		AddRow("10.0.0.2", "region2")
	mock.ExpectQuery("^SELECT (.+) FROM nodes(.*)$").WillReturnRows(nodeRows)

	domainRows := sqlmock.NewRows([]string{"domain", "ip"}).
		AddRow("example.com", "192.168.1.1").
		AddRow("test.com", "192.168.1.2")
	mock.ExpectQuery("^SELECT (.+) FROM domain_ip_mappings(.*)$").WillReturnRows(domainRows)

	result = taskGenerator.GenerateTasksPeriodically()
	assert.True(t, result, "，")

	nodeListPath := filepath.Join(tempDir, "nodes.json")
	assert.FileExists(t, nodeListPath, "")

	domainMappingPath := filepath.Join(tempDir, "domain_ip_mappings.json")
	assert.FileExists(t, domainMappingPath, "")

	tasksPath1 := filepath.Join(tempDir, "tasks_10.0.0.1.json")
	assert.FileExists(t, tasksPath1, "1")

	tasksPath2 := filepath.Join(tempDir, "tasks_10.0.0.2.json")
	assert.FileExists(t, tasksPath2, "2")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGenerateTaskID(t *testing.T) {
	sourceIP := "10.0.0.1"
	targetIP := "10.0.0.2"
	expectedID := "task_10.0.0.1_10.0.0.2"
	actualID := generateTaskID(sourceIP, targetIP)
	assert.Equal(t, expectedID, actualID, "ID")
}

func TestStartTaskGenerator(t *testing.T) {

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tempDir := createTempDir(t)
	defer os.RemoveAll(tempDir)

	fileManager, _ := metrics_storage.NewFileManager(tempDir)

	interval := 100 * time.Millisecond
	taskGenerator := NewTaskGenerator(db, fileManager, interval)

	nodeRows := sqlmock.NewRows([]string{"ip", "region"}).
		AddRow("10.0.0.1", "region1").
		AddRow("10.0.0.2", "region2")
	mock.ExpectQuery("^SELECT (.+) FROM nodes(.*)$").WillReturnRows(nodeRows)
	mock.ExpectQuery("^SELECT (.+) FROM domain_ip_mappings(.*)$").WillReturnRows(sqlmock.NewRows([]string{"domain", "ip"}))

	mock.ExpectQuery("^SELECT (.+) FROM nodes(.*)$").WillReturnRows(nodeRows)
	mock.ExpectQuery("^SELECT (.+) FROM domain_ip_mappings(.*)$").WillReturnRows(sqlmock.NewRows([]string{"domain", "ip"}))

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	taskGenerator.StartTaskGenerator(ctx)

	if err := mock.ExpectationsWereMet(); err != nil {

		t.Logf("，：%v", err)
	}

	nodeListPath := filepath.Join(tempDir, "nodes.json")
	assert.FileExists(t, nodeListPath, "")
}

func TestTaskGeneratorHandleDBErrors(t *testing.T) {

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tempDir := createTempDir(t)
	defer os.RemoveAll(tempDir)

	fileManager, _ := metrics_storage.NewFileManager(tempDir)

	interval := 1 * time.Hour
	taskGenerator := NewTaskGenerator(db, fileManager, interval)
	taskGenerator.lastGenTime = time.Now().Add(-2 * time.Hour) // 2

	mock.ExpectQuery("^SELECT (.+) FROM nodes(.*)$").WillReturnError(sql.ErrNoRows)

	result := taskGenerator.GenerateTasksPeriodically()
	assert.False(t, result, "，false")

	assert.NoError(t, mock.ExpectationsWereMet())

	nodeListPath := filepath.Join(tempDir, "nodes.json")
	_, err = os.Stat(nodeListPath)
	assert.True(t, os.IsNotExist(err), "")
}

type mockFailFileManager struct {
	*metrics_storage.FileManager
}

func (m *mockFailFileManager) SaveNodeList(nodeList *pb.NodeList) error {
	return os.ErrPermission //
}

func (m *mockFailFileManager) SaveDomainIPMappings(mappings []*pb.DomainIPMapping) error {
	return os.ErrPermission //
}

func (m *mockFailFileManager) SaveNodeTasks(nodeIP string, tasks []*pb.ProbeTask) error {
	return os.ErrPermission //
}

func TestTaskGeneratorConcurrency(t *testing.T) {

	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	tempDir := createTempDir(t)
	defer os.RemoveAll(tempDir)

	fileManager, _ := metrics_storage.NewFileManager(tempDir)

	interval := 500 * time.Millisecond
	taskGenerator := NewTaskGenerator(db, fileManager, interval)

	nodeRows := sqlmock.NewRows([]string{"ip", "region"}).
		AddRow("10.0.0.1", "region1")
	mock.ExpectQuery("^SELECT (.+) FROM nodes(.*)$").WillReturnRows(nodeRows)
	mock.ExpectQuery("^SELECT (.+) FROM domain_ip_mappings(.*)$").WillReturnRows(sqlmock.NewRows([]string{"domain", "ip"}))

	const concurrency = 10
	results := make(chan bool, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			results <- taskGenerator.GenerateTasksPeriodically()
		}()
	}

	trueCount := 0
	for i := 0; i < concurrency; i++ {
		if <-results {
			trueCount++
		}
	}

	assert.Equal(t, 1, trueCount, "，true")
}

package forwarding

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"math"
	"sync"
	"time"

	packet "forwarding/packet_handler"
)

type BufferConfig struct {
	BuffersPerPath       int           // （InitialBuffersPerPathMaxBuffersPerPath）
	MinIdleTime          time.Duration // （60s）
	MaxPathIdleTime      time.Duration // （300s）
	MaxRequestsPerBuffer int           // （10）
	MaxBufferSize        int           // （1MB）
	MaxWaitTime          time.Duration // （100ms）
	StatisticsInterval   time.Duration // （5s）
	CleanupInterval      time.Duration // （30s）
}

func DefaultBufferConfig() BufferConfig {
	return BufferConfig{
		BuffersPerPath:       1,
		MinIdleTime:          60 * time.Second,
		MaxPathIdleTime:      300 * time.Second,
		MaxRequestsPerBuffer: 1,
		MaxBufferSize:        1300, // MTU-tcpheader-ipheader-HEADER
		MaxWaitTime:          5 * time.Millisecond,
		StatisticsInterval:   5 * time.Second,
		CleanupInterval:      30 * time.Second,
	}
}

type BufferStats struct {
	TotalRequests       int64
	MergedRequests      int64
	MergeRatio          float64
	AvgRequestsPerMerge float64
	AvgWaitTime         time.Duration
	ActivePaths         int
	TotalBuffers        int
	BufferUtilization   float64
}

type RequestBuffer struct {
	BufferID         string
	Requests         []*RequestState
	FirstRequestTime time.Time
	LastAccessTime   time.Time
	CurrentSize      int
	IsFlushing       bool
	Mutex            sync.Mutex
	Config           *BufferConfig
}

type ResponseBuffer struct {
	BufferID          string
	Responses         []*BufferedResponse
	FirstResponseTime time.Time
	LastAccessTime    time.Time
	CurrentSize       int
	IsFlushing        bool
	Mutex             sync.Mutex
	Config            *BufferConfig
	CommonHopList     []uint32
}

type BufferedResponse struct {
	RequestID    uint32
	ResponseData []byte
	Size         int
	ReceivedAt   time.Time
	HopList      []uint32
}

type RequestPathBuffers struct {
	PathHash       string
	Buffers        []*RequestBuffer
	BufferCount    int
	LastAccessTime time.Time
	TotalRequests  int64
	MergedRequests int64

	TimeoutTimer *time.Timer
	TimerActive  bool
	TimerLock    sync.Mutex
}

type ResponsePathBuffers struct {
	PathHash        string
	Buffers         []*ResponseBuffer
	BufferCount     int
	LastAccessTime  time.Time
	TotalResponses  int64
	MergedResponses int64
	TimeoutTimer    *time.Timer
	TimerActive     bool
	TimerLock       sync.Mutex
}

type BufferManager struct {
	RequestPaths  map[string]*RequestPathBuffers
	ResponsePaths map[string]*ResponsePathBuffers
	PathMutexes   map[string]*sync.RWMutex
	GlobalMutex   sync.RWMutex
	Config        BufferConfig
	Stats         BufferStats
	stopChan      chan struct{}
	stateManager  *RequestStateManager

	ConfigUpdateChan chan BufferConfig
	ConfigLock       sync.RWMutex

	SendRequestFunc       func([]byte, string, uint32, *RequestState) error
	SendMergedRequestFunc func([]byte, string, *packet.Packet) error

	SendMergedResponseFunc func(string, []byte, []byte) error // previousHopIP, headerBytes, responseBytes
}

func NewBufferManager(config BufferConfig, stateManager *RequestStateManager) *BufferManager {
	manager := &BufferManager{
		RequestPaths:     make(map[string]*RequestPathBuffers),
		ResponsePaths:    make(map[string]*ResponsePathBuffers),
		PathMutexes:      make(map[string]*sync.RWMutex),
		Config:           config,
		stopChan:         make(chan struct{}),
		stateManager:     stateManager,
		ConfigUpdateChan: make(chan BufferConfig, 1),
	}

	go manager.handleConfigUpdates()
	go manager.statisticsCollector()
	go manager.cleanupRoutine()

	return manager
}

func (bm *BufferManager) handleConfigUpdates() {
	for {
		select {
		case <-bm.stopChan:
			return
		case newConfig := <-bm.ConfigUpdateChan:
			bm.updateConfiguration(newConfig)
		}
	}
}

func (bm *BufferManager) updateConfiguration(newConfig BufferConfig) {
	bm.ConfigLock.Lock()
	oldConfig := bm.Config
	bm.Config = newConfig
	bm.ConfigLock.Unlock()

	go bm.applyConfigChanges(oldConfig, newConfig)
}

func (bm *BufferManager) applyConfigChanges(oldConfig, newConfig BufferConfig) {

	bm.GlobalMutex.RLock()
	pathHashes := make([]string, 0, len(bm.RequestPaths))
	for pathHash := range bm.RequestPaths {
		pathHashes = append(pathHashes, pathHash)
	}
	bm.GlobalMutex.RUnlock()

	for _, pathHash := range pathHashes {
		bm.adjustPathBuffers(pathHash, newConfig.BuffersPerPath)
	}

	log.Debugf("[BUFFER]applyConfigChanges, buffers=%d, maxReqs=%d, maxSize=%d, maxWait=%v",
		newConfig.BuffersPerPath, newConfig.MaxRequestsPerBuffer,
		newConfig.MaxBufferSize, newConfig.MaxWaitTime)
}

func (bm *BufferManager) adjustPathBuffers(pathHash string, targetCount int) {
	bm.GlobalMutex.RLock()
	pathMutex, exists := bm.PathMutexes[pathHash]
	bm.GlobalMutex.RUnlock()

	if !exists {
		return
	}

	pathMutex.Lock()
	defer pathMutex.Unlock()

	path, exists := bm.RequestPaths[pathHash]
	if !exists {
		return
	}

	if path.BufferCount == targetCount {
		return
	}

	if path.BufferCount < targetCount {

		for i := path.BufferCount; i < targetCount; i++ {
			path.Buffers = append(path.Buffers, NewRequestBuffer(
				fmt.Sprintf("%s-%d", pathHash, i), &bm.Config))
		}
	} else {

		bm.redistributeRequests(path, targetCount)

		path.Buffers = path.Buffers[:targetCount]
	}

	path.BufferCount = targetCount
	log.Infof("[BUFFER]  %s  %d", pathHash, targetCount)
}

func (bm *BufferManager) redistributeRequests(path *RequestPathBuffers, targetCount int) {

	var allRequests []*RequestState

	for i := targetCount; i < path.BufferCount; i++ {
		buffer := path.Buffers[i]
		buffer.Mutex.Lock()

		if len(buffer.Requests) > 0 {
			allRequests = append(allRequests, buffer.Requests...)
			buffer.Requests = nil
			buffer.CurrentSize = 0
			buffer.FirstRequestTime = time.Time{}
		}

		buffer.Mutex.Unlock()
	}

	for _, req := range allRequests {

		targetBuffer := bm.getLeastLoadedBufferFromSlice(path.Buffers[:targetCount])

		targetBuffer.Mutex.Lock()

		if len(targetBuffer.Requests) == 0 {
			targetBuffer.FirstRequestTime = time.Now()
		}

		targetBuffer.Requests = append(targetBuffer.Requests, req)
		targetBuffer.CurrentSize += req.Size

		req.mu.Lock()
		req.BufferID = targetBuffer.BufferID
		req.mu.Unlock()

		targetBuffer.Mutex.Unlock()
	}

	if len(allRequests) > 0 {
		log.Infof("[BUFFER]redistributeRequests, lenallRequests=%d, targetCount=%d", len(allRequests), targetCount)
	}
}

func (bm *BufferManager) SetSendFunctions(
	sendFunc func([]byte, string, uint32, *RequestState) error,
	sendMergedFunc func([]byte, string, *packet.Packet) error,
	sendMergedRespFunc func(string, []byte, []byte) error) {
	bm.SendRequestFunc = sendFunc
	bm.SendMergedRequestFunc = sendMergedFunc
	bm.SendMergedResponseFunc = sendMergedRespFunc
}

func (bm *BufferManager) Stop() {
	close(bm.stopChan)
}

func (bm *BufferManager) GetStats() BufferStats {
	bm.GlobalMutex.RLock()
	defer bm.GlobalMutex.RUnlock()
	return bm.Stats
}

func (bm *BufferManager) statisticsCollector() {
	ticker := time.NewTicker(bm.Config.StatisticsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-bm.stopChan:
			return
		case <-ticker.C:
			bm.updateStats()
		}
	}
}

func (bm *BufferManager) updateStats() {
	var stats BufferStats

	bm.GlobalMutex.RLock()

	stats.ActivePaths = len(bm.RequestPaths) + len(bm.ResponsePaths)
	stats.TotalBuffers = 0

	for _, path := range bm.RequestPaths {
		stats.TotalRequests += path.TotalRequests
		stats.MergedRequests += path.MergedRequests
		stats.TotalBuffers += path.BufferCount
	}

	for _, path := range bm.ResponsePaths {
		stats.TotalRequests += path.TotalResponses
		stats.MergedRequests += path.MergedResponses
		stats.TotalBuffers += path.BufferCount
	}

	bm.GlobalMutex.RUnlock()

	if stats.TotalRequests > 0 {
		stats.MergeRatio = float64(stats.MergedRequests) / float64(stats.TotalRequests)
	}

	if stats.MergedRequests > 0 {

		stats.AvgRequestsPerMerge = float64(stats.MergedRequests) / float64(stats.MergedRequests/5)
	}

	bm.GlobalMutex.Lock()
	bm.Stats = stats
	bm.GlobalMutex.Unlock()
}

func (bm *BufferManager) cleanupRoutine() {
	ticker := time.NewTicker(bm.Config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-bm.stopChan:
			return
		case <-ticker.C:
			bm.cleanupBuffers()
		}
	}
}

func (bm *BufferManager) cleanupBuffers() {
	now := time.Now()

	bm.cleanupRequestPaths(now)

	bm.cleanupResponsePaths(now)
}

func (bm *BufferManager) cleanupRequestPaths(now time.Time) {

	bm.GlobalMutex.RLock()
	pathHashes := make([]string, 0, len(bm.RequestPaths))
	for pathHash := range bm.RequestPaths {
		pathHashes = append(pathHashes, pathHash)
	}
	bm.GlobalMutex.RUnlock()

	for _, pathHash := range pathHashes {
		bm.GlobalMutex.RLock()
		pathMutex, exists := bm.PathMutexes[pathHash]
		bm.GlobalMutex.RUnlock()

		if !exists {
			continue
		}

		pathMutex.RLock()
		path, exists := bm.RequestPaths[pathHash]
		pathMutex.RUnlock()

		if !exists {
			continue
		}

		if now.Sub(path.LastAccessTime) > bm.Config.MaxPathIdleTime {

			pathMutex.Lock()

			path.TimerLock.Lock()
			if path.TimerActive && path.TimeoutTimer != nil {
				path.TimeoutTimer.Stop()
				path.TimerActive = false
			}
			path.TimerLock.Unlock()

			bm.GlobalMutex.Lock()
			delete(bm.RequestPaths, pathHash)
			bm.GlobalMutex.Unlock()

			pathMutex.Unlock()

			log.Infof("[BUFFER]cleanupRequestPaths, pathHash=%s", pathHash)
		}
	}
}

func (bm *BufferManager) cleanupResponsePaths(now time.Time) {

	bm.GlobalMutex.RLock()
	pathHashes := make([]string, 0, len(bm.ResponsePaths))
	for pathHash := range bm.ResponsePaths {
		pathHashes = append(pathHashes, pathHash)
	}
	bm.GlobalMutex.RUnlock()

	for _, pathHash := range pathHashes {
		bm.GlobalMutex.RLock()
		pathMutex, exists := bm.PathMutexes[pathHash]
		bm.GlobalMutex.RUnlock()

		if !exists {
			continue
		}

		pathMutex.RLock()
		path, exists := bm.ResponsePaths[pathHash]
		pathMutex.RUnlock()

		if !exists {
			continue
		}

		if now.Sub(path.LastAccessTime) > bm.Config.MaxPathIdleTime {

			pathMutex.Lock()

			path.TimerLock.Lock()
			if path.TimerActive && path.TimeoutTimer != nil {
				path.TimeoutTimer.Stop()
				path.TimerActive = false
			}
			path.TimerLock.Unlock()

			bm.GlobalMutex.Lock()
			delete(bm.ResponsePaths, pathHash)
			bm.GlobalMutex.Unlock()

			pathMutex.Unlock()

			log.Infof("[BUFFER]cleanupResponsePaths, pathHash=%s", pathHash)
		}
	}
}

func (bm *BufferManager) ProcessRequest(reqState *RequestState) error {

	bm.stateManager.UpdateStatus(reqState.RequestID, StatusPending)

	pathHash := CalculatePathHash(reqState.HopList)

	buffer := bm.getRequestBuffer(pathHash, reqState.RequestID)

	shouldFlush := buffer.AddRequest(reqState)

	if shouldFlush {

		bm.stateManager.UpdateStatus(reqState.RequestID, StatusMerged)
		buffer.ProcessBuffer(bm.mergeAndSendRequests)
	}

	return nil
}

func (bm *BufferManager) startPathTimeoutTimer(path *RequestPathBuffers) {
	path.TimerLock.Lock()
	defer path.TimerLock.Unlock()

	if path.TimerActive {
		return
	}

	bm.ConfigLock.RLock()
	maxWaitTime := bm.Config.MaxWaitTime
	bm.ConfigLock.RUnlock()

	path.TimerActive = true
	path.TimeoutTimer = time.AfterFunc(maxWaitTime, func() {
		bm.handlePathTimeout(path)
	})

	log.Infof("[BUFFER]startPathTimeoutTimer, PathHash=%s, maxWaitTime=%v", path.PathHash, maxWaitTime)
}

func (bm *BufferManager) startResponsePathTimeoutTimer(path *ResponsePathBuffers) {
	path.TimerLock.Lock()
	defer path.TimerLock.Unlock()

	if path.TimerActive {
		return
	}

	bm.ConfigLock.RLock()
	maxWaitTime := bm.Config.MaxWaitTime
	bm.ConfigLock.RUnlock()

	path.TimerActive = true

	var pathRef = path
	path.TimeoutTimer = time.AfterFunc(maxWaitTime, func() {
		bm.handleResponsePathTimeout(pathRef)
	})

	log.Infof("[BUFFER]startResponsePathTimeoutTimer, PathHash=%s, maxWaitTime=%v", path.PathHash, maxWaitTime)
}

func (bm *BufferManager) handleResponsePathTimeout(path *ResponsePathBuffers) {
	log.Infof("[BUFFER]handleResponsePathTimeout, PathHash=%s", path.PathHash)

	restartTimer := false
	now := time.Now()

	bm.ConfigLock.RLock()
	maxWaitTime := bm.Config.MaxWaitTime
	bm.ConfigLock.RUnlock()

	for _, buffer := range path.Buffers {
		buffer.Mutex.Lock()

		hasResponses := len(buffer.Responses) > 0
		shouldFlush := false

		if hasResponses {

			if buffer.FirstResponseTime.IsZero() {
				buffer.FirstResponseTime = now
				restartTimer = true
			} else if now.Sub(buffer.FirstResponseTime) >= maxWaitTime {
				shouldFlush = true
			} else {
				restartTimer = true
			}
		}

		needsFlush := shouldFlush && !buffer.IsFlushing
		buffer.Mutex.Unlock()

		if needsFlush {
			log.Infof("[BUFFER]handleResponsePathTimeout,  PathHash=%s, BufferID=%s", path.PathHash, buffer.BufferID)
			buffer.FlushBuffer(bm.mergeAndSendBufferedResponses)
		}
	}

	path.TimerLock.Lock()
	path.TimerActive = false

	if restartTimer {
		path.TimeoutTimer = time.AfterFunc(maxWaitTime, func() {
			bm.handleResponsePathTimeout(path)
		})
		path.TimerActive = true
	}
	path.TimerLock.Unlock()
}

func (bm *BufferManager) handlePathTimeout(path *RequestPathBuffers) {
	log.Infof("[BUFFER]handlePathTimeout, PathHash=%s", path.PathHash)

	restartTimer := false
	now := time.Now()

	bm.ConfigLock.RLock()
	maxWaitTime := bm.Config.MaxWaitTime
	bm.ConfigLock.RUnlock()

	for _, buffer := range path.Buffers {
		buffer.Mutex.Lock()

		hasRequests := len(buffer.Requests) > 0
		shouldFlush := false

		if hasRequests {

			if buffer.FirstRequestTime.IsZero() {
				buffer.FirstRequestTime = now
				restartTimer = true
			} else if now.Sub(buffer.FirstRequestTime) >= maxWaitTime {
				shouldFlush = true
			} else {
				restartTimer = true
			}
		}

		needsFlush := shouldFlush && !buffer.IsFlushing
		buffer.Mutex.Unlock()

		if needsFlush {
			log.Infof("[BUFFER]handlePathTimeout, PathHash=%s, BufferID=%s", path.PathHash, buffer.BufferID)
			buffer.ProcessBuffer(bm.mergeAndSendRequests)
		}
	}

	path.TimerLock.Lock()
	path.TimerActive = false

	if restartTimer {
		path.TimeoutTimer = time.AfterFunc(maxWaitTime, func() {
			bm.handlePathTimeout(path)
		})
		path.TimerActive = true
	}
	path.TimerLock.Unlock()
}

func (bm *BufferManager) getLeastLoadedBuffer(path *RequestPathBuffers) *RequestBuffer {
	var leastLoaded *RequestBuffer
	minRequests := math.MaxInt32

	for _, buffer := range path.Buffers {
		buffer.Mutex.Lock()
		requestCount := len(buffer.Requests)
		buffer.Mutex.Unlock()

		if requestCount < minRequests {
			minRequests = requestCount
			leastLoaded = buffer
		}
	}

	if leastLoaded == nil && len(path.Buffers) > 0 {
		leastLoaded = path.Buffers[0]
	}

	return leastLoaded
}

func (bm *BufferManager) getLeastLoadedBufferFromSlice(buffers []*RequestBuffer) *RequestBuffer {
	var leastLoaded *RequestBuffer
	minRequests := math.MaxInt32

	for _, buffer := range buffers {
		buffer.Mutex.Lock()
		requestCount := len(buffer.Requests)
		buffer.Mutex.Unlock()

		if requestCount < minRequests {
			minRequests = requestCount
			leastLoaded = buffer
		}
	}

	if leastLoaded == nil && len(buffers) > 0 {
		leastLoaded = buffers[0]
	}

	return leastLoaded
}

func (bm *BufferManager) getRequestBuffer(pathHash string, requestID uint32) *RequestBuffer {

	bm.GlobalMutex.RLock()
	pathMutex, exists := bm.PathMutexes[pathHash]
	bm.GlobalMutex.RUnlock()

	if !exists {

		bm.GlobalMutex.Lock()
		pathMutex = &sync.RWMutex{}
		bm.PathMutexes[pathHash] = pathMutex
		bm.GlobalMutex.Unlock()
	}

	pathMutex.RLock()
	path, exists := bm.RequestPaths[pathHash]
	pathMutex.RUnlock()

	if !exists {

		pathMutex.Lock()

		path, exists = bm.RequestPaths[pathHash]
		if !exists {

			bm.ConfigLock.RLock()
			buffersPerPath := bm.Config.BuffersPerPath
			bm.ConfigLock.RUnlock()

			path = &RequestPathBuffers{
				PathHash:       pathHash,
				Buffers:        make([]*RequestBuffer, buffersPerPath),
				BufferCount:    buffersPerPath,
				LastAccessTime: time.Now(),
				TotalRequests:  0,
				MergedRequests: 0,
				TimerActive:    false,
			}

			for i := 0; i < buffersPerPath; i++ {
				path.Buffers[i] = NewRequestBuffer(fmt.Sprintf("%s-%d", pathHash, i), &bm.Config)
			}

			bm.GlobalMutex.Lock()
			bm.RequestPaths[pathHash] = path
			bm.GlobalMutex.Unlock()
		}

		pathMutex.Unlock()
	}

	path.LastAccessTime = time.Now()
	path.TotalRequests++

	isEmpty := true
	for _, buf := range path.Buffers {
		buf.Mutex.Lock()
		if len(buf.Requests) > 0 {
			isEmpty = false
		}
		buf.Mutex.Unlock()

		if !isEmpty {
			break
		}
	}

	if isEmpty {
		bm.startPathTimeoutTimer(path)
	}

	buffer := bm.getLeastLoadedBuffer(path)

	log.Infof("[BUFFER-DETAIL]getRequestBuffer, requestID=%d, BufferID=%s (BufferCount=%d)",
		requestID, buffer.BufferID, path.BufferCount)

	return buffer
}

func (bm *BufferManager) getResponseBuffer(pathHash string, requestID uint32) *ResponseBuffer {

	bm.GlobalMutex.RLock()
	pathMutex, exists := bm.PathMutexes[pathHash]
	bm.GlobalMutex.RUnlock()

	if !exists {

		bm.GlobalMutex.Lock()
		pathMutex = &sync.RWMutex{}
		bm.PathMutexes[pathHash] = pathMutex
		bm.GlobalMutex.Unlock()
	}

	pathMutex.RLock()
	path, exists := bm.ResponsePaths[pathHash]
	pathMutex.RUnlock()

	if !exists {

		pathMutex.Lock()

		path, exists = bm.ResponsePaths[pathHash]
		if !exists {

			bm.ConfigLock.RLock()
			buffersPerPath := bm.Config.BuffersPerPath
			bm.ConfigLock.RUnlock()

			path = &ResponsePathBuffers{
				PathHash:        pathHash,
				Buffers:         make([]*ResponseBuffer, buffersPerPath),
				BufferCount:     buffersPerPath,
				LastAccessTime:  time.Now(),
				TotalResponses:  0,
				MergedResponses: 0,
			}

			for i := 0; i < buffersPerPath; i++ {
				path.Buffers[i] = NewResponseBuffer(fmt.Sprintf("%s-%d", pathHash, i), &bm.Config)
			}

			bm.GlobalMutex.Lock()
			bm.ResponsePaths[pathHash] = path
			bm.GlobalMutex.Unlock()
		}

		pathMutex.Unlock()
	}

	path.LastAccessTime = time.Now()
	path.TotalResponses++

	isEmpty := true
	for _, buf := range path.Buffers {
		buf.Mutex.Lock()
		if len(buf.Responses) > 0 {
			isEmpty = false
		}
		buf.Mutex.Unlock()

		if !isEmpty {
			break
		}
	}

	if isEmpty {
		bm.startResponsePathTimeoutTimer(path)
	}

	bufferIndex := uint32(requestID) % uint32(path.BufferCount)
	buffer := path.Buffers[bufferIndex]

	log.Infof("[BUFFER-DETAIL]getResponseBuffer, requestID=%d BufferID=%s (BufferCount=%d)",
		requestID, buffer.BufferID, path.BufferCount)

	return buffer
}

func NewRequestBuffer(id string, config *BufferConfig) *RequestBuffer {
	return &RequestBuffer{
		BufferID:         id,
		Requests:         make([]*RequestState, 0, config.MaxRequestsPerBuffer),
		FirstRequestTime: time.Time{}, //
		LastAccessTime:   time.Now(),
		CurrentSize:      0,
		IsFlushing:       false,
		Config:           config,
	}
}

func NewResponseBuffer(id string, config *BufferConfig) *ResponseBuffer {
	return &ResponseBuffer{
		BufferID:          id,
		Responses:         make([]*BufferedResponse, 0, config.MaxRequestsPerBuffer),
		FirstResponseTime: time.Time{},
		LastAccessTime:    time.Now(),
		CurrentSize:       0,
		IsFlushing:        false,
		Config:            config,
	}
}

func (b *RequestBuffer) AddRequest(req *RequestState) bool {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()

	b.LastAccessTime = time.Now()

	if len(b.Requests) == 0 {
		b.FirstRequestTime = time.Now()
	}

	reqSize := req.Size

	b.Requests = append(b.Requests, req)
	b.CurrentSize += reqSize

	req.mu.Lock()
	req.BufferID = b.BufferID
	req.mu.Unlock()

	return b.shouldTriggerFlush()
}

func (b *RequestBuffer) isFull() bool {
	return len(b.Requests) >= b.Config.MaxRequestsPerBuffer ||
		b.CurrentSize >= b.Config.MaxBufferSize
}

func (b *ResponseBuffer) isFull() bool {
	return len(b.Responses) >= b.Config.MaxRequestsPerBuffer ||
		b.CurrentSize >= b.Config.MaxBufferSize
}

func (b *RequestBuffer) shouldTriggerFlush() bool {

	if b.IsFlushing {
		return false
	}

	if len(b.Requests) == 0 {
		return false
	}

	if len(b.Requests) >= b.Config.MaxRequestsPerBuffer {
		return true
	}

	if b.CurrentSize >= b.Config.MaxBufferSize {
		return true
	}

	if !b.FirstRequestTime.IsZero() &&
		time.Since(b.FirstRequestTime) >= b.Config.MaxWaitTime {
		return true
	}

	log.Infof("[BUFFER-DETAIL]shouldTriggerFlush, BufferID=%s LenRequests:MaxRequestsPerBuffer =%d/%d,"+
		"CurrentSize:MaxBufferSize =%d/%d, SinceFirstRequestTime:MaxWaitTime=%v/%v",
		b.BufferID, len(b.Requests), b.Config.MaxRequestsPerBuffer,
		b.CurrentSize, b.Config.MaxBufferSize,
		time.Since(b.FirstRequestTime), b.Config.MaxWaitTime)

	return false
}

func (b *RequestBuffer) ProcessBuffer(mergeAndSendFunc func([]*RequestState)) {
	b.Mutex.Lock()

	if b.IsFlushing || len(b.Requests) == 0 {
		b.Mutex.Unlock()
		return
	}

	b.IsFlushing = true

	requests := make([]*RequestState, len(b.Requests))
	copy(requests, b.Requests)

	b.Requests = make([]*RequestState, 0, b.Config.MaxRequestsPerBuffer)
	b.CurrentSize = 0
	b.FirstRequestTime = time.Time{}

	b.IsFlushing = false
	b.Mutex.Unlock()

	go mergeAndSendFunc(requests)
}

func CalculatePathHash(hopList []uint32) string {

	return fmt.Sprintf("%v", hopList)
}

func CalculateResponsePathHash(hopList []uint32) string {
	if len(hopList) <= 1 {
		return "empty"
	}

	commonPath := hopList[:len(hopList)-1]

	return fmt.Sprintf("%v", commonPath)
}

func (b *ResponseBuffer) AddResponse(resp *BufferedResponse) bool {
	b.Mutex.Lock()
	defer b.Mutex.Unlock()

	b.LastAccessTime = time.Now()

	if len(b.Responses) == 0 {
		b.FirstResponseTime = time.Now()
	}

	b.Responses = append(b.Responses, resp)
	b.CurrentSize += resp.Size

	return b.shouldTriggerFlush()
}

func (b *ResponseBuffer) shouldTriggerFlush() bool {

	if b.IsFlushing {
		return false
	}

	if len(b.Responses) == 0 {
		return false
	}

	if len(b.Responses) >= b.Config.MaxRequestsPerBuffer {
		return true
	}

	if b.CurrentSize >= b.Config.MaxBufferSize {
		return true
	}

	if !b.FirstResponseTime.IsZero() &&
		time.Since(b.FirstResponseTime) >= b.Config.MaxWaitTime {
		return true
	}

	log.Infof("[BUFFER-DETAIL]shouldTriggerFlush, BufferID=%s, LenResponses:MaxRequestsPerBuffer =%d/%d,"+
		"CurrentSize:MaxBufferSize=%d/%d, SinceFirstResponseTime:MaxWaitTime=%v/%v",
		b.BufferID, len(b.Responses), b.Config.MaxRequestsPerBuffer,
		b.CurrentSize, b.Config.MaxBufferSize,
		time.Since(b.FirstResponseTime), b.Config.MaxWaitTime)

	return false
}

func (b *ResponseBuffer) FlushBuffer(mergeAndSendFunc func([]*BufferedResponse, string)) {
	b.Mutex.Lock()

	if b.IsFlushing || len(b.Responses) == 0 {
		b.Mutex.Unlock()
		return
	}

	b.IsFlushing = true

	responses := make([]*BufferedResponse, len(b.Responses))
	copy(responses, b.Responses)

	var previousHopIP string
	if len(b.CommonHopList) > 0 {

		previousHopIP = fmt.Sprintf("%d.%d.%d.%d",
			(b.CommonHopList[0]>>24)&0xFF,
			(b.CommonHopList[0]>>16)&0xFF,
			(b.CommonHopList[0]>>8)&0xFF,
			b.CommonHopList[0]&0xFF)
	} else if len(responses) > 0 && len(responses[0].HopList) > 1 {

		hopIndex := len(responses[0].HopList) - 2
		hop := responses[0].HopList[hopIndex]
		previousHopIP = fmt.Sprintf("%d.%d.%d.%d",
			(hop>>24)&0xFF, (hop>>16)&0xFF, (hop>>8)&0xFF, hop&0xFF)
	} else {
		previousHopIP = "unknown"
	}

	b.Responses = make([]*BufferedResponse, 0, b.Config.MaxRequestsPerBuffer)
	b.CurrentSize = 0
	b.FirstResponseTime = time.Time{}

	b.IsFlushing = false
	b.Mutex.Unlock()

	go mergeAndSendFunc(responses, previousHopIP)
}

func (bm *BufferManager) ProcessResponse(resp *ResponseData) error {

	pathHash := CalculateResponsePathHash(resp.HopList)

	buffer := bm.getResponseBuffer(pathHash, resp.RequestID)

	bufferedResp := &BufferedResponse{
		RequestID:    resp.RequestID,
		ResponseData: resp.Data,
		Size:         len(resp.Data),
		ReceivedAt:   time.Now(),
		HopList:      resp.HopList,
	}

	shouldFlush := buffer.AddResponse(bufferedResp)

	if shouldFlush {
		buffer.FlushBuffer(bm.mergeAndSendBufferedResponses)
	}

	return nil
}

func (bm *BufferManager) mergeAndSendRequests(requests []*RequestState) {
	if len(requests) == 0 {
		return
	}

	firstReq := requests[0]
	nextHopIP := firstReq.NextHopIP

	packetIDs := make([]uint32, len(requests))
	requestSizes := make([]int, len(requests))
	requestBodies := make([][]byte, len(requests))

	mergeGroupID := uint32(time.Now().UnixNano())

	for i, req := range requests {
		packetIDs[i] = req.RequestID
		requestSizes[i] = req.Size
		requestBodies[i] = req.RequestData

		req.mu.Lock()
		req.MergeGroupID = mergeGroupID
		req.mu.Unlock()

		bm.stateManager.UpdateStatus(req.RequestID, StatusSent)
	}

	var headerToUse *packet.Packet
	if firstReq.UpdatedHeader != nil {

		headerToUse = firstReq.UpdatedHeader
		headerToUse.PacketCount = byte(len(requests))
		headerToUse.PacketID = packetIDs
		headerToUse.Offsets = packet.CalcRelativeOffsets(requestSizes)

		log.Infof("[BUFFER]mergeAndSendRequests, UpdatedHeader HopCounts=%d", headerToUse.HopCounts)
	} else {

		headerToUse = packet.NewMergedPacket(packetIDs, requestSizes, firstReq.HopList, 0x01)
		log.Warningf("[BUFFER-WARN]mergeAndSendRequests, UpdatedHeader HopCounts=%d", headerToUse.HopCounts)
	}

	headerBytes, err := headerToUse.Pack()
	if err != nil {
		log.Errorf("[BUFFER-ERROR]mergeAndSendRequests, headerToUse Pack failed, err: %v", err)

		for _, req := range requests {
			if bm.SendRequestFunc != nil {
				bm.SendRequestFunc(req.RequestData, req.NextHopIP, req.RequestID, req)
			}
		}
		return
	}

	totalSize := len(headerBytes)
	for _, body := range requestBodies {
		totalSize += len(body)
	}

	mergedData := make([]byte, 0, totalSize)
	mergedData = append(mergedData, headerBytes...)

	for _, body := range requestBodies {
		mergedData = append(mergedData, body...)
	}

	if bm.SendMergedRequestFunc != nil {
		err = bm.SendMergedRequestFunc(mergedData, nextHopIP, headerToUse)
		if err != nil {
			log.Errorf("[BUFFER-ERROR]mergeAndSendRequests,  SendMergedRequestFunc failed ,err: %v", err)

			for _, req := range requests {
				if bm.SendRequestFunc != nil {
					bm.SendRequestFunc(req.RequestData, req.NextHopIP, req.RequestID, req)
				}
			}
		}
	}
}

func (bm *BufferManager) mergeAndSendBufferedResponses(responses []*BufferedResponse, previousHopIP string) {
	if len(responses) == 0 {
		return
	}

	log.Infof("[BUFFER]mergeAndSendBufferedResponses, len of responses: %d", len(responses))

	commonHopList := responses[0].HopList

	respHeader := &packet.Packet{
		PacketCount: byte(len(responses)),
		PacketID:    make([]uint32, len(responses)),
		HopList:     commonHopList,
		HopCounts:   byte(len(commonHopList)) - 1,
	}

	respSizes := make([]int, len(responses))
	respBodies := make([][]byte, len(responses))

	for i, resp := range responses {
		respHeader.PacketID[i] = resp.RequestID
		respSizes[i] = resp.Size
		respBodies[i] = resp.ResponseData
	}

	respHeader.Offsets = packet.CalcRelativeOffsets(respSizes)

	headerBytes, err := respHeader.Pack()
	if err != nil {
		log.Errorf("[BUFFER-ERROR]mergeAndSendBufferedResponses, err: %v", err)
		return
	}

	totalSize := 0
	for _, respBody := range respBodies {
		totalSize += len(respBody)
	}

	mergedRespData := make([]byte, 0, totalSize)
	for _, respBody := range respBodies {
		mergedRespData = append(mergedRespData, respBody...)
	}

	log.Errorf("[BUFFER]mergeAndSendBufferedResponses,mergedRespData=%d, previousHopIP=%s",
		len(mergedRespData), previousHopIP)

	if bm.SendMergedResponseFunc != nil {
		err := bm.SendMergedResponseFunc(previousHopIP, headerBytes, mergedRespData)
		if err != nil {
			log.Errorf("[BUFFER-ERROR]mergeAndSendBufferedResponses, SendMergedResponseFunc failed, err: %v", err)
		} else {
			log.Infof("[BUFFER]mergeAndSendBufferedResponses, previousHopIP: %s", previousHopIP)
		}
	} else {
		log.Errorf("[BUFFER-ERROR]mergeAndSendBufferedResponses, SendMergedResponseFunc is null")
	}
}

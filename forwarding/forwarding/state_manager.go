package forwarding

import (
	packet "forwarding/packet_handler"
	log "github.com/sirupsen/logrus"
	"net/http"
	"sync"
	"time"
)

type RequestStatus int

const (
	StatusCreated RequestStatus = iota
	StatusPending
	StatusMerged
	StatusSent
	StatusResponding
	StatusCompleted
	StatusFailed
)

func (s RequestStatus) String() string {
	switch s {
	case StatusCreated:
		return "Created"
	case StatusPending:
		return "Pending"
	case StatusMerged:
		return "Merged"
	case StatusSent:
		return "Sent"
	case StatusResponding:
		return "Responding"
	case StatusCompleted:
		return "Completed"
	case StatusFailed:
		return "Failed"
	default:
		return "Unknown"
	}
}

type RequestState struct {
	RequestID       uint32
	OriginalRequest *http.Request
	ResponseWriter  http.ResponseWriter

	RequestData []byte
	Size        int

	Status        RequestStatus
	CreatedAt     time.Time
	LastUpdatedAt time.Time

	NextHopIP string
	HopList   []uint32
	IsLastHop bool

	ResponseReceived chan struct{}

	BufferID     string
	MergeGroupID uint32

	UpdatedHeader *packet.Packet

	mu sync.RWMutex
}

func (s *RequestState) UpdateStatus(status RequestStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Status = status
	s.LastUpdatedAt = time.Now()
}

func (s *RequestState) SetRequestData(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.RequestData = data
	s.Size = len(data)
}

func (s *RequestState) IsFinished() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.Status == StatusCompleted || s.Status == StatusFailed
}

type RequestStateManager struct {
	states            map[uint32]*RequestState
	expiration        time.Duration
	cleanupInterval   time.Duration
	activeRequests    int64
	totalRequests     int64
	completedRequests int64
	failedRequests    int64
	mu                sync.RWMutex
	stopCleanup       chan struct{}
}

func NewRequestStateManager(expiration, cleanupInterval time.Duration) *RequestStateManager {
	manager := &RequestStateManager{
		states:          make(map[uint32]*RequestState),
		expiration:      expiration,
		cleanupInterval: cleanupInterval,
		stopCleanup:     make(chan struct{}),
	}

	go manager.cleanupExpiredStates()

	return manager
}

func (m *RequestStateManager) Stop() {
	close(m.stopCleanup)
}

func (m *RequestStateManager) AddState(state *RequestState) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.states[state.RequestID] = state
	m.activeRequests++
	m.totalRequests++

	log.Infof("[StateManager]AddState, RequestID=%d, Status=%s", state.RequestID, state.Status)
}

func (m *RequestStateManager) GetState(requestID uint32) (*RequestState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, exists := m.states[requestID]
	return state, exists
}

func (m *RequestStateManager) UpdateStatus(requestID uint32, newStatus RequestStatus) bool {
	m.mu.RLock()
	state, exists := m.states[requestID]
	m.mu.RUnlock()

	if !exists {
		log.Printf("[StateManager]UpdateStatus, requestID=%d", requestID)
		return false
	}

	state.mu.RLock()
	oldStatus := state.Status
	state.mu.RUnlock()

	state.UpdateStatus(newStatus)

	if newStatus == StatusCompleted {
		m.mu.Lock()
		m.completedRequests++
		m.mu.Unlock()
	} else if newStatus == StatusFailed {
		m.mu.Lock()
		m.failedRequests++
		m.mu.Unlock()
	}

	log.Infof("[StateManager]UpdateStatus, requestID=%d, oldStatus=%s -> newStatus=%s", requestID, oldStatus, newStatus)
	return true
}

func (m *RequestStateManager) RemoveState(requestID uint32) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, exists := m.states[requestID]
	if !exists {
		return false
	}

	delete(m.states, requestID)
	m.activeRequests--

	log.Infof("[StateManager]RemoveState, requestID=%d", requestID)
	return true
}

func (m *RequestStateManager) cleanupExpiredStates() {
	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCleanup:
			return
		case <-ticker.C:
			m.doCleanup()
		}
	}
}

func (m *RequestStateManager) doCleanup() {
	now := time.Now()
	expiredIDs := make([]uint32, 0)

	m.mu.RLock()
	for id, state := range m.states {
		state.mu.RLock()
		lastUpdated := state.LastUpdatedAt
		isFinished := state.Status == StatusCompleted || state.Status == StatusFailed
		state.mu.RUnlock()

		if (isFinished && now.Sub(lastUpdated) > m.expiration) ||
			(now.Sub(lastUpdated) > 2*m.expiration) {
			expiredIDs = append(expiredIDs, id)
		}
	}
	m.mu.RUnlock()

	if len(expiredIDs) > 0 {
		for _, id := range expiredIDs {
			m.RemoveState(id)
		}
		log.Infof("[StateManager]doCleanup, lenexpiredIDs=%d", len(expiredIDs))
	}
}

func (m *RequestStateManager) GetStats() (total, active, completed, failed int64) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.totalRequests, m.activeRequests, m.completedRequests, m.failedRequests
}

func (m *RequestStateManager) GetStateCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.states)
}

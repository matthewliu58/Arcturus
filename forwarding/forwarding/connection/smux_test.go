package connection

import (
	"github.com/xtaci/smux"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func createPipeSessionPair(t *testing.T) (*smux.Session, *smux.Session) {
	clientConn, serverConn := net.Pipe()

	var clientSession, serverSession *smux.Session
	var clientErr, serverErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		clientSession, clientErr = smux.Client(clientConn, DefaultSmuxConfig())
	}()

	go func() {
		defer wg.Done()
		serverSession, serverErr = smux.Server(serverConn, DefaultSmuxConfig())
	}()

	wg.Wait()

	if clientErr != nil {
		t.Fatalf(": %v", clientErr)
	}
	if serverErr != nil {
		t.Fatalf(": %v", serverErr)
	}

	return clientSession, serverSession
}

func resetSessionPools() {
	clientSessionPool = &SmuxSessionPool{
		sessions: make(map[string][]*smux.Session),
	}
	serverSessionPool = &SmuxSessionPool{
		sessions: make(map[string][]*smux.Session),
	}
	atomic.StoreUint64(&sessionCounter, 0)
}

func TestDefaultSmuxConfig(t *testing.T) {
	config := DefaultSmuxConfig()

	if config == nil {
		t.Fatal("DefaultSmuxConfignil")
	}

	if config.KeepAliveInterval != 10*time.Second {
		t.Errorf("KeepAliveInterval10，%v", config.KeepAliveInterval)
	}

	if config.KeepAliveTimeout != 30*time.Second {
		t.Errorf("KeepAliveTimeout30，%v", config.KeepAliveTimeout)
	}

	if config.MaxFrameSize != 32768 {
		t.Errorf("MaxFrameSize32768，%v", config.MaxFrameSize)
	}
}

func TestSessionPool_AddRemove(t *testing.T) {
	resetSessionPools()

	clientSession, serverSession := createPipeSessionPair(t)
	defer clientSession.Close()
	defer serverSession.Close()

	clientAddr := "probing_client.example.com:9000"
	clientSessionPool.mu.Lock()
	clientSessionPool.sessions[clientAddr] = append(clientSessionPool.sessions[clientAddr], clientSession)
	clientSessionPool.mu.Unlock()

	clientSessionPool.mu.RLock()
	count := len(clientSessionPool.sessions[clientAddr])
	clientSessionPool.mu.RUnlock()

	if count != 1 {
		t.Errorf("，1，%d", count)
	}

	clientSessionPool.mu.Lock()
	sessions := clientSessionPool.sessions[clientAddr]
	for i, session := range sessions {
		if session == clientSession {
			clientSessionPool.sessions[clientAddr] = append(sessions[:i], sessions[i+1:]...)
			break
		}
	}
	clientSessionPool.mu.Unlock()

	clientSessionPool.mu.RLock()
	count = len(clientSessionPool.sessions[clientAddr])
	clientSessionPool.mu.RUnlock()

	if count != 0 {
		t.Errorf("，0，%d", count)
	}

	serverAddr := "controller.example.com:8000"
	serverSessionPool.mu.Lock()
	serverSessionPool.sessions[serverAddr] = append(serverSessionPool.sessions[serverAddr], serverSession)
	serverSessionPool.mu.Unlock()

	serverSessionPool.mu.RLock()
	count = len(serverSessionPool.sessions[serverAddr])
	serverSessionPool.mu.RUnlock()

	if count != 1 {
		t.Errorf("，1，%d", count)
	}
}

func TestGetServerSession(t *testing.T) {
	resetSessionPools()

	remoteAddr := "probing_client.example.com:8000"

	_, err := GetServerSession(remoteAddr)
	if err == nil {
		t.Error("，nil")
	}

	clientSession, serverSession := createPipeSessionPair(t)
	defer clientSession.Close()
	defer serverSession.Close()

	serverSessionPool.mu.Lock()
	serverSessionPool.sessions[remoteAddr] = append(serverSessionPool.sessions[remoteAddr], serverSession)
	serverSessionPool.mu.Unlock()

	session, err := GetServerSession(remoteAddr)
	if err != nil {
		t.Errorf("，：%v", err)
	}
	if session == nil {
		t.Error("nil")
	}
}

func TestCloseAllSessions(t *testing.T) {
	resetSessionPools()

	clientSession, serverSession := createPipeSessionPair(t)

	clientAddr := "controller.example.com:9000"
	serverAddr := "probing_client.example.com:8000"

	clientSessionPool.mu.Lock()
	clientSessionPool.sessions[clientAddr] = append(clientSessionPool.sessions[clientAddr], clientSession)
	clientSessionPool.mu.Unlock()

	serverSessionPool.mu.Lock()
	serverSessionPool.sessions[serverAddr] = append(serverSessionPool.sessions[serverAddr], serverSession)
	serverSessionPool.mu.Unlock()

	CloseAllSessions()

	clientSessionPool.mu.RLock()
	clientCount := len(clientSessionPool.sessions)
	clientSessionPool.mu.RUnlock()

	serverSessionPool.mu.RLock()
	serverCount := len(serverSessionPool.sessions)
	serverSessionPool.mu.RUnlock()

	if clientCount != 0 || serverCount != 0 {
		t.Errorf("，，%d，%d",
			clientCount, serverCount)
	}

	if !clientSession.IsClosed() {
		t.Error("")
	}
	if !serverSession.IsClosed() {
		t.Error("")
	}
}

func TestSessionStreamOperations(t *testing.T) {

	clientSession, serverSession := createPipeSessionPair(t)
	defer clientSession.Close()
	defer serverSession.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		stream, err := serverSession.AcceptStream()
		if err != nil {
			t.Errorf(": %v", err)
			return
		}
		defer stream.Close()

		buf := make([]byte, 10)
		n, err := stream.Read(buf)
		if err != nil {
			t.Errorf(": %v", err)
			return
		}

		expected := "hello smux"
		if string(buf[:n]) != expected {
			t.Errorf("，:%s, :%s", expected, string(buf[:n]))
		}
	}()

	go func() {
		defer wg.Done()
		stream, err := clientSession.OpenStream()
		if err != nil {
			t.Errorf(": %v", err)
			return
		}
		defer stream.Close()

		data := []byte("hello smux")
		_, err = stream.Write(data)
		if err != nil {
			t.Errorf(": %v", err)
		}
	}()

	wg.Wait()
}

func TestLoadBalancing(t *testing.T) {
	resetSessionPools()

	targetAddr := "test.example.com:9000"
	sessionCount := 3

	sessionIDs := make(map[int]*smux.Session)
	idToSession := make(map[*smux.Session]int)

	for i := 0; i < sessionCount; i++ {
		clientSession, _ := createPipeSessionPair(t)
		sessionID := i + 1
		sessionIDs[sessionID] = clientSession
		idToSession[clientSession] = sessionID

		clientSessionPool.mu.Lock()
		clientSessionPool.sessions[targetAddr] = append(clientSessionPool.sessions[targetAddr], clientSession)
		clientSessionPool.mu.Unlock()
	}

	selectionCounts := make(map[int]int)
	totalCalls := 90

	for i := 0; i < totalCalls; i++ {

		clientSessionPool.mu.RLock()
		sessions := clientSessionPool.sessions[targetAddr]
		var validSessions []*smux.Session
		for _, session := range sessions {
			if session != nil && !session.IsClosed() {
				validSessions = append(validSessions, session)
			}
		}
		clientSessionPool.mu.RUnlock()

		index := atomic.AddUint64(&sessionCounter, 1) % uint64(len(validSessions))
		selectedSession := validSessions[index]

		sessionID := idToSession[selectedSession]
		selectionCounts[sessionID]++
	}

	expectedPerSession := totalCalls / sessionCount
	tolerance := expectedPerSession / 5

	for id, count := range selectionCounts {
		if count < expectedPerSession-tolerance || count > expectedPerSession+tolerance {
			t.Errorf(":  #%d  %d ， %d  (±%d)",
				id, count, expectedPerSession, tolerance)
		}
	}

	for _, session := range sessionIDs {
		session.Close()
	}
}

func TestClientSessionSelection(t *testing.T) {
	resetSessionPools()

	targetAddr := "test.example.com:9000"

	for i := 0; i < 3; i++ {
		clientSession, _ := createPipeSessionPair(t)
		clientSessionPool.mu.Lock()
		clientSessionPool.sessions[targetAddr] = append(clientSessionPool.sessions[targetAddr], clientSession)
		clientSessionPool.mu.Unlock()
	}

	clientSessionPool.mu.RLock()
	sessions := clientSessionPool.sessions[targetAddr]
	var validSessions []*smux.Session
	for _, session := range sessions {
		if session != nil && !session.IsClosed() {
			validSessions = append(validSessions, session)
		}
	}
	clientSessionPool.mu.RUnlock()

	if len(validSessions) != 3 {
		t.Errorf("3，%d", len(validSessions))
	}

	sessionMap := make(map[*smux.Session]bool)
	for i := 0; i < 3; i++ {
		index := atomic.AddUint64(&sessionCounter, 1) % uint64(len(validSessions))
		session := validSessions[index]
		sessionMap[session] = true
	}

	if len(sessionMap) < 2 {
		t.Error("2，1")
	}

	for _, session := range validSessions {
		session.Close()
	}
}

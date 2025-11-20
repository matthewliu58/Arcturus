package connection

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/xtaci/smux"
	"sync"
	"sync/atomic"
	"time"
)

type SmuxSessionPool struct {
	sessions map[string][]*smux.Session
	mu       sync.RWMutex
}

var (
	clientSessionPool = &SmuxSessionPool{
		sessions: make(map[string][]*smux.Session),
	}
	serverSessionPool = &SmuxSessionPool{
		sessions: make(map[string][]*smux.Session),
	}

	sessionCounter uint64 = 0
)

func DefaultSmuxConfig() *smux.Config {
	return &smux.Config{
		Version:           1,
		KeepAliveInterval: 5 * time.Second,
		KeepAliveTimeout:  30 * time.Second,
		MaxFrameSize:      65535,
		MaxReceiveBuffer:  4194304,
		MaxStreamBuffer:   131072,
	}
}

func GetOrCreateClientSession(targetAddr string, config ...*smux.Config) (*smux.Session, error) {

	clientSessionPool.mu.RLock()
	sessions := clientSessionPool.sessions[targetAddr]

	var validSessions []*smux.Session
	for _, session := range sessions {
		if session != nil && !session.IsClosed() {
			validSessions = append(validSessions, session)
		}
	}

	if len(validSessions) > 0 {

		index := atomic.AddUint64(&sessionCounter, 1) % uint64(len(validSessions))
		session := validSessions[index]
		clientSessionPool.mu.RUnlock()
		return session, nil
	}
	clientSessionPool.mu.RUnlock()

	pool, err := GetOrCreatePool(targetAddr)
	if err != nil {
		return nil, fmt.Errorf(": %v", err)
	}

	conn, err := (*pool).Get()
	if err != nil {
		return nil, fmt.Errorf(": %v", err)
	}

	var smuxConfig *smux.Config
	if len(config) > 0 && config[0] != nil {
		smuxConfig = config[0]
	} else {
		smuxConfig = DefaultSmuxConfig()
	}

	session, err := smux.Client(conn, smuxConfig)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SMUX: %v", err)
	}

	clientSessionPool.mu.Lock()

	updatedSessions := clientSessionPool.sessions[targetAddr]
	newSessions := make([]*smux.Session, 0)
	for _, s := range updatedSessions {
		if s != nil && !s.IsClosed() {
			newSessions = append(newSessions, s)
		} else if s != nil {

			s.Close()
		}
	}

	newSessions = append(newSessions, session)
	clientSessionPool.sessions[targetAddr] = newSessions
	clientSessionPool.mu.Unlock()

	log.Infof("SMUX, targetAddr=%s", targetAddr)
	return session, nil
}

func RemoveClientSession(targetAddr string, sessionToRemove *smux.Session) {
	if sessionToRemove == nil {
		return
	}

	clientSessionPool.mu.Lock()
	defer clientSessionPool.mu.Unlock()

	sessions := clientSessionPool.sessions[targetAddr]
	for i, session := range sessions {
		if session == sessionToRemove {

			if !session.IsClosed() {
				session.Close()
			}
			clientSessionPool.sessions[targetAddr] = append(sessions[:i], sessions[i+1:]...)
			log.Printf(" %s ", targetAddr)
			break
		}
	}

	if len(clientSessionPool.sessions[targetAddr]) == 0 {
		delete(clientSessionPool.sessions, targetAddr)
	}
}

func AddServerSession(remoteAddr string, session *smux.Session, config ...*smux.Config) {
	if session == nil {
		return
	}

	serverSessionPool.mu.Lock()
	defer serverSessionPool.mu.Unlock()

	sessions := serverSessionPool.sessions[remoteAddr]
	validSessions := make([]*smux.Session, 0)
	for _, s := range sessions {
		if s != nil && !s.IsClosed() {
			validSessions = append(validSessions, s)
		} else if s != nil {
			s.Close()
		}
	}

	validSessions = append(validSessions, session)
	serverSessionPool.sessions[remoteAddr] = validSessions
	log.Infof("AddServerSession, remoteAddr=%s", remoteAddr)
}

func RemoveServerSession(remoteAddr string, session *smux.Session) {
	if session == nil {
		return
	}

	serverSessionPool.mu.Lock()
	defer serverSessionPool.mu.Unlock()

	sessions := serverSessionPool.sessions[remoteAddr]
	for i, s := range sessions {
		if s == session {

			if !s.IsClosed() {
				s.Close()
			}
			serverSessionPool.sessions[remoteAddr] = append(sessions[:i], sessions[i+1:]...)
			log.Printf(" %s ", remoteAddr)
			break
		}
	}

	if len(serverSessionPool.sessions[remoteAddr]) == 0 {
		delete(serverSessionPool.sessions, remoteAddr)
	}
}

func GetServerSession(remoteAddr string) (*smux.Session, error) {
	serverSessionPool.mu.RLock()
	defer serverSessionPool.mu.RUnlock()

	sessions := serverSessionPool.sessions[remoteAddr]
	var validSessions []*smux.Session

	for _, session := range sessions {
		if session != nil && !session.IsClosed() {
			validSessions = append(validSessions, session)
		}
	}

	if len(validSessions) == 0 {
		return nil, fmt.Errorf(": %s", remoteAddr)
	}

	index := atomic.AddUint64(&sessionCounter, 1) % uint64(len(validSessions))
	return validSessions[index], nil
}

func CloseAllSessions() {

	clientSessionPool.mu.Lock()
	for addr, sessions := range clientSessionPool.sessions {
		for _, session := range sessions {
			if session != nil && !session.IsClosed() {
				session.Close()
			}
		}
		delete(clientSessionPool.sessions, addr)
	}
	clientSessionPool.mu.Unlock()

	serverSessionPool.mu.Lock()
	for addr, sessions := range serverSessionPool.sessions {
		for _, session := range sessions {
			if session != nil && !session.IsClosed() {
				session.Close()
			}
		}
		delete(serverSessionPool.sessions, addr)
	}
	serverSessionPool.mu.Unlock()

	log.Infof("SMUX,CloseAllSessions")
}

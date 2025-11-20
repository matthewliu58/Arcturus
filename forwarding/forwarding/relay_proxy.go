package forwarding

import (
	"bufio"
	"bytes"
	"fmt"
	"forwarding/forwarding/connection"
	packet "forwarding/packet_handler"
	log "github.com/sirupsen/logrus"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/xtaci/smux"
)

type ResponseData struct {
	RequestID uint32
	Data      []byte
	HopList   []uint32 // HopList of the original request, used to route response back
}

type RelayRepositoryConfig struct {
	ProcessingInterval time.Duration
	RequestBufferSize  int
	ResponseBufferSize int
}

var DefaultRelayRepositoryConfig = RelayRepositoryConfig{
	ProcessingInterval: 10 * time.Millisecond,
	RequestBufferSize:  100,
	ResponseBufferSize: 100,
}

type RelayRepository struct {
	requestChan  chan *RelayRequestItem  // Channel for incoming relay requests
	responseChan chan *RelayResponseItem // Channel for incoming relay responses (from next hop or target)

	done chan struct{}  // Channel to signal goroutines to stop
	wg   sync.WaitGroup // WaitGroup to wait for goroutines to finish

	config      RelayRepositoryConfig
	relayConfig RelayConfig

	bufferManager *BufferManager
	stateManager  *RequestStateManager
}

type RelayRequestItem struct {
	Data       []byte       // Raw data received, includes header and payload
	Stream     *smux.Stream // SMUX stream from which the request was read, used for sending response if this is the first hop from access
	ReceivedAt time.Time
	RemoteAddr string // Network address of the sender
}

type RelayResponseItem struct {
	Data       []byte // Raw data received, includes header and payload
	ReceivedAt time.Time
	RemoteAddr string // Network address of the sender
}

type RelayConfig struct {
	RequestPort  string // Port for listening to incoming requests from other relays or access points (e.g., "50056")
	ResponsePort string // Port for listening to incoming responses from other relays or target services (e.g., "50057")

	RelayPort  string // Default port to connect to for sending requests to the next relay (e.g., "50056")
	SourcePort string // Default port to connect to for sending requests to the target source/origin server (e.g., "8080")

	AccessResponsePort string // Port used by Access points to listen for responses (e.g., "50054")
	RelayResponsePort  string // Port used by Relay points to listen for responses (e.g., "50057")
}

var DefaultRelayConfig = RelayConfig{
	RequestPort:        "50056",
	ResponsePort:       "50057",
	RelayPort:          "50056",
	SourcePort:         "8080",
	AccessResponsePort: "50054",
	RelayResponsePort:  "50057",
}

type RelayProxy struct {
	repository *RelayRepository
	config     RelayConfig
}

func CreateRelayProxy(relayConfig RelayConfig, repoConfig RelayRepositoryConfig) *RelayProxy {
	log.Infof("[Relay-INFO] Creating RelayProxy with RelayConfig ports: Req=%s, Resp=%s, NextRelay=%s, Source=%s; "+
		"RepoConfig: ReqBuf=%d, RespBuf=%d", relayConfig.RequestPort, relayConfig.ResponsePort, relayConfig.RelayPort,
		relayConfig.SourcePort, repoConfig.RequestBufferSize, repoConfig.ResponseBufferSize)

	stateManager := NewRequestStateManager(15*time.Minute, 1*time.Minute) // TODO: Make these configurable

	repo := &RelayRepository{
		requestChan:  make(chan *RelayRequestItem, repoConfig.RequestBufferSize),
		responseChan: make(chan *RelayResponseItem, repoConfig.ResponseBufferSize),
		done:         make(chan struct{}),
		config:       repoConfig,
		relayConfig:  relayConfig,
		stateManager: stateManager,
	}

	bufferConfig := DefaultBufferConfig() // Assuming DefaultBufferConfig is suitable
	repo.bufferManager = NewBufferManager(bufferConfig, stateManager)

	// Set send functions for the buffer manager
	repo.bufferManager.SetSendFunctions(
		repo.sendSingleRequest,
		repo.sendMergedRequest,
		repo.forwardResponseToPreviousHop, // This function is specific to RelayProxy for routing responses back along the path
	)

	log.Infof("[Relay-INFO] RelayProxy repository created and configured.")
	return &RelayProxy{
		repository: repo,
		config:     relayConfig,
	}
}

func (r *RelayRepository) StartProcessors() {
	r.wg.Add(2) // For processRequests and processResponses goroutines

	go r.processRequests()
	go r.processResponses()

	log.Infof("[RelayRepository-INFO] Started request and response processors.")
}

func (r *RelayRepository) Stop() {
	log.Infof("[RelayRepository-INFO] Stopping repository processors...")
	close(r.done) // Signal goroutines to stop
	r.wg.Wait()   // Wait for processor goroutines to finish

	if r.bufferManager != nil {
		log.Infof("[RelayRepository-INFO] Stopping BufferManager.")
		r.bufferManager.Stop()
	}

	if r.stateManager != nil {
		log.Infof("[RelayRepository-INFO] Stopping RequestStateManager.")
		r.stateManager.Stop()
	}

	log.Infof("[RelayRepository-INFO] All repository processors stopped.")
}

func (r *RelayRepository) processRequests() {
	defer r.wg.Done()

	workerCount := runtime.NumCPU() * 2
	if workerCount < 4 {
		workerCount = 4 // Ensure a minimum number of workers
	}

	log.Infof("[RelayRepository-INFO] Starting request processing dispatcher with %d worker goroutines.", workerCount)

	for i := 0; i < workerCount; i++ {
		go func(workerID int) {
			log.Debugf("[RelayRepository-DEBUG] Request Worker #%d started.", workerID)
			for {
				select {
				case <-r.done:
					log.Infof("[RelayRepository-INFO] Request Worker #%d stopping as done signal received.", workerID)
					return
				case req, ok := <-r.requestChan:
					if !ok {
						log.Warningf("[RelayRepository-WARN] Request Worker #%d: requestChan closed, exiting.", workerID)
						return
					}
					log.Debugf("[Relay-DEBUG] Worker #%d received relay request from %s, data size: %d bytes. "+
						"Processing with target routing.", workerID, req.RemoteAddr, len(req.Data))
					// Each request is processed in its own goroutine to avoid blocking the worker
					go r.processRequestWithTargetRouting(req.Data, req.Stream, req.RemoteAddr)
				}
			}
		}(i)
	}

	<-r.done // Wait for the done signal to stop the dispatcher itself
	log.Infof("[RelayRepository-INFO] Request processing dispatcher stopped.")
}

func (r *RelayRepository) processRequestWithTargetRouting(data []byte, responseStream *smux.Stream, remoteAddr string) {
	log.Debugf("[Relay-DEBUG] Processing request from %s, data size: %d bytes.", remoteAddr, len(data))

	if len(data) < 4 { // Assuming a minimum header size of 4 bytes if packet.MinHeaderSize is not defined
		log.Errorf("[Relay-ERROR] Received data from %s is too short (%d bytes) to contain a valid header.",
			remoteAddr, len(data))
		return
	}

	// Assuming header length is at bytes 2 and 3 (0-indexed)
	headerLen := uint16(data[2])<<8 | uint16(data[3])
	if int(headerLen) > len(data) || int(headerLen) < 4 { // Additional check for headerLen sanity, using 4 as min header size
		log.Errorf("[Relay-ERROR] Invalid header length %d parsed from data (total data size: %d bytes) from %s. "+
			"Header length too large or too small.", headerLen, len(data), remoteAddr)
		return
	}

	headerBytes := data[:headerLen]
	requestPayloadBytes := data[headerLen:] // Renamed from requestBytes to avoid confusion with overall data

	header, err := packet.Unpack(headerBytes)
	if err != nil {
		log.Errorf("[Relay-ERROR] Failed to unpack packet header from %s: %v. Header bytes (first %d): %x",
			remoteAddr, err, min(len(headerBytes), 32), headerBytes[:min(len(headerBytes), 32)])
		return
	}
	log.Infof("[Relay-INFO] Received request from %s. Header: PacketCount=%d, IDs=%v, HopCounts=%d, "+
		"Current HopList: %v", remoteAddr, header.PacketCount, header.PacketID, header.HopCounts, header.HopList)

	header.IncrementHopCounts()
	log.Infof("[Relay-INFO] Incremented HopCounts to %d for request(s) %v.", header.HopCounts, header.PacketID)

	nextHopIP, isLastHop, err := header.GetNextHopIP()
	if err != nil {
		log.Errorf("[Relay-ERROR] Failed to determine next hop IP for request(s) %v (HopCounts: %d): %v. Header: %+v",
			header.PacketID, header.HopCounts, err, header)
		// Cannot proceed without a next hop, so we return. Consider sending an error response if possible/applicable.
		return
	}
	log.Infof("[Relay-INFO] Determined next hop for request(s) %v: %s. IsLastHop: %v",
		header.PacketID, nextHopIP, isLastHop)

	var requestStates []*RequestState

	if header.PacketCount > 1 {
		log.Debugf("[Relay-DEBUG] Processing %d merged requests from %s. Request IDs: %v",
			header.PacketCount, remoteAddr, header.PacketID)
		positions := packet.GetRequestPositions(header, len(requestPayloadBytes))

		for i := 0; i < int(header.PacketCount); i++ {
			requestID := header.PacketID[i]
			// Boundary check for positions to prevent panic
			if positions[i] > positions[i+1] || positions[i+1] > len(requestPayloadBytes) {
				log.Errorf("[Relay-ERROR] Invalid packet positions [%d:%d] for request ID %d in merged request. "+
					"Payload size %d. From %s.", positions[i], positions[i+1], requestID, len(requestPayloadBytes), remoteAddr)
				continue // Skip this invalid part of the merged request
			}
			reqData := requestPayloadBytes[positions[i]:positions[i+1]]
			log.Debugf("[Relay-DEBUG] Extracted individual request ID %d from merged packet. Size: %d bytes.",
				requestID, len(reqData))

			reqState := &RequestState{
				RequestID:        requestID,
				OriginalRequest:  nil, // In relay, we don't typically deal with the original *http.Request
				ResponseWriter:   nil, // Not applicable for relay node directly unless it's the final hop for an HTTP request
				RequestData:      reqData,
				Size:             len(reqData),
				Status:           StatusCreated, // Initial status
				CreatedAt:        time.Now(),
				LastUpdatedAt:    time.Now(),
				NextHopIP:        nextHopIP,
				HopList:          header.HopList, // The full hop list from the received packet
				IsLastHop:        isLastHop,
				ResponseReceived: make(chan struct{}), // Channel to signal response arrival for this specific request ID
				BufferID:         "",                  // Will be set by BufferManager if used
				MergeGroupID:     0,                   // Will be set by BufferManager if used
				UpdatedHeader:    header,              // Store the potentially modified header (e.g., incremented HopCounts)
			}

			r.stateManager.AddState(reqState) // StateManager logs this addition
			requestStates = append(requestStates, reqState)
		}
	} else if header.PacketCount == 1 {
		requestID := header.PacketID[0]
		log.Debugf("[Relay-DEBUG] Processing single request ID %d from %s. Size: %d bytes.",
			requestID, remoteAddr, len(requestPayloadBytes))
		reqState := &RequestState{
			RequestID:        requestID,
			OriginalRequest:  nil,
			ResponseWriter:   nil,
			RequestData:      requestPayloadBytes, // Entire payload is for this single request
			Size:             len(requestPayloadBytes),
			Status:           StatusCreated,
			CreatedAt:        time.Now(),
			LastUpdatedAt:    time.Now(),
			NextHopIP:        nextHopIP,
			HopList:          header.HopList,
			IsLastHop:        isLastHop,
			ResponseReceived: make(chan struct{}),
			BufferID:         "",
			MergeGroupID:     0,
			UpdatedHeader:    header,
		}
		r.stateManager.AddState(reqState)
		requestStates = append(requestStates, reqState)
	} else {
		log.Errorf("[Relay-ERROR] Received packet from %s with PacketCount = 0. Header: %+v. Discarding.",
			remoteAddr, header)
		return
	}

	// (isLastHop)
	if isLastHop {
		log.Infof("[Relay-INFO] Request(s) %v from %s: This is the last hop. Processing directly (e.g., to source server)."+
			" PacketCount: %d", header.PacketID, remoteAddr, header.PacketCount)

		var wg sync.WaitGroup
		// Process each request state (can be multiple if it was a merged request)
		for i, reqState := range requestStates {
			wg.Add(1)
			go func(state *RequestState, idx int) {
				defer wg.Done()
				log.Debugf("[Relay-DEBUG] Last hop processing for Request ID %d (index %d in batch).",
					state.RequestID, idx)

				// This assumes handleSingleDirectRequest makes an HTTP request to the final destination
				respData, err := r.handleSingleDirectRequest(state)
				if err != nil {
					// Error already logged in handleSingleDirectRequest, stateManager status also updated there.
					// No need to call r.stateManager.UpdateStatus here again if handleSingleDirectRequest does it.
					log.Errorf("[Relay-ERROR] Last hop: Error handling direct request for ID %d: %v", state.RequestID, err)
					return // Error response should have been handled or logged by handleSingleDirectRequest or state manager.
				}

				// If handleSingleDirectRequest is successful, it returns response data to be sent back.
				// This response data needs to be processed by the buffer manager to be sent to the previous hop.
				err = r.bufferManager.ProcessResponse(respData) // ProcessResponse will use forwardResponseToPreviousHop
				if err != nil {
					log.Errorf("[Relay-ERROR] Last hop: BufferManager failed to process response for Request ID %d: %v",
						state.RequestID, err)
					// If ProcessResponse fails, the response might not be sent back.
					// Consider if state needs an update here or if it's handled by ProcessResponse/underlying send functions.
				}

				log.Infof("[Relay-INFO] Last hop: Successfully processed and queued response for Request ID %d (index %d).", state.RequestID, idx)
			}(reqState, i)
		}

		wg.Wait() // Wait for all direct requests in the batch to be handled
		log.Infof("[Relay-INFO] Last hop: All %d direct requests from %s processed.", len(requestStates), remoteAddr)
	} else {
		// Not the last hop, so forwarding to the next relay node
		log.Infof("[Relay-INFO] Request(s) %v from %s: Not the last hop. Forwarding to next hop: %s. PacketCount: %d",
			header.PacketID, remoteAddr, nextHopIP, header.PacketCount)

		updatedHeaderBytes, err := header.Pack() // Header has HopCounts incremented
		if err != nil {
			log.Errorf("[Relay-ERROR] Failed to pack updated header for forwarding (Request IDs: %v from %s): %v",
				header.PacketID, remoteAddr, err)
			// Mark states as failed if we cannot pack the header
			for _, rs := range requestStates {
				r.stateManager.UpdateStatus(rs.RequestID, StatusFailed)
			}
			return
		}

		// The payload is requestPayloadBytes which was extracted earlier.
		// The original `data` contained the old header, so we use `requestPayloadBytes` with the `updatedHeaderBytes`.
		mergedForwardData := append(updatedHeaderBytes, requestPayloadBytes...)
		log.Debugf("[Relay-DEBUG] Forwarding data to %s. Header size: %d, Payload size: %d, Total size: %d.",
			nextHopIP, len(updatedHeaderBytes), len(requestPayloadBytes), len(mergedForwardData))

		err = r.forwardToNextHop(mergedForwardData, nextHopIP, header, isLastHop)
		if err != nil {
			log.Errorf("[Relay-ERROR] Failed to forwarding request(s) %v from %s to next hop %s: %v",
				header.PacketID, remoteAddr, nextHopIP, err)
			// Mark states as failed if forwarding fails
			for _, rs := range requestStates {
				r.stateManager.UpdateStatus(rs.RequestID, StatusFailed)
			}
		} else {
			log.Infof("[Relay-INFO] Successfully forwarded request(s) %v from %s to next hop %s.",
				header.PacketID, remoteAddr, nextHopIP)
			// Update status to Sent for all individual requests after successful forwarding to next hop
			for _, rs := range requestStates {
				r.stateManager.UpdateStatus(rs.RequestID, StatusSent)
			}
		}
	}
}

// forwardToNextHop sends data (could be single or merged requests) to the specified nextHopIP.
// The `header` argument is the updated header (e.g. with incremented HopCounts).
// `isNextHopTheActualLastHop` indicates if `nextHopIP` is the final destination in the chain.
func (r *RelayRepository) forwardToNextHop(dataToSend []byte, nextHopIP string, updatedHeader *packet.Packet,
	isNextHopTheActualLastHop bool) error {
	log.Debugf("[Relay-DEBUG] Forwarding data. NextHopIP: %s, IsFinalDest: %v, Data size: %d bytes.",
		nextHopIP, isNextHopTheActualLastHop, len(dataToSend))

	parts := strings.Split(nextHopIP, ":")
	ip := parts[0]
	var port string
	var targetDescription string

	if len(parts) > 1 {
		port = parts[1]
		targetDescription = "Relay (specific port)"
	} else {
		// If port is not in nextHopIP, determine if the *ultimate* destination is a source or another relay
		// This logic seems to assume `originalReceivedHeader.GetNextHopIP()` would tell us about the hop *after* `nextHopIP`.
		// However, `originalReceivedHeader` is from *before* `nextHopIP`.
		// A more direct way: if `nextHopIP` itself is the last hop in the `originalReceivedHeader.HopList` (after current node).
		// For now, let's assume the existing logic's intent based on SourcePort/RelayPort.
		// The `isLastHop` here should refer to whether `nextHopIP` is the final destination in the *entire chain*.
		// We can infer this by checking if `nextHopIP` is the last one in `originalReceivedHeader.HopList` *after our current position*.
		// This is complex. A simpler approach might be if `isLastHop` was passed explicitly for `nextHopIP`.
		// Given the current structure, we rely on the task_dispatching for SourcePort if it *looks* like a final destination.

		// Let's re-evaluate port selection: `originalReceivedHeader` is the header *before* HopCount was incremented.
		// `nextHopIP` is determined from this original header + 1 hop.
		// So, `isNextHopTheActualLastHop` should be based on `originalReceivedHeader` and its `HopList`
		// This is simplified here to use the task_dispatching, assuming if no port, it's either final source or next relay.
		// The existing logic uses `originalReceivedHeader.GetNextHopIP()` again, which is confusing.
		// Correct logic: If `nextHopIP` is the absolute last hop, use `SourcePort`. Otherwise, use `RelayPort`.
		// We can determine if `nextHopIP` is the last from `originalReceivedHeader.IsLastNextHop()` or similar (if exists) OR by looking at HopList.
		// For simplicity, let's assume if port is missing, then:
		// If the *current forwarding action* is to the *overall last hop* then use SourcePort, else RelayPort.
		// This `isLastHop` should come from `originalReceivedHeader.GetNextHopIP()` call for *this specific forwarding step*.

		// We now use the passed 'isNextHopTheActualLastHop' parameter

		if isNextHopTheActualLastHop {
			port = r.relayConfig.SourcePort
			targetDescription = "Source (default port)"
		} else {
			port = r.relayConfig.RelayPort
			targetDescription = "Relay (default port)"
		}
	}

	targetAddr := ip + ":" + port
	log.Infof("[Relay-INFO] Determined target address for next hop %s: %s (%s). Updated HopCounts: %d, "+
		"IsNextTheLastInChain: %v", nextHopIP, targetAddr, targetDescription, updatedHeader.HopCounts, isNextHopTheActualLastHop)

	session, err := connection.GetOrCreateClientSession(targetAddr)
	if err != nil {
		log.Errorf("[Relay-ERROR] Failed to get/create SMUX probing_client session for %s target %s: %v",
			targetDescription, targetAddr, err)
		return fmt.Errorf("failed to connect to %s target %s: %w", targetDescription, targetAddr, err)
	}

	stream, err := session.OpenStream()
	if err != nil {
		log.Errorf("[Relay-ERROR] Failed to open SMUX stream for %s target %s (session: %p): %v",
			targetDescription, targetAddr, session, err)
		connection.RemoveClientSession(targetAddr, session) // Attempt to remove potentially bad session
		// Retry logic
		log.Infof("[Relay-INFO] Retrying to establish SMUX connection to %s target %s...",
			targetDescription, targetAddr)
		session, err = connection.GetOrCreateClientSession(targetAddr)
		if err != nil {
			log.Errorf("[Relay-ERROR] Retry failed to get/create SMUX probing_client session for %s target %s: %v",
				targetDescription, targetAddr, err)
			return fmt.Errorf("retry failed to connect to %s target %s: %w", targetDescription, targetAddr, err)
		}
		stream, err = session.OpenStream()
		if err != nil {
			log.Errorf("[Relay-ERROR] Retry failed to open SMUX stream for %s target %s (session: %p): %v",
				targetDescription, targetAddr, session, err)
			return fmt.Errorf("retry failed to open stream to %s target %s: %w", targetDescription, targetAddr, err)
		}
		log.Infof("[Relay-INFO] Successfully established SMUX stream to %s target %s after retry.",
			targetDescription, targetAddr)
	}
	defer stream.Close()

	log.Debugf("[Relay-DEBUG] Writing %d bytes to SMUX stream for %s target %s (stream: %p)",
		len(dataToSend), targetDescription, targetAddr, stream)
	_, err = stream.Write(dataToSend)
	if err != nil {
		log.Errorf("[Relay-ERROR] Failed to write data to SMUX stream for %s target %s (stream: %p): %v",
			targetDescription, targetAddr, stream, err)
		return fmt.Errorf("failed to write data to %s target %s: %w", targetDescription, targetAddr, err)
	}

	log.Infof("[Relay-INFO] Successfully forwarded %d bytes to %s target %s.",
		len(dataToSend), targetDescription, targetAddr)

	return nil
}

// handleSingleDirectRequest processes a request that is at its last hop (i.e., this relay is to send it to the actual target server).
// It assumes the requestData is a full HTTP request.
func (r *RelayRepository) handleSingleDirectRequest(reqState *RequestState) (*ResponseData, error) {
	// StateManager.UpdateStatus is called by the caller or here, ensure consistency.
	// Here, it implies an attempt to send has started.
	r.stateManager.UpdateStatus(reqState.RequestID, StatusSent) // Logged by StateManager

	reqState.mu.RLock()
	requestID := reqState.RequestID
	requestData := reqState.RequestData    // This is the HTTP request bytes
	nextHopIP := reqState.NextHopIP        // This should be the target server IP:Port
	hopListForResponse := reqState.HopList // Original hop list to send back with the response
	reqState.mu.RUnlock()

	log.Infof("[Relay-INFO] Request ID %d: Handling direct request to target %s. Payload size: %d bytes.",
		requestID, nextHopIP, len(requestData))

	reader := bytes.NewReader(requestData)
	bufReader := bufio.NewReader(reader)
	httpReq, err := http.ReadRequest(bufReader) // Parses the raw request bytes into an http.Request
	if err != nil {
		log.Errorf("[Relay-ERROR] Request ID %d: Failed to parse HTTP request from data for target %s: %v",
			requestID, nextHopIP, err)
		r.stateManager.UpdateStatus(requestID, StatusFailed)
		return nil, fmt.Errorf("failed to parse HTTP request for ID %d: %w", requestID, err)
	}
	log.Debugf("[Relay-DEBUG] Request ID %d: Parsed HTTP request: %s %s %s",
		requestID, httpReq.Method, httpReq.Host, httpReq.URL.Path)

	// Determine target host and port from nextHopIP (which is the target server)
	parts := strings.Split(nextHopIP, ":")
	host := parts[0]
	var port string
	if len(parts) > 1 {
		port = parts[1]
	} else {
		// If no port specified in nextHopIP for the target, use the configured SourcePort (e.g., 80 or 8080 for HTTP)
		port = r.relayConfig.SourcePort
		log.Debugf("[Relay-DEBUG] Request ID %d: No port in target %s, using default SourcePort: %s",
			requestID, nextHopIP, port)
	}

	// Construct the target URL for the HTTP probing_client
	// httpReq.URL.Path already contains the path and query params from the original request
	targetURLStr := fmt.Sprintf("http://%s:%s%s", host, port, httpReq.URL.String()) // Use httpReq.URL.String() to preserve query params
	log.Infof("[Relay-INFO] Request ID %d: Constructed target URL for direct request: %s", requestID, targetURLStr)

	destURL, err := url.Parse(targetURLStr)
	if err != nil {
		log.Errorf("[Relay-ERROR] Request ID %d: Failed to parse target URL '%s': %v", requestID, targetURLStr, err)
		r.stateManager.UpdateStatus(requestID, StatusFailed)
		return nil, fmt.Errorf("failed to parse target URL '%s' for ID %d: %w", targetURLStr, requestID, err)
	}

	// Prepare the request for the probing_client.Do call
	httpReq.URL = destURL   // Set the URL to the absolute target URL
	httpReq.RequestURI = "" // Must be empty for probing_client requests
	httpReq.Host = host     // Set the Host header explicitly for the target

	// TODO: Make probing_client timeout configurable
	client := &http.Client{
		Timeout: 30 * time.Second,
		// Potentially configure transport for connection pooling, keep-alives etc.
		// Transport: &http.Transport{...},
	}

	log.Debugf("[Relay-DEBUG] Request ID %d: Sending HTTP %s request to %s", requestID, httpReq.Method, targetURLStr)
	httpResp, err := client.Do(httpReq)
	if err != nil {
		log.Errorf("[Relay-ERROR] Request ID %d: HTTP probing_client failed to execute request to %s: %v",
			requestID, targetURLStr, err)
		r.stateManager.UpdateStatus(requestID, StatusFailed)
		return nil, fmt.Errorf("HTTP probing_client failed for ID %d to %s: %w", requestID, targetURLStr, err)
	}
	defer httpResp.Body.Close()
	log.Infof("[Relay-INFO] Request ID %d: Received HTTP response from %s. Status: %s (%d)",
		requestID, targetURLStr, httpResp.Status, httpResp.StatusCode)

	r.stateManager.UpdateStatus(requestID, StatusResponding) // Logged by StateManager

	respBodyBytes, err := io.ReadAll(httpResp.Body) // Read the entire response body
	if err != nil {
		log.Errorf("[Relay-ERROR] Request ID %d: Failed to read response body from %s: %v", requestID, targetURLStr, err)
		r.stateManager.UpdateStatus(requestID, StatusFailed)
		return nil, fmt.Errorf("failed to read response body for ID %d from %s: %w", requestID, targetURLStr, err)
	}
	log.Tracef("[RESP-TRACE] Direct request ID %d: Target response status: %d, Body size read: %d bytes.",
		requestID, httpResp.StatusCode, len(respBodyBytes))

	// Reconstruct the HTTP response to be sent back through the relay chain
	// This involves creating a new http.Response and writing it to a buffer to get the raw bytes.
	clonedRespForRelay := &http.Response{
		Status:        httpResp.Status, // e.g., "200 OK"
		StatusCode:    httpResp.StatusCode,
		Proto:         httpReq.Proto, // Use the protocol from the incoming request for consistency or httpResp.Proto
		ProtoMajor:    httpReq.ProtoMajor,
		ProtoMinor:    httpReq.ProtoMinor,
		Header:        httpResp.Header.Clone(),                      // Clone headers from target response
		Body:          io.NopCloser(bytes.NewReader(respBodyBytes)), // Use the read body bytes
		ContentLength: int64(len(respBodyBytes)),                    // Set ContentLength explicitly
		Request:       httpReq,                                      // Associate the original request for context (optional, but good practice)
	}

	var rawResponseBuffer bytes.Buffer
	err = clonedRespForRelay.Write(&rawResponseBuffer) // Write the reconstructed response to a buffer
	if err != nil {
		log.Errorf("[Relay-ERROR] Request ID %d: Failed to write reconstructed HTTP response to buffer: %v",
			requestID, err)
		r.stateManager.UpdateStatus(requestID, StatusFailed)
		return nil, fmt.Errorf("failed to write reconstructed response for ID %d: %w", requestID, err)
	}

	r.stateManager.UpdateStatus(requestID, StatusCompleted) // Logged by StateManager

	log.Infof("[Relay-INFO] Request ID %d: Successfully handled direct request to %s. Raw response size for relay: %d bytes.",
		requestID, targetURLStr, rawResponseBuffer.Len())

	// Return the raw HTTP response bytes and the original HopList for routing back
	return &ResponseData{
		RequestID: requestID,
		Data:      rawResponseBuffer.Bytes(),
		HopList:   hopListForResponse, // Use the HopList stored in reqState
	}, nil
}

func (r *RelayRepository) sendSingleRequest(data []byte, nextHopIP string, requestID uint32, request *RequestState) error {
	log.Debugf("[Relay-sendSingleRequest-DEBUG] Attempting to send single request ID %d to NextHopIP: %s. "+
		"Payload data size: %d bytes.", requestID, nextHopIP, len(data))

	parts := bytes.Split([]byte(nextHopIP), []byte(":"))
	ip := string(parts[0])

	var port string
	var portResolutionReason string
	if len(parts) > 1 {
		port = string(parts[1])
		portResolutionReason = "specified in NextHopIP"
	} else {
		var isLastHop bool
		if request != nil {
			request.mu.RLock()
			// IsLastHop here refers to whether the *ultimate* destination for this request state is the final target server
			isLastHop = request.IsLastHop
			request.mu.RUnlock()
		}

		if isLastHop {
			port = r.relayConfig.SourcePort // Use configured SourcePort for the final destination
			portResolutionReason = fmt.Sprintf("isLastHop=true for request ID %d, using configured SourcePort: %s", requestID, port)
		} else {
			port = r.relayConfig.RelayPort // Use configured RelayPort for an intermediate relay
			portResolutionReason = fmt.Sprintf("isLastHop=false for request ID %d, using configured RelayPort: %s", requestID, port)
		}
	}

	targetAddr := ip + ":" + port
	log.Infof("[Relay-sendSingleRequest-INFO] Determined target address for request ID %d: %s. Port resolved via: %s.", requestID, targetAddr, portResolutionReason)

	session, err := connection.GetOrCreateClientSession(targetAddr)
	if err != nil {
		log.Errorf("[Relay-sendSingleRequest-ERROR] Failed to get/create SMUX probing_client session for target %s (request ID %d): %v", targetAddr, requestID, err)
		return err
	}

	stream, err := session.OpenStream()
	if err != nil {
		log.Errorf("[Relay-sendSingleRequest-ERROR] Failed to open SMUX stream for target %s (request ID %d, "+
			"session: %p): %v. Attempting to remove session and retry.", targetAddr, requestID, session, err)
		connection.RemoveClientSession(targetAddr, session) // Attempt to remove potentially bad session

		log.Infof("[Relay-sendSingleRequest-INFO] Retrying to establish SMUX connection to target %s "+
			"for request ID %d...", targetAddr, requestID)
		session, err = connection.GetOrCreateClientSession(targetAddr)
		if err != nil {
			log.Errorf("[Relay-sendSingleRequest-ERROR] Retry failed to get/create SMUX probing_client session for "+
				"target %s (request ID %d): %v", targetAddr, requestID, err)
			return err
		}
		stream, err = session.OpenStream()
		if err != nil {
			log.Errorf("[Relay-sendSingleRequest-ERROR] Retry failed to open SMUX stream for target %s "+
				"(request ID %d, session: %p): %v", targetAddr, requestID, session, err)
			return err
		}
		log.Infof("[Relay-sendSingleRequest-INFO] Successfully established SMUX stream to target %s for "+
			"request ID %d after retry (session: %p, stream: %p).", targetAddr, requestID, session, stream)
	} else {
		log.Debugf("[Relay-sendSingleRequest-DEBUG] Successfully opened SMUX stream to target %s for request ID "+
			"%d (session: %p, stream: %p).", targetAddr, requestID, session, stream)
	}
	defer stream.Close()

	var finalData []byte
	if request != nil {
		request.mu.RLock()
		// Use HopList and HopCounts from the request's UpdatedHeader, which should be set correctly
		// for this forwarding attempt (e.g., HopCounts already incremented).
		originalHopList := request.UpdatedHeader.HopList
		currentHopCounts := request.UpdatedHeader.HopCounts
		request.mu.RUnlock()

		// Construct a new header for this single packet.
		// 'data' is the payload for this specific requestID.
		newHeader := &packet.Packet{
			PacketCount: 1,
			PacketID:    []uint32{requestID},
			HopList:     originalHopList,
			HopCounts:   currentHopCounts,
			// Offsets are not strictly needed for PacketCount = 1, Pack will handle.
		}

		newHeaderBytes, packErr := newHeader.Pack()
		if packErr != nil {
			log.Errorf("[Relay-sendSingleRequest-ERROR] Request ID %d: Failed to pack new header: %v. "+
				"Header details: %+v", requestID, packErr, newHeader)
			// If header packing fails, we might not be able to send correctly.
			// Consider returning error or trying to send payload `data` directly if that's ever intended (unlikely for relays).
			return fmt.Errorf("failed to pack header for request ID %d: %w", requestID, packErr)
		}
		finalData = append(newHeaderBytes, data...) // Prepend new header to the payload 'data'
		log.Debugf("[Relay-sendSingleRequest-DEBUG] Request ID %d: Re-packed single request. New Header HopCounts: %d,"+
			" HopList: %v. Final data size: %d bytes (Header: %d, Payload: %d).",
			requestID, newHeader.HopCounts, newHeader.HopList, len(finalData), len(newHeaderBytes), len(data))

	} else {
		// This case should ideally not happen if sendSingleRequest is always called with a valid request state.
		// If it does, it means we don't have contextual header information.
		finalData = data
		log.Warningf("[Relay-sendSingleRequest-WARN] Request ID %d: Request state is nil. Sending payload data as-is. "+
			"Size: %d bytes. This might be missing a required relay header.", requestID, len(finalData))
	}

	if len(finalData) == 0 {
		log.Errorf("[Relay-sendSingleRequest-ERROR] Request ID %d: finalData to send is empty. Target: %s.",
			requestID, targetAddr)
		return fmt.Errorf("finalData for request ID %d is empty", requestID)
	}

	log.Debugf("[Relay-sendSingleRequest-DEBUG] Writing %d bytes to SMUX stream for request ID %d to target %s "+
		"(stream: %p).", len(finalData), requestID, targetAddr, stream)
	_, err = stream.Write(finalData)
	if err != nil {
		log.Errorf("[Relay-sendSingleRequest-ERROR] Failed to write data for request ID %d to SMUX stream for"+
			" target %s (stream: %p): %v", requestID, targetAddr, stream, err)
		// Note: If write fails, stateManager status is not updated to StatusFailed here, caller might need to handle.
		return err
	}

	if request != nil {
		r.stateManager.UpdateStatus(requestID, StatusSent) // StateManager logs this update
	}

	log.Infof("[Relay-sendSingleRequest-INFO] Successfully sent %d bytes for request ID %d to target %s.",
		len(finalData), requestID, targetAddr)
	return nil
}

func (r *RelayRepository) sendMergedRequest(mergedDataPayload []byte, nextHopIP string, headerForNextHop *packet.Packet) error {
	if headerForNextHop == nil {
		log.Errorf("[Relay-sendMergedRequest-ERROR] Cannot send merged request to NextHopIP %s: "+
			"headerForNextHop is nil.", nextHopIP)
		return fmt.Errorf("headerForNextHop is nil for merged request to %s", nextHopIP)
	}

	log.Debugf("[Relay-sendMergedRequest-DEBUG] Attempting to send merged request to NextHopIP: %s. "+
		"Request IDs: %v, HopCounts: %d. Merged payload size: %d bytes.",
		nextHopIP, headerForNextHop.PacketID, headerForNextHop.HopCounts, len(mergedDataPayload))

	parts := bytes.Split([]byte(nextHopIP), []byte(":"))
	ip := string(parts[0])

	var port string
	var portResolutionReason string
	if len(parts) > 1 {
		port = string(parts[1])
		portResolutionReason = "specified in NextHopIP"
	} else {
		// Use GetNextHopIP on the provided 'headerForNextHop' to determine if the *current* target 'nextHopIP' is the final one in the chain.
		// 'headerForNextHop' should have HopCounts already incremented for this hop.
		_, isNextHopTheActualLastHopInChain, err := headerForNextHop.GetNextHopIP()
		if err != nil {
			log.Errorf("[Relay-sendMergedRequest-ERROR] Failed to determine if next hop %s is the last hop "+
				"using header (IDs: %v, HopCounts: %d): %v. Defaulting to RelayPort %s.",
				nextHopIP, headerForNextHop.PacketID, headerForNextHop.HopCounts, err, r.relayConfig.RelayPort)
			// Fallback to RelayPort if determination fails, though this indicates a potential issue.
			port = r.relayConfig.RelayPort
			portResolutionReason = fmt.Sprintf("error in GetNextHopIP from header, defaulted to RelayPort: %s", port)
		} else if isNextHopTheActualLastHopInChain {
			port = r.relayConfig.SourcePort
			portResolutionReason = fmt.Sprintf("next hop %s is the last hop in chain, using configured SourcePort: %s", nextHopIP, port)
		} else {
			port = r.relayConfig.RelayPort
			portResolutionReason = fmt.Sprintf("next hop %s is an intermediate relay, using configured RelayPort: %s", nextHopIP, port)
		}
	}

	targetAddr := ip + ":" + port
	log.Infof("[Relay-sendMergedRequest-INFO] Determined target address for merged request (IDs: %v): %s. "+
		"Port resolved via: %s.", headerForNextHop.PacketID, targetAddr, portResolutionReason)

	// Pack the provided headerForNextHop (this should be the fully prepared header for this hop).
	packedHeaderBytes, err := headerForNextHop.Pack()
	if err != nil {
		log.Errorf("[Relay-sendMergedRequest-ERROR] Failed to pack header for merged request (IDs: %v) to %s: %v. "+
			"Header details: %+v", headerForNextHop.PacketID, targetAddr, err, headerForNextHop)
		return fmt.Errorf("failed to pack header for merged request to %s: %w", targetAddr, err)
	}

	// For debugging, verify the packed header (optional)
	// _, verifyErr := packet.Unpack(packedHeaderBytes)
	// if verifyErr != nil {
	// 	log.Printf("[Relay-sendMergedRequest-WARN] Verification of packed header failed for merged request (IDs: %v, HopCounts: %d): %v", headerForNextHop.PacketID, headerForNextHop.HopCounts, verifyErr)
	// }

	finalMergedData := append(packedHeaderBytes, mergedDataPayload...)
	log.Debugf("[Relay-sendMergedRequest-DEBUG] Prepared final merged data for %s. Request IDs: %v, "+
		"Header HopCounts: %d. Header size: %d, Payload size: %d, Total size: %d.", targetAddr, headerForNextHop.PacketID,
		headerForNextHop.HopCounts, len(packedHeaderBytes), len(mergedDataPayload), len(finalMergedData))

	session, err := connection.GetOrCreateClientSession(targetAddr)
	if err != nil {
		log.Errorf("[Relay-sendMergedRequest-ERROR] Failed to get/create SMUX probing_client session for target %s "+
			"(merged request IDs: %v): %v", targetAddr, headerForNextHop.PacketID, err)
		return err
	}

	stream, err := session.OpenStream()
	if err != nil {
		log.Errorf("[Relay-sendMergedRequest-ERROR] Failed to open SMUX stream for target %s (merged request IDs: "+
			"%v, session: %p): %v. Attempting to remove session and retry.", targetAddr, headerForNextHop.PacketID, session, err)
		connection.RemoveClientSession(targetAddr, session)

		log.Infof("[Relay-sendMergedRequest-INFO] Retrying SMUX connection to target %s for merged request (IDs: %v)...",
			targetAddr, headerForNextHop.PacketID)
		session, err = connection.GetOrCreateClientSession(targetAddr)
		if err != nil {
			log.Errorf("[Relay-sendMergedRequest-ERROR] Retry failed to get/create SMUX probing_client session for target %s "+
				"(merged request IDs: %v): %v", targetAddr, headerForNextHop.PacketID, err)
			return err
		}
		stream, err = session.OpenStream()
		if err != nil {
			log.Errorf("[Relay-sendMergedRequest-ERROR] Retry failed to open SMUX stream for target %s (merged request "+
				"IDs: %v, session: %p): %v", targetAddr, headerForNextHop.PacketID, session, err)
			return err
		}
		log.Infof("[Relay-sendMergedRequest-INFO] Successfully established SMUX stream to target %s for merged request "+
			"(IDs: %v) after retry (session: %p, stream: %p).", targetAddr, headerForNextHop.PacketID, session, stream)
	} else {
		log.Debugf("[Relay-sendMergedRequest-DEBUG] Successfully opened SMUX stream to target %s for merged request "+
			"(IDs: %v, session: %p, stream: %p).", targetAddr, headerForNextHop.PacketID, session, stream)
	}
	defer stream.Close()

	log.Debugf("[Relay-sendMergedRequest-DEBUG] Writing %d bytes (merged request IDs: %v, HopCounts: %d) to SMUX stream "+
		"for target %s (stream: %p).",
		len(finalMergedData), headerForNextHop.PacketID, headerForNextHop.HopCounts, targetAddr, stream)
	_, err = stream.Write(finalMergedData)
	if err != nil {
		log.Errorf("[Relay-sendMergedRequest-ERROR] Failed to write merged data (IDs: %v) to SMUX stream for target "+
			"%s (stream: %p): %v", headerForNextHop.PacketID, targetAddr, stream, err)
		return err
	}

	// StateManager updates for individual requests within the merged data are typically handled by the caller (e.g., processRequestWithTargetRouting or BufferManager).
	log.Infof("[Relay-sendMergedRequest-INFO] Successfully sent %d bytes for merged request (IDs: %v, HopCounts: %d) to target %s.",
		len(finalMergedData), headerForNextHop.PacketID, headerForNextHop.HopCounts, targetAddr)
	return nil
}

func (r *RelayRepository) forwardResponseToPreviousHop(previousHopIP string, updatedResponseHeaderBytes []byte,
	responsePayloadBytes []byte) error {
	// Unpack the header to log details; this header should have HopCounts decremented.
	header, err := packet.Unpack(updatedResponseHeaderBytes)
	if err != nil {
		log.Errorf("[Relay-forwardResponse-ERROR] Failed to unpack response header for forwarding to %s: %v. "+
			"HeaderBytes (first %d): %x", previousHopIP, err, min(len(updatedResponseHeaderBytes), 32),
			updatedResponseHeaderBytes[:min(len(updatedResponseHeaderBytes), 32)])
		return fmt.Errorf("failed to unpack response header for forwarding: %w", err)
	}

	log.Debugf("[Relay-forwardResponse-DEBUG] Attempting to forwarding response to PreviousHopIP: %s. Request IDs: %v, "+
		"HopCounts in header: %d. Header size: %d, Payload size: %d.",
		previousHopIP, header.PacketID, header.HopCounts, len(updatedResponseHeaderBytes), len(responsePayloadBytes))

	parts := strings.Split(previousHopIP, ":")
	ip := parts[0]

	var port string
	var targetType string // To specify "Access" or "Relay" for logging clarity
	if len(parts) > 1 {
		port = parts[1]
		// If previousHopIP includes a port, it implies a specific port was used by that relay for receiving responses.
		targetType = fmt.Sprintf("Relay Node (specific port %s from PreviousHopIP)", port)
	} else if header.HopCounts == 0 {
		// HopCounts == 0 in a response header means this response is going back to the Access node.
		port = r.relayConfig.AccessResponsePort
		targetType = fmt.Sprintf("Access Node (HopCounts is 0, using AccessResponsePort: %s)", port)
	} else {
		// HopCounts > 0 means the response is going to another Relay node.
		port = r.relayConfig.RelayResponsePort
		targetType = fmt.Sprintf("Relay Node (HopCounts is %d, using RelayResponsePort: %s)", header.HopCounts, port)
	}

	targetAddr := ip + ":" + port
	log.Infof("[Relay-forwardResponse-INFO] Determined target for response: %s (%s). Original Request IDs: %v.",
		targetAddr, targetType, header.PacketID)

	session, err := connection.GetOrCreateClientSession(targetAddr)
	if err != nil {
		log.Errorf("[Relay-forwardResponse-ERROR] Failed to get/create SMUX probing_client session for %s target %s "+
			"(Request IDs: %v): %v", targetType, targetAddr, header.PacketID, err)
		return fmt.Errorf("failed to get/create SMUX probing_client session for %s target %s: %w", targetType, targetAddr, err)
	}
	log.Debugf("[Relay-forwardResponse-DEBUG] Established SMUX session to %s target %s for response (Request IDs: %v, session: %p). PacketCount in response header: %d.",
		targetType, targetAddr, header.PacketID, session, header.PacketCount)

	stream, err := session.OpenStream()
	if err != nil {
		log.Errorf("[Relay-forwardResponse-ERROR] Failed to open SMUX stream for %s target %s (Request IDs: %v, "+
			"session: %p): %v. Attempting to remove session and retry.", targetType, targetAddr, header.PacketID, session, err)
		connection.RemoveClientSession(targetAddr, session)

		log.Infof("[Relay-forwardResponse-INFO] Retrying SMUX connection to %s target %s for response (Request IDs: %v)...", targetType, targetAddr, header.PacketID)
		session, err = connection.GetOrCreateClientSession(targetAddr)
		if err != nil {
			log.Errorf("[Relay-forwardResponse-ERROR] Retry failed to get/create SMUX probing_client session for %s "+
				"target %s (Request IDs: %v): %v", targetType, targetAddr, header.PacketID, err)
			return fmt.Errorf("retry failed for SMUX session to %s target %s: %w", targetType, targetAddr, err)
		}
		stream, err = session.OpenStream()
		if err != nil {
			log.Errorf("[Relay-forwardResponse-ERROR] Retry failed to open SMUX stream for %s target %s (Request "+
				"IDs: %v, session: %p): %v", targetType, targetAddr, header.PacketID, session, err)
			return fmt.Errorf("retry failed for SMUX stream to %s target %s: %w", targetType, targetAddr, err)
		}
		log.Infof("[Relay-forwardResponse-INFO] Successfully established SMUX stream to %s target %s for response "+
			"(Request IDs: %v) after retry (session: %p, stream: %p).", targetType, targetAddr, header.PacketID, session, stream)
	} else {
		log.Debugf("[Relay-forwardResponse-DEBUG] Successfully opened SMUX stream to %s target %s for response "+
			"(Request IDs: %v, session: %p, stream: %p).", targetType, targetAddr, header.PacketID, session, stream)
	}
	defer stream.Close()

	fullResponseData := append(updatedResponseHeaderBytes, responsePayloadBytes...)

	log.Debugf("[Relay-forwardResponse-DEBUG] Writing %d bytes of response data (Header: %d, Payload: %d; Request "+
		"IDs: %v) to SMUX stream for %s target %s (stream: %p).", len(fullResponseData), len(updatedResponseHeaderBytes),
		len(responsePayloadBytes), header.PacketID, targetType, targetAddr, stream)

	_, err = stream.Write(fullResponseData)
	if err != nil {
		log.Errorf("[Relay-forwardResponse-ERROR] Failed to write response data (Request IDs: %v) to SMUX stream "+
			"for %s target %s (stream: %p): %v", header.PacketID, targetType, targetAddr, stream, err)
		return fmt.Errorf("failed to write response data to %s target %s: %w", targetType, targetAddr, err)
	}

	log.Infof("[Relay-forwardResponse-INFO] Successfully forwarded response (Request IDs: %v) to %s target %s. "+
		"TotalSize: %d (Header: %d, Payload: %d). HopCounts in sent header: %d.", header.PacketID, targetType, targetAddr,
		len(fullResponseData), len(updatedResponseHeaderBytes), len(responsePayloadBytes), header.HopCounts)
	return nil
}

func (r *RelayRepository) processResponses() {
	defer r.wg.Done()

	log.Infof("[RelayRepository] processResponses")

	workerCount := runtime.NumCPU() * 2
	if workerCount < 4 {
		workerCount = 4
	}

	for i := 0; i < workerCount; i++ {
		go func(workerID int) {
			for {
				select {
				case <-r.done:

					return
				case resp := <-r.responseChan:

					r.handleResponse(resp.Data, resp.RemoteAddr)
				}
			}
		}(i)
	}

	<-r.done
}

func (r *RelayRepository) handleResponse(data []byte, remoteAddr string) {

	if len(data) < 4 {

		log.Errorf("[Relay-ERROR] handleResponse len(data) < 4")

		return
	}

	headerLen := uint16(data[2])<<8 | uint16(data[3])

	if int(headerLen) > len(data) {
		log.Errorf("[Relay-ERROR] : %d (: %d)", headerLen, len(data))
		return
	}

	headerBytes := data[:headerLen]
	responseBytes := data[headerLen:]

	header, err := packet.Unpack(headerBytes)
	if err != nil {
		log.Errorf("[Relay-ERROR] : %v", err)
		return
	}

	log.Infof("[Relay] ，HopCounts=%d", header.HopCounts)

	header.DecrementHopCounts()
	log.Infof("[Relay] ，HopCounts=%d", header.HopCounts)

	updatedHeaderBytes, err := header.Pack()
	if err != nil {
		log.Errorf("[Relay-ERROR] header: %v", err)
		return
	}

	previousHopIP, _, err := header.GetPreviousHopIP()
	if err != nil {
		log.Errorf("[Relay-ERROR] : %v", err)
		return
	}

	log.Infof("[Relay] : %d，: %s", header.PacketCount, previousHopIP)

	err = r.forwardResponseToPreviousHop(previousHopIP, updatedHeaderBytes, responseBytes)
	if err != nil {
		log.Errorf("[Relay-ERROR] : %v", err)
	}
	return

}

func (r *RelayRepository) getResponseBuffer(pathHash string, requestID uint32) *ResponseBuffer {
	return r.bufferManager.getResponseBuffer(pathHash, requestID)
}

func (r *RelayRepository) mergeAndSendBufferedResponses(responses []*BufferedResponse, previousHopIP string) {
	if len(responses) == 0 {
		return
	}

	log.Infof("[Relay]  %d ", len(responses))

	commonHopList := responses[0].HopList

	respHeader := &packet.Packet{
		PacketCount: byte(len(responses)),
		PacketID:    make([]uint32, len(responses)),
		HopList:     commonHopList,
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
		log.Errorf("[Relay-ERROR] : %v", err)
		return
	}

	var mergedRespData []byte
	for _, respBody := range respBodies {
		mergedRespData = append(mergedRespData, respBody...)
	}

	log.Infof("[Relay] ，=%d ，=%s",
		len(mergedRespData), previousHopIP)

	err = r.forwardResponseToPreviousHop(previousHopIP, headerBytes, mergedRespData)
	if err != nil {
		log.Errorf("[Relay-ERROR] : %v", err)
	}
}

func (r *RelayRepository) StartRequestListener() {

	listenAddr := fmt.Sprintf("0.0.0.0:%s", r.relayConfig.RequestPort)
	log.Infof("[Relay]  %s ", listenAddr)

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("[Relay-FATAL] : %v", err)
		return

	}
	defer listener.Close()

	for {
		select {
		case <-r.done:
			log.Infof("[Relay] ")
			return
		default:
			conn, err := listener.Accept()
			if err != nil {
				log.Errorf("[Relay-ERROR] : %v", err)
				continue
			}

			log.Infof("[Relay]  %s ", conn.RemoteAddr().String())

			go r.handleRequestConnection(conn)
		}

	}
}

func (r *RelayRepository) handleRequestConnection(conn net.Conn) {
	remoteAddr := conn.RemoteAddr().String()
	log.Infof("[Relay]  %s ", remoteAddr)

	session, err := smux.Server(conn, connection.DefaultSmuxConfig())
	if err != nil {

		log.Errorf("[Relay-ERROR] SMUX: %v", err)
		conn.Close()
		return
	}

	connection.AddServerSession(remoteAddr, session)

	log.Infof("[Relay] : %s", remoteAddr)

	go func() {
		for {
			stream, err := session.AcceptStream()
			if err != nil {

				if session.IsClosed() {
					log.Infof("[Relay] : %s", remoteAddr)
					break
				}
				log.Errorf("[Relay-ERROR] : %v", err)
				continue

			}

			go r.handleRequestStream(stream, remoteAddr)
		}
	}()
}

func (r *RelayRepository) handleRequestStream(stream *smux.Stream, remoteAddr string) {
	log.Infof("[Relay] : %p  %s", stream, remoteAddr)

	defer stream.Close()

	buffer := make([]byte, 16384)
	n, err := stream.Read(buffer)
	if err != nil {

		if err == io.EOF {
			log.Infof("[Relay] : %s", remoteAddr)
		} else {
			log.Errorf("[Relay-ERROR] : %v", err)
		}

		return
	}

	log.Infof("[Relay] : %d ", n)

	r.requestChan <- &RelayRequestItem{
		Data:       buffer[:n],
		Stream:     stream,
		ReceivedAt: time.Now(),
		RemoteAddr: remoteAddr,
	}
}

func (r *RelayRepository) StartResponseListener() {

	listenAddr := fmt.Sprintf("0.0.0.0:%s", r.relayConfig.ResponsePort)
	log.Infof("[Relay]  %s ", listenAddr)

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {

		log.Fatalf("[Relay-FATAL] : %v", err)
		return
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer listener.Close()

		for {
			select {
			case <-r.done:
				log.Infof("[Relay] ")
				return
			default:
				conn, err := listener.Accept()
				if err != nil {
					log.Errorf("[Relay-ERROR] : %v", err)
					continue
				}

				log.Infof("[Relay]  %s ", conn.RemoteAddr().String())

				go r.handleResponseConnection(conn)
			}
		}
	}()
}

func (r *RelayRepository) handleResponseConnection(conn net.Conn) {
	remoteAddr := conn.RemoteAddr().String()
	log.Infof("[Relay]  %s ", remoteAddr)

	session, err := smux.Server(conn, connection.DefaultSmuxConfig())
	if err != nil {
		log.Errorf("[Relay-ERROR] SMUX: %v", err)
		conn.Close()
		return
	}

	connection.AddServerSession(remoteAddr, session)

	for {
		stream, err := session.AcceptStream()
		if err != nil {
			if session.IsClosed() {
				log.Infof("[Relay] : %s", remoteAddr)
				break
			}
			log.Errorf("[Relay-ERROR] : %v", err)
			continue
		}

		go r.handleResponseStream(stream, remoteAddr)
	}
}

func (r *RelayRepository) handleResponseStream(stream *smux.Stream, remoteAddr string) {
	log.Infof("[Relay] : %p  %s", stream, remoteAddr)
	defer stream.Close()

	buffer := make([]byte, 65536)
	n, err := stream.Read(buffer)
	if err != nil {
		if err == io.EOF {
			log.Infof("[Relay] : %s", remoteAddr)
		} else {
			log.Errorf("[Relay-ERROR] : %v", err)
		}

		return
	}
	log.Infof("[RESP-FLOW] : ID=%p, =%d, =%s", stream, n, remoteAddr)
	log.Infof("[Relay] : %d ", n)
	log.Infof("[RESP-FLOW] : =%d, =%v", n, time.Now().Format("15:04:05.000"))

	r.responseChan <- &RelayResponseItem{
		Data:       buffer[:n],
		ReceivedAt: time.Now(),
		RemoteAddr: remoteAddr,
	}
}

func (rp *RelayProxy) Start() {

	rp.repository.StartProcessors()

	go rp.repository.StartRequestListener()

	rp.repository.StartResponseListener()
}

func (rp *RelayProxy) Stop() {
	rp.repository.Stop()
}

func RelayProxyWithFullConfig(relayConfig RelayConfig, repoConfig RelayRepositoryConfig) {
	proxy := CreateRelayProxy(relayConfig, repoConfig)
	proxy.Start()

	select {}
}

func RelayProxyfunc() {
	proxy := CreateRelayProxy(DefaultRelayConfig, DefaultRelayRepositoryConfig)
	proxy.Start()

	select {}
}

// Helper function min for logging byte slices safely
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

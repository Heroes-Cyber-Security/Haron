package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"
)

// TimeoutManager manages automatic cleanup of player instances
type TimeoutManager struct {
	instances      map[string]*time.Timer
	mutex          sync.RWMutex
	defaultTimeout time.Duration
}

// NewTimeoutManager creates a new TimeoutManager with default 5-minute timeout
func NewTimeoutManager() *TimeoutManager {
	return &TimeoutManager{
		instances:      make(map[string]*time.Timer),
		defaultTimeout: 5 * time.Minute,
	}
}

// Register registers or updates a timeout for an access token
func (tm *TimeoutManager) Register(accessToken string, timeoutMinutes int) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	if existing, ok := tm.instances[accessToken]; ok {
		existing.Stop()
	}

	timeout := tm.defaultTimeout
	if timeoutMinutes > 0 {
		timeout = time.Duration(timeoutMinutes) * time.Minute
	}

	log.Printf("Registering timeout for %s: %v", accessToken, timeout)
	tm.instances[accessToken] = time.AfterFunc(timeout, func() {
		tm.cleanup(accessToken)
	})
}

// cleanup performs automatic instance cleanup after timeout
func (tm *TimeoutManager) cleanup(accessToken string) {
	tm.mutex.Lock()
	delete(tm.instances, accessToken)
	tm.mutex.Unlock()

	log.Printf("Timeout expired for %s, cleaning up instance", accessToken)

	reqBody := map[string]interface{}{}
	bodyBytes, _ := json.Marshal(reqBody)

	resp, err := http.Post(
		"http://localhost:8080/stop",
		"application/json",
		bytes.NewReader(bodyBytes),
	)
	if err != nil {
		log.Printf("Failed to cleanup instance %s: %v", accessToken, err)
		return
	}
	defer resp.Body.Close()
}

// Cancel cancels the timeout for an access token
func (tm *TimeoutManager) Cancel(accessToken string) {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	if timer, ok := tm.instances[accessToken]; ok {
		timer.Stop()
		delete(tm.instances, accessToken)
		log.Printf("Cancelled timeout for %s", accessToken)
	}
}

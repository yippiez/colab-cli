package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
)

// KernelClient manages a Jupyter kernel WebSocket connection.
type KernelClient struct {
	conn           *websocket.Conn
	sessionID      string // client session ID used in WS message headers
	jupyterSession string // Jupyter server session ID (for cleanup on close)
	kernelID       string
	proxyURL       string // runtime proxy URL (e.g., https://8080-gpu-xxx.prod.colab.dev)
	proxyToken     string // runtime proxy auth token
	http           *http.Client
}

// jupyterMessage is the Jupyter messaging protocol message format.
type jupyterMessage struct {
	Channel      string                 `json:"channel"`
	Header       jupyterHeader          `json:"header"`
	ParentHeader map[string]interface{} `json:"parent_header"`
	Metadata     map[string]interface{} `json:"metadata"`
	Content      map[string]interface{} `json:"content"`
}

type jupyterHeader struct {
	MsgID    string `json:"msg_id"`
	MsgType  string `json:"msg_type"`
	Session  string `json:"session"`
	Username string `json:"username"`
	Version  string `json:"version"`
	Date     string `json:"date"`
}

type colabAuthRequest struct {
	AuthType   string
	ColabMsgID int
}

// NewKernelClient creates a Jupyter session and connects to the kernel via WebSocket.
func NewKernelClient(ctx context.Context, rt *Runtime) (*KernelClient, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	// Use ProxyURL (the full runtime URL) for Jupyter API calls
	endpoint := rt.ProxyURL
	if endpoint == "" {
		return nil, fmt.Errorf("no proxy URL available — runtime may not be fully assigned")
	}
	validatedEndpoint, err := validateRuntimeProxyURL(endpoint)
	if err != nil {
		logRuntimeProxyValidationFailure(endpoint, err)
		return nil, fmt.Errorf("invalid runtime proxy URL: %w", err)
	}
	endpoint = validatedEndpoint

	// Create a new Jupyter session to get a kernel.
	// The name/path are arbitrary — Jupyter just uses them for display.
	sessURL := endpoint + "/api/sessions"
	sessBody := strings.NewReader(`{"kernel":{"name":"python3"},"name":"colab","path":"colab.ipynb","type":"notebook"}`)

	req, err := http.NewRequestWithContext(ctx, "POST", sessURL, sessBody)
	if err != nil {
		return nil, fmt.Errorf("create session request: %w", err)
	}
	req.Header.Set("X-Colab-Runtime-Proxy-Token", rt.ProxyToken)
	req.Header.Set("X-Colab-Client-Agent", clientAgent)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create session failed (status %d): %s", resp.StatusCode, body)
	}

	var sessResp struct {
		ID     string `json:"id"`
		Kernel struct {
			ID string `json:"id"`
		} `json:"kernel"`
	}
	if err := json.Unmarshal(body, &sessResp); err != nil {
		return nil, fmt.Errorf("parse session response: %w (body: %s)", err, body)
	}

	if sessResp.Kernel.ID == "" {
		return nil, fmt.Errorf("no kernel ID in session response: %s", body)
	}

	// Connect WebSocket to kernel channels
	// Generate a client session ID for the WebSocket protocol
	clientSession := uuid.New().String()

	wsURL := strings.Replace(endpoint, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL += "/api/kernels/" + sessResp.Kernel.ID + "/channels?session_id=" + clientSession

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"X-Colab-Runtime-Proxy-Token": {rt.ProxyToken},
			"X-Colab-Client-Agent":        {clientAgent},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("websocket connect: %w", err)
	}

	// 10MB read limit — Jupyter can return large outputs (e.g., training logs).
	// The default websocket limit is too small for real workloads.
	conn.SetReadLimit(10 * 1024 * 1024)

	kc := &KernelClient{
		conn:           conn,
		sessionID:      clientSession,
		kernelID:       sessResp.Kernel.ID,
		proxyURL:       endpoint,
		proxyToken:     rt.ProxyToken,
		http:           httpClient,
		jupyterSession: sessResp.ID,
	}

	// Wait for kernel to be ready by sending kernel_info_request
	if err := kc.waitReady(ctx); err != nil {
		conn.Close(websocket.StatusNormalClosure, "")
		return nil, fmt.Errorf("kernel not ready: %w", err)
	}

	// Start ping keepalive to prevent idle disconnection
	go kc.pingLoop(ctx)

	return kc, nil
}

// pingLoop sends periodic pings to keep the WebSocket alive.
func (kc *KernelClient) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := kc.conn.Ping(ctx); err != nil {
				return
			}
		}
	}
}

// waitReady sends a kernel_info_request and waits for the reply.
func (kc *KernelClient) waitReady(ctx context.Context) error {
	msgID := uuid.New().String()

	msg := jupyterMessage{
		Channel: "shell",
		Header: jupyterHeader{
			MsgID:    msgID,
			MsgType:  "kernel_info_request",
			Session:  kc.sessionID,
			Username: "colab",
			Version:  "5.3", // Jupyter messaging protocol version
			Date:     time.Now().UTC().Format(time.RFC3339),
		},
		ParentHeader: map[string]interface{}{},
		Metadata:     map[string]interface{}{},
		Content:      map[string]interface{}{},
	}

	if err := wsjson.Write(ctx, kc.conn, msg); err != nil {
		return fmt.Errorf("send kernel_info_request: %w", err)
	}

	readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for {
		var reply jupyterMessage
		if err := wsjson.Read(readCtx, kc.conn, &reply); err != nil {
			return fmt.Errorf("wait for kernel ready: %w", err)
		}
		if reply.Header.MsgType == "kernel_info_reply" {
			return nil
		}
	}
}

// Execute runs Python code and returns the combined output.
func (kc *KernelClient) Execute(ctx context.Context, code string) (string, error) {
	return kc.ExecuteStream(ctx, code, nil)
}

// ExecuteStream runs Python code and streams output via the callback.
// If onOutput is nil, output is collected and returned as a string.
func (kc *KernelClient) ExecuteStream(ctx context.Context, code string, onOutput func(stream, text string)) (string, error) {
	msgID := uuid.New().String()

	msg := jupyterMessage{
		Channel: "shell",
		Header: jupyterHeader{
			MsgID:    msgID,
			MsgType:  "execute_request",
			Session:  kc.sessionID,
			Username: "colab",
			Version:  "5.3",
			Date:     time.Now().UTC().Format(time.RFC3339),
		},
		ParentHeader: map[string]interface{}{},
		Metadata:     map[string]interface{}{},
		Content: map[string]interface{}{
			"code":             code,
			"silent":           false,
			"store_history":    true,
			"allow_stdin":      false,
			"stop_on_error":    true,
			"user_expressions": map[string]interface{}{},
		},
	}

	if err := wsjson.Write(ctx, kc.conn, msg); err != nil {
		return "", fmt.Errorf("send execute_request: %w", err)
	}

	var output strings.Builder
	for {
		var reply jupyterMessage
		if err := wsjson.Read(ctx, kc.conn, &reply); err != nil {
			return output.String(), fmt.Errorf("read message: %w", err)
		}

		// Only process messages for our request
		parentMsgID, _ := reply.ParentHeader["msg_id"].(string)
		if parentMsgID != msgID {
			continue
		}

		switch reply.Header.MsgType {
		case "stream":
			text, _ := reply.Content["text"].(string)
			if onOutput != nil {
				name, _ := reply.Content["name"].(string)
				onOutput(name, text)
			}
			output.WriteString(text)

		case "execute_result":
			data, _ := reply.Content["data"].(map[string]interface{})
			if text, ok := data["text/plain"].(string); ok {
				if onOutput != nil {
					onOutput("stdout", text+"\n")
				}
				output.WriteString(text)
				output.WriteString("\n")
			}

		case "error":
			ename, _ := reply.Content["ename"].(string)
			evalue, _ := reply.Content["evalue"].(string)
			traceback, _ := reply.Content["traceback"].([]interface{})

			errMsg := fmt.Sprintf("%s: %s", ename, evalue)
			if onOutput != nil {
				onOutput("stderr", errMsg+"\n")
				for _, tb := range traceback {
					if s, ok := tb.(string); ok {
						onOutput("stderr", s+"\n")
					}
				}
			}
			output.WriteString(errMsg)
			output.WriteString("\n")
			for _, tb := range traceback {
				if s, ok := tb.(string); ok {
					output.WriteString(s)
					output.WriteString("\n")
				}
			}

		case "execute_reply":
			status, _ := reply.Content["status"].(string)
			if status == "error" {
				ename, _ := reply.Content["ename"].(string)
				evalue, _ := reply.Content["evalue"].(string)
				if output.Len() == 0 {
					return "", fmt.Errorf("execution error: %s: %s", ename, evalue)
				}
			}
			return output.String(), nil
		}
	}
}

func (kc *KernelClient) MountDrive(ctx context.Context, client *ColabClient, endpoint string, onOutput func(stream, text string)) (string, error) {
	msgID := uuid.New().String()
	code := "from google.colab import drive\ndrive.mount('/content/drive')"

	msg := jupyterMessage{
		Channel: "shell",
		Header: jupyterHeader{
			MsgID:    msgID,
			MsgType:  "execute_request",
			Session:  kc.sessionID,
			Username: "colab",
			Version:  "5.3",
			Date:     time.Now().UTC().Format(time.RFC3339),
		},
		ParentHeader: map[string]interface{}{},
		Metadata:     map[string]interface{}{},
		Content: map[string]interface{}{
			"code":             code,
			"silent":           false,
			"store_history":    true,
			"allow_stdin":      true,
			"stop_on_error":    true,
			"user_expressions": map[string]interface{}{},
		},
	}

	if err := wsjson.Write(ctx, kc.conn, msg); err != nil {
		return "", fmt.Errorf("send execute_request: %w", err)
	}

	var output strings.Builder
	for {
		var reply jupyterMessage
		if err := wsjson.Read(ctx, kc.conn, &reply); err != nil {
			return output.String(), fmt.Errorf("read message: %w", err)
		}

		if authReq, ok := parseColabAuthRequest(reply); ok {
			err := kc.handleEphemeralAuth(ctx, client, endpoint, authReq, onOutput)
			if sendErr := kc.sendInputReply(ctx, authReq.ColabMsgID, err); sendErr != nil {
				return output.String(), fmt.Errorf("send auth reply: %w", sendErr)
			}
			if err != nil {
				return output.String(), err
			}
			continue
		}

		parentMsgID, _ := reply.ParentHeader["msg_id"].(string)
		if parentMsgID != msgID {
			continue
		}

		switch reply.Header.MsgType {
		case "stream":
			text, _ := reply.Content["text"].(string)
			if onOutput != nil {
				name, _ := reply.Content["name"].(string)
				onOutput(name, text)
			}
			output.WriteString(text)
		case "error":
			ename, _ := reply.Content["ename"].(string)
			evalue, _ := reply.Content["evalue"].(string)
			return output.String(), fmt.Errorf("mount drive error: %s: %s", ename, evalue)
		case "execute_reply":
			status, _ := reply.Content["status"].(string)
			if status == "error" {
				ename, _ := reply.Content["ename"].(string)
				evalue, _ := reply.Content["evalue"].(string)
				return output.String(), fmt.Errorf("mount drive error: %s: %s", ename, evalue)
			}
			return output.String(), nil
		}
	}
}

func parseColabAuthRequest(msg jupyterMessage) (colabAuthRequest, bool) {
	if msg.Header.MsgType != "colab_request" {
		return colabAuthRequest{}, false
	}
	requestType, _ := msg.Metadata["colab_request_type"].(string)
	if requestType != "request_auth" {
		return colabAuthRequest{}, false
	}
	contentRequest, _ := msg.Content["request"].(map[string]interface{})
	authType, _ := contentRequest["authType"].(string)
	if authType == "" {
		return colabAuthRequest{}, false
	}
	colabMsgIDFloat, ok := msg.Metadata["colab_msg_id"].(float64)
	if !ok {
		return colabAuthRequest{}, false
	}
	return colabAuthRequest{AuthType: authType, ColabMsgID: int(colabMsgIDFloat)}, true
}

func (kc *KernelClient) handleEphemeralAuth(ctx context.Context, client *ColabClient, endpoint string, req colabAuthRequest, onOutput func(stream, text string)) error {
	dryRunResult, err := client.PropagateCredentials(ctx, endpoint, req.AuthType, true)
	if err != nil {
		return fmt.Errorf("credentials propagation dry run: %w", err)
	}
	if dryRunResult.Success {
		finalResult, err := client.PropagateCredentials(ctx, endpoint, req.AuthType, false)
		if err != nil {
			return fmt.Errorf("credentials propagation: %w", err)
		}
		if !finalResult.Success {
			return fmt.Errorf("credentials propagation unsuccessful")
		}
		return nil
	}
	if dryRunResult.UnauthorizedRedirectURI == "" {
		return fmt.Errorf("authorization required but no redirect URL was returned")
	}

	message := "\nGoogle Drive authorization required.\n\nOpen this URL in your browser:\n" + dryRunResult.UnauthorizedRedirectURI + "\n\nAfter completing authorization, press Enter here to continue...\n"
	if onOutput != nil {
		onOutput("stdout", message)
	} else {
		fmt.Print(message)
	}

	reader := bufio.NewReader(os.Stdin)
	if _, err := reader.ReadString('\n'); err != nil {
		return fmt.Errorf("waiting for authorization confirmation: %w", err)
	}

	finalResult, err := client.PropagateCredentials(ctx, endpoint, req.AuthType, false)
	if err != nil {
		return fmt.Errorf("credentials propagation: %w", err)
	}
	if !finalResult.Success {
		return fmt.Errorf("credentials propagation unsuccessful")
	}
	return nil
}

func (kc *KernelClient) sendInputReply(ctx context.Context, requestMessageID int, replyErr error) error {
	reply := jupyterMessage{
		Channel: "stdin",
		Header: jupyterHeader{
			MsgID:    uuid.New().String(),
			MsgType:  "input_reply",
			Session:  kc.sessionID,
			Username: "username",
			Version:  "5.0",
			Date:     time.Now().UTC().Format(time.RFC3339),
		},
		ParentHeader: map[string]interface{}{},
		Metadata:     map[string]interface{}{},
		Content: map[string]interface{}{
			"value": map[string]interface{}{
				"type":         "colab_reply",
				"colab_msg_id": requestMessageID,
			},
		},
	}
	if replyErr != nil {
		reply.Content["value"].(map[string]interface{})["error"] = replyErr.Error()
	}
	return wsjson.Write(ctx, kc.conn, reply)
}

// Close closes the WebSocket connection and cleans up the session.
func (kc *KernelClient) Close() error {
	// Delete the session
	delURL := kc.proxyURL + "/api/sessions/" + kc.jupyterSession
	req, err := http.NewRequest("DELETE", delURL, nil)
	if err == nil {
		req.Header.Set("X-Colab-Runtime-Proxy-Token", kc.proxyToken)
		req.Header.Set("X-Colab-Client-Agent", clientAgent)
		resp, err := kc.http.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}

	return kc.conn.Close(websocket.StatusNormalClosure, "done")
}

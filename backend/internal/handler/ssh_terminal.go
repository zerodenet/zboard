package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"

	"github.com/zerodenet/zboard/backend/internal/model"
)

const (
	sshTerminalTicketTTL       = 30 * time.Second
	sshTerminalIdleTimeout     = 15 * time.Minute
	sshTerminalMaximumDuration = 2 * time.Hour
	sshTerminalPingInterval    = 25 * time.Second
	sshTerminalWriteTimeout    = 10 * time.Second
	sshTerminalMaxInputBytes   = 32 * 1024
	sshTerminalMaxTickets      = 4096
	sshTerminalMaxPerUser      = 3
	sshTerminalMaxPerNode      = 2
)

type sshTerminalTicket struct {
	Claims    authClaims
	NodeID    uint
	ExpiresAt time.Time
}

type sshTerminalTicketStore struct {
	mu      sync.Mutex
	tickets map[[sha256.Size]byte]sshTerminalTicket
	now     func() time.Time
}

func newSSHTerminalTicketStore() *sshTerminalTicketStore {
	return &sshTerminalTicketStore{
		tickets: make(map[[sha256.Size]byte]sshTerminalTicket),
		now:     time.Now,
	}
}

func (s *sshTerminalTicketStore) issue(claims authClaims, nodeID uint) (string, time.Time, error) {
	var randomBytes [32]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", time.Time{}, fmt.Errorf("generate SSH terminal ticket: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(randomBytes[:])
	digest := sha256.Sum256([]byte(token))

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	for key, ticket := range s.tickets {
		if !ticket.ExpiresAt.After(now) {
			delete(s.tickets, key)
		}
	}
	if len(s.tickets) >= sshTerminalMaxTickets {
		return "", time.Time{}, errors.New("too many pending SSH terminal tickets")
	}
	expiresAt := now.Add(sshTerminalTicketTTL)
	s.tickets[digest] = sshTerminalTicket{Claims: claims, NodeID: nodeID, ExpiresAt: expiresAt}
	return token, expiresAt, nil
}

func (s *sshTerminalTicketStore) consume(token string, nodeID uint) (sshTerminalTicket, bool) {
	if len(token) < 32 || len(token) > 128 {
		return sshTerminalTicket{}, false
	}
	digest := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	ticket, ok := s.tickets[digest]
	delete(s.tickets, digest)
	if !ok || ticket.NodeID != nodeID || !ticket.ExpiresAt.After(s.now().UTC()) {
		return sshTerminalTicket{}, false
	}
	return ticket, true
}

type sshTerminalSessionLimiter struct {
	mu    sync.Mutex
	users map[uint]int
	nodes map[uint]int
}

func newSSHTerminalSessionLimiter() *sshTerminalSessionLimiter {
	return &sshTerminalSessionLimiter{users: make(map[uint]int), nodes: make(map[uint]int)}
}

func (l *sshTerminalSessionLimiter) acquire(userID uint, nodeID uint) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.users[userID] >= sshTerminalMaxPerUser || l.nodes[nodeID] >= sshTerminalMaxPerNode {
		return nil, false
	}
	l.users[userID]++
	l.nodes[nodeID]++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.users[userID]--
			l.nodes[nodeID]--
			if l.users[userID] == 0 {
				delete(l.users, userID)
			}
			if l.nodes[nodeID] == 0 {
				delete(l.nodes, nodeID)
			}
		})
	}, true
}

type sshTerminalRuntime struct {
	tickets *sshTerminalTicketStore
	limiter *sshTerminalSessionLimiter
}

func newSSHTerminalRuntime() *sshTerminalRuntime {
	return &sshTerminalRuntime{
		tickets: newSSHTerminalTicketStore(),
		limiter: newSSHTerminalSessionLimiter(),
	}
}

type sshTerminalClientMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

type sshTerminalServerMessage struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

type sshTerminalInbound struct {
	message sshTerminalClientMessage
	err     error
}

type sshTerminalSocketWriter struct {
	connection   *websocket.Conn
	mu           sync.Mutex
	bytesWritten atomic.Uint64
	lastActivity atomic.Int64
}

func newSSHTerminalSocketWriter(connection *websocket.Conn) *sshTerminalSocketWriter {
	w := &sshTerminalSocketWriter{connection: connection}
	w.touch()
	return w
}

func (w *sshTerminalSocketWriter) touch() {
	w.lastActivity.Store(time.Now().UnixNano())
}

func (w *sshTerminalSocketWriter) Write(payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.connection.SetWriteDeadline(time.Now().Add(sshTerminalWriteTimeout))
	if err := w.connection.WriteMessage(websocket.BinaryMessage, payload); err != nil {
		return 0, err
	}
	w.bytesWritten.Add(uint64(len(payload)))
	w.touch()
	return len(payload), nil
}

func (w *sshTerminalSocketWriter) writeJSON(message sshTerminalServerMessage) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.connection.SetWriteDeadline(time.Now().Add(sshTerminalWriteTimeout))
	return w.connection.WriteJSON(message)
}

func (w *sshTerminalSocketWriter) writeControl(messageType int, payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.connection.WriteControl(messageType, payload, time.Now().Add(sshTerminalWriteTimeout))
}

func (h *handlers) NodeSSHTerminalTicketHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	node, err := h.loadNode(nodeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	if err := h.validateNodeSSH(node); err != nil {
		BadRequest(w, err.Error())
		return
	}
	ticket, expiresAt, err := h.sshTerminal.tickets.issue(claims, node.ID)
	if err != nil {
		writeJSON(w, http.StatusTooManyRequests, err.Error(), nil)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	OK(w, map[string]interface{}{
		"ticket":     ticket,
		"expires_at": expiresAt.Unix(),
	})
}

func (h *handlers) NodeSSHTerminalHandler(w http.ResponseWriter, r *http.Request) {
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	ticket, ok := h.sshTerminal.tickets.consume(strings.TrimSpace(r.URL.Query().Get("ticket")), nodeID)
	if !ok {
		Unauthorized(w, "invalid or expired SSH terminal ticket")
		return
	}
	if !sshTerminalSameOrigin(r) {
		Forbidden(w, "SSH terminal origin does not match this panel")
		return
	}
	var user model.User
	if err := h.db.Select("id", "email", "is_admin", "status").
		Where("id = ? AND status = ? AND is_admin = 1", ticket.Claims.UserID, userStatusActive).
		First(&user).Error; err != nil {
		Unauthorized(w, "SSH terminal administrator is no longer active")
		return
	}
	ticket.Claims.Email = user.Email
	ticket.Claims.IsAdmin = true
	node, err := h.loadNode(nodeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	if err := h.validateNodeSSH(node); err != nil {
		BadRequest(w, err.Error())
		return
	}
	release, ok := h.sshTerminal.limiter.acquire(ticket.Claims.UserID, node.ID)
	if !ok {
		writeJSON(w, http.StatusTooManyRequests, "SSH terminal session limit reached", nil)
		return
	}
	defer release()

	sessionID := uuid.NewString()
	if err := createAuditLog(h.db, ticket.Claims, "node.ssh_terminal.open", fmt.Sprintf("node:%d", node.ID), fmt.Sprintf("session=%s remote=%s", sessionID, terminalRemoteAddress(r.RemoteAddr))); err != nil {
		ServerError(w, err)
		return
	}
	startedAt := time.Now()
	reason := "client_closed"
	var bytesRead atomic.Uint64
	var writer *sshTerminalSocketWriter
	defer func() {
		var bytesWritten uint64
		if writer != nil {
			bytesWritten = writer.bytesWritten.Load()
		}
		detail := fmt.Sprintf("session=%s duration_ms=%d bytes_in=%d bytes_out=%d reason=%s", sessionID, time.Since(startedAt).Milliseconds(), bytesRead.Load(), bytesWritten, reason)
		if err := createAuditLog(h.db, ticket.Claims, "node.ssh_terminal.close", fmt.Sprintf("node:%d", node.ID), detail); err != nil {
			log.Printf("record SSH terminal close audit failed: session=%s error=%v", sessionID, err)
		}
	}()

	upgrader := websocket.Upgrader{
		ReadBufferSize:  4096,
		WriteBufferSize: 4096,
		CheckOrigin:     sshTerminalSameOrigin,
	}
	connection, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		reason = "upgrade_failed"
		return
	}
	defer connection.Close()
	connection.SetReadLimit(sshTerminalMaxInputBytes + 4096)
	writer = newSSHTerminalSocketWriter(connection)

	_ = writer.writeJSON(sshTerminalServerMessage{Type: "connecting", Message: "正在建立 SSH 连接…"})
	sshClient, _, err := h.dialNodeSSH(node)
	if err != nil {
		reason = "ssh_connect_failed"
		_ = writer.writeJSON(sshTerminalServerMessage{Type: "error", Message: "SSH 连接失败：" + err.Error()})
		_ = writer.writeControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "SSH connection failed"))
		return
	}
	defer sshClient.Close()

	sshSession, err := sshClient.NewSession()
	if err != nil {
		reason = "ssh_session_failed"
		_ = writer.writeJSON(sshTerminalServerMessage{Type: "error", Message: "无法创建 SSH 会话：" + err.Error()})
		return
	}
	defer sshSession.Close()
	stdin, err := sshSession.StdinPipe()
	if err != nil {
		reason = "ssh_stdin_failed"
		_ = writer.writeJSON(sshTerminalServerMessage{Type: "error", Message: "无法打开 SSH 输入：" + err.Error()})
		return
	}
	sshSession.Stdout = writer
	sshSession.Stderr = writer
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sshSession.RequestPty("xterm-256color", 30, 120, modes); err != nil {
		reason = "ssh_pty_failed"
		_ = writer.writeJSON(sshTerminalServerMessage{Type: "error", Message: "服务器拒绝分配终端：" + err.Error()})
		return
	}
	if err := sshSession.Shell(); err != nil {
		reason = "ssh_shell_failed"
		_ = writer.writeJSON(sshTerminalServerMessage{Type: "error", Message: "服务器拒绝启动 Shell：" + err.Error()})
		return
	}
	if err := writer.writeJSON(sshTerminalServerMessage{Type: "connected", Message: "SSH 已连接"}); err != nil {
		reason = "client_write_failed"
		return
	}

	done := make(chan struct{})
	defer close(done)
	incoming := make(chan sshTerminalInbound, 16)
	go readSSHTerminalMessages(connection, incoming, done)
	waitResult := make(chan error, 1)
	go func() { waitResult <- sshSession.Wait() }()
	pingTicker := time.NewTicker(sshTerminalPingInterval)
	defer pingTicker.Stop()
	idleTicker := time.NewTicker(time.Minute)
	defer idleTicker.Stop()
	maximumTimer := time.NewTimer(sshTerminalMaximumDuration)
	defer maximumTimer.Stop()

	for {
		select {
		case inbound := <-incoming:
			if inbound.err != nil {
				if websocket.IsUnexpectedCloseError(inbound.err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					reason = "client_connection_lost"
				} else {
					reason = "client_closed"
				}
				return
			}
			switch inbound.message.Type {
			case "input":
				if len(inbound.message.Data) > sshTerminalMaxInputBytes {
					reason = "input_too_large"
					_ = writer.writeJSON(sshTerminalServerMessage{Type: "error", Message: "终端输入过长，连接已关闭"})
					return
				}
				written, err := io.WriteString(stdin, inbound.message.Data)
				if err != nil {
					reason = "ssh_input_failed"
					return
				}
				bytesRead.Add(uint64(written))
				writer.touch()
			case "resize":
				if inbound.message.Cols < 20 || inbound.message.Cols > 500 || inbound.message.Rows < 5 || inbound.message.Rows > 200 {
					continue
				}
				if err := sshSession.WindowChange(inbound.message.Rows, inbound.message.Cols); err != nil {
					reason = "ssh_resize_failed"
					return
				}
				writer.touch()
			case "ping":
				writer.touch()
			}
		case err := <-waitResult:
			reason = "remote_exit"
			if err != nil && !errors.Is(err, io.EOF) {
				_ = writer.writeJSON(sshTerminalServerMessage{Type: "error", Message: "远程 Shell 已结束：" + err.Error()})
			}
			return
		case <-pingTicker.C:
			if err := writer.writeControl(websocket.PingMessage, nil); err != nil {
				reason = "client_ping_failed"
				return
			}
		case <-idleTicker.C:
			lastActivity := time.Unix(0, writer.lastActivity.Load())
			if time.Since(lastActivity) >= sshTerminalIdleTimeout {
				reason = "idle_timeout"
				_ = writer.writeJSON(sshTerminalServerMessage{Type: "error", Message: "终端因长时间无操作已断开"})
				return
			}
		case <-maximumTimer.C:
			reason = "maximum_duration"
			_ = writer.writeJSON(sshTerminalServerMessage{Type: "error", Message: "终端已达到最长会话时间，请重新连接"})
			return
		case <-r.Context().Done():
			reason = "request_cancelled"
			return
		}
	}
}

func readSSHTerminalMessages(connection *websocket.Conn, output chan<- sshTerminalInbound, done <-chan struct{}) {
	for {
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			select {
			case output <- sshTerminalInbound{err: err}:
			case <-done:
			}
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var message sshTerminalClientMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			select {
			case output <- sshTerminalInbound{err: errors.New("invalid terminal message")}:
			case <-done:
			}
			return
		}
		select {
		case output <- sshTerminalInbound{message: message}:
		case <-done:
			return
		}
	}
}

func sshTerminalSameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host) && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func terminalRemoteAddress(remoteAddress string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddress))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(remoteAddress)
}

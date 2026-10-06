package backend

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Hub struct {
	mu            sync.RWMutex
	clients       map[string]*wsClient
	subscriptions map[string]string // cid -> fileId

	treeMu          sync.Mutex
	treeTimer       *time.Timer
	pendingTreeJSON []byte
	pendingReasons  []string
	lastTreeJSON    []byte

	Versions *RecentVersionStore

	// EditHandler processes "edit" messages read off a client's connection.
	// Set by Server after both it and the Hub exist.
	EditHandler func(senderCid string, msg EditMessage)

	// OnUnsubscribe is called (without any Hub lock held) with the fileId a
	// connection was subscribed to when it disconnects.
	OnUnsubscribe func(fileId string)
}

type wsClient struct {
	cid  string
	conn *websocket.Conn
	send chan []byte
}

func NewHub() *Hub {
	return &Hub{
		clients:       map[string]*wsClient{},
		subscriptions: map[string]string{},
		Versions:      newRecentVersionStore(),
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	cid := uniqueId(16)
	c := &wsClient{cid: cid, conn: conn, send: make(chan []byte, 64)}

	h.mu.Lock()
	h.clients[cid] = c
	h.mu.Unlock()

	// send init
	initMsg, _ := json.Marshal(map[string]string{"type": "init", "cid": cid})
	c.send <- initMsg

	go c.writePump()
	go h.readPump(c)
}

func (h *Hub) readPump(c *wsClient) {
	defer func() {
		h.mu.Lock()
		delete(h.clients, c.cid)
		old := h.subscriptions[c.cid]
		delete(h.subscriptions, c.cid)
		h.mu.Unlock()
		c.conn.Close()
		if old != "" && h.OnUnsubscribe != nil {
			h.OnUnsubscribe(old)
		}
	}()
	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &base); err != nil {
			continue
		}
		switch base.Type {
		case "edit":
			var msg EditMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			if h.EditHandler != nil {
				h.EditHandler(c.cid, msg)
			}
		}
	}
}

func (c *wsClient) writePump() {
	defer c.conn.Close()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

// SetSubscription replaces cid's subscription (an empty fileId clears it) and
// returns the previous fileId, if any.
func (h *Hub) SetSubscription(cid, fileId string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[cid]; !ok {
		return ""
	}
	old := h.subscriptions[cid]
	if fileId == "" {
		delete(h.subscriptions, cid)
	} else {
		h.subscriptions[cid] = fileId
	}
	return old
}

// HasSubscribers reports whether any connection is subscribed to fileId.
func (h *Hub) HasSubscribers(fileId string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, f := range h.subscriptions {
		if f == fileId {
			return true
		}
	}
	return false
}

// Broadcast sends msg (a JSON-marshalable value) to every connection.
func (h *Hub) Broadcast(v any) {
	msg, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		select {
		case c.send <- msg:
		default:
			log.Printf("ws: send buffer full for cid %s", c.cid)
		}
	}
}

func (h *Hub) BroadcastFS(tree FolderNode, reason string) {
	treeJSON, err := json.Marshal(tree)
	if err != nil {
		return
	}
	h.treeMu.Lock()
	h.pendingTreeJSON = treeJSON
	h.pendingReasons = append(h.pendingReasons, reason)
	if h.treeTimer != nil {
		h.treeTimer.Reset(50 * time.Millisecond)
	} else {
		h.treeTimer = time.AfterFunc(50*time.Millisecond, h.flushTree)
	}
	h.treeMu.Unlock()
}

func (h *Hub) flushTree() {
	h.treeMu.Lock()
	treeJSON := h.pendingTreeJSON
	reason := strings.Join(h.pendingReasons, " | ")
	h.pendingTreeJSON = nil
	h.pendingReasons = nil
	h.treeTimer = nil
	same := bytes.Equal(treeJSON, h.lastTreeJSON)
	if !same {
		h.lastTreeJSON = treeJSON
	}
	h.treeMu.Unlock()

	if same {
		return
	}

	msg, err := json.Marshal(map[string]interface{}{
		"_reason": reason,
		"type":    "filesystem",
		"tree":    json.RawMessage(treeJSON),
	})
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		select {
		case c.send <- msg:
		default:
			log.Printf("ws: send buffer full for cid %s", c.cid)
		}
	}
}

// SendTo delivers msg to a single connection by cid, if it's still connected.
func (h *Hub) SendTo(cid string, msg []byte) {
	h.mu.RLock()
	c, ok := h.clients[cid]
	h.mu.RUnlock()
	if !ok {
		return
	}
	select {
	case c.send <- msg:
	default:
	}
}

// SendEditConflict notifies a single sender that its edit was rejected, carrying the
// latest known content/versionId so the client can resync.
func (h *Hub) SendEditConflict(cid, fileId, content, versionId string) {
	msg, err := json.Marshal(map[string]string{
		"type":      "editConflict",
		"fileId":    fileId,
		"content":   content,
		"versionId": versionId,
	})
	if err != nil {
		return
	}
	h.SendTo(cid, msg)
}

func (h *Hub) BroadcastContent(fileId, content, versionId, senderCid string) {
	msg, err := json.Marshal(map[string]string{
		"type":      "content",
		"fileId":    fileId,
		"content":   content,
		"versionId": versionId,
	})
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for cid, c := range h.clients {
		if cid == senderCid {
			continue
		}
		if h.subscriptions[cid] == fileId {
			select {
			case c.send <- msg:
			default:
			}
		}
	}
}

package backend

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait    = 10 * time.Second
	pongWait     = 60 * time.Second
	pingInterval = 30 * time.Second
	maxReadBytes = 16 << 20
)

var upgrader = websocket.Upgrader{CheckOrigin: sameOrigin}

// sameOrigin allows requests with no Origin header (non-browser clients) or whose
// Origin host matches the Host the request was sent to. This blocks other web
// pages from driving the server through a visitor's browser.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host
}

type Hub struct {
	mu            sync.RWMutex
	clients       map[string]*wsClient
	subscriptions map[string]string // cid -> fileId

	treeMu       sync.Mutex
	lastTreeJSON []byte

	Versions *RecentVersionStore

	// EditHandler processes "edit" messages read off a client's connection.
	// Set by Server after both it and the Hub exist.
	EditHandler func(senderCid string, msg EditMessage)

	// OnUnsubscribe is called (without any Hub lock held) with the fileId a
	// connection was subscribed to when it disconnects.
	OnUnsubscribe func(fileId string)
}

type wsClient struct {
	cid       string
	conn      *websocket.Conn
	send      chan []byte
	done      chan struct{} // closed when the connection is torn down
	closeOnce sync.Once
}

// close tears the connection down; the pumps exit and readPump unregisters it.
func (c *wsClient) close() {
	c.closeOnce.Do(func() {
		if c.done != nil {
			close(c.done)
		}
		if c.conn != nil {
			c.conn.Close()
		}
	})
}

// enqueue queues msg for c. A client whose buffer is full has missed messages it
// can't recover from, so it is disconnected; its reconnect refetches everything.
func (c *wsClient) enqueue(msg []byte) {
	select {
	case c.send <- msg:
	default:
		log.Printf("ws: send buffer full for cid %s, disconnecting", c.cid)
		c.close()
	}
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
	c := &wsClient{cid: cid, conn: conn, send: make(chan []byte, 64), done: make(chan struct{})}

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
		c.close()
		if old != "" && h.OnUnsubscribe != nil {
			h.OnUnsubscribe(old)
		}
	}()
	c.conn.SetReadLimit(maxReadBytes)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
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
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.close()
	}()
	for {
		select {
		case msg := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
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
		c.enqueue(msg)
	}
}

// BroadcastFS sends the tree to every connection unless it is unchanged since the
// last broadcast. Debouncing is the caller's job (Server.scheduleTree).
func (h *Hub) BroadcastFS(tree *FolderNode) {
	treeJSON, err := json.Marshal(tree)
	if err != nil {
		return
	}
	h.treeMu.Lock()
	same := bytes.Equal(treeJSON, h.lastTreeJSON)
	if !same {
		h.lastTreeJSON = treeJSON
	}
	h.treeMu.Unlock()
	if same {
		return
	}
	msg, err := json.Marshal(map[string]any{
		"type": "filesystem",
		"tree": json.RawMessage(treeJSON),
	})
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.clients {
		c.enqueue(msg)
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
	c.enqueue(msg)
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
			c.enqueue(msg)
		}
	}
}

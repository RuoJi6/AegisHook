package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

type ClientNode struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Platform  string    `json:"platform"`
	CreatedAt time.Time `json:"createdAt"`
	Revoked   bool      `json:"revoked"`
}

// The stored form keeps credential hashes out of all management responses.
type storedClientNode struct {
	ClientNode
	Hash string `json:"tokenHash"`
}
type enrollment struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ExpiresAt time.Time `json:"expiresAt"`
	Used      bool      `json:"used"`
}
type EnrollmentCode struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type ClientConnection struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	NodeID   string `json:"nodeId"`
}

func credentialHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (e *Engine) CreateEnrollment(name string) (EnrollmentCode, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(name) > 120 {
		return EnrollmentCode{}, errors.New("设备名称过长")
	}
	code := ID() + ID()
	expires := e.clock().Add(10 * time.Minute)
	record := enrollment{ID: credentialHash(code), Name: name, ExpiresAt: expires}
	err := e.change("client_enrollments", record.ID, record, "client.enrollment.create", "创建十分钟有效的一次性接入码")
	return EnrollmentCode{Code: code, ExpiresAt: expires}, err
}
func (e *Engine) EnrollClient(code, platform string) (ClientConnection, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(code) != 64 || !hasExact(platform, "linux", "darwin", "windows") {
		return ClientConnection{}, errors.New("接入码或平台无效")
	}
	var record enrollment
	if e.get("client_enrollments", credentialHash(code), &record) != nil || record.Used || !e.clock().Before(record.ExpiresAt) {
		return ClientConnection{}, errors.New("接入码无效、已使用或已过期")
	}
	record.Used = true
	return e.issueClientCredential("client_enrollments", record.ID, record, record.Name, platform)
}

// Called with e.mu held. Consumption, credential issuance and audit commit together.
func (e *Engine) issueClientCredential(kind, id string, record any, name, platform string) (ClientConnection, error) {
	token := ID() + ID()
	node := storedClientNode{ClientNode: ClientNode{ID: ID(), Name: name, Platform: platform, CreatedAt: e.clock()}, Hash: credentialHash(token)}
	tx, err := e.DB.Begin()
	if err != nil {
		return ClientConnection{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO records(kind,id,payload) VALUES('client_nodes',?,?)", node.ID, mustJSON(node)); err != nil {
		return ClientConnection{}, err
	}
	if _, err = tx.Exec("UPDATE records SET payload=? WHERE kind=? AND id=?", mustJSON(record), kind, id); err != nil {
		return ClientConnection{}, err
	}
	audit := Audit{ID: ID(), Action: "client.enroll", Subject: node.ID, Detail: "设备已接入", CreatedAt: e.clock()}
	if _, err = tx.Exec("INSERT INTO records(kind,id,payload) VALUES('audit',?,?)", audit.ID, mustJSON(audit)); err != nil {
		return ClientConnection{}, err
	}
	if err = tx.Commit(); err != nil {
		return ClientConnection{}, err
	}
	if e.Notify != nil {
		e.Notify()
	}
	return ClientConnection{Token: token, NodeID: node.ID}, nil
}
func (e *Engine) AuthenticateClient(token string) (ClientNode, bool) {
	if len(token) != 64 {
		return ClientNode{}, false
	}
	hash := credentialHash(token)
	var payload []byte
	if err := e.DB.QueryRow("SELECT payload FROM records WHERE kind='client_nodes' AND json_extract(payload,'$.tokenHash')=?", hash).Scan(&payload); err != nil {
		return ClientNode{}, false
	}
	var node storedClientNode
	if json.Unmarshal(payload, &node) != nil || node.Revoked {
		return ClientNode{}, false
	}
	return node.ClientNode, true
}
func (e *Engine) ClientNodes() ([]ClientNode, error) {
	nodes, err := list[storedClientNode](e, "client_nodes")
	out := []ClientNode{}
	for _, node := range nodes {
		out = append(out, node.ClientNode)
	}
	return out, err
}
func (e *Engine) RevokeClient(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var node storedClientNode
	if err := e.get("client_nodes", id, &node); err != nil {
		return errors.New("设备不存在")
	}
	node.Revoked = true
	return e.change("client_nodes", id, node, "client.revoke", "撤销设备接入")
}
func (e *Engine) ClientOwnsInstance(nodeID, instanceID string) bool {
	var i Instance
	return e.get("instances", instanceID, &i) == nil && i.NodeID == nodeID
}

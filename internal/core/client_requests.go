package core

import (
	"errors"
	"net/netip"
	"time"
)

type ClientRequest struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Platform  string    `json:"platform"`
	IP        string    `json:"ip"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type storedClientRequest struct {
	ClientRequest
	SecretHash string `json:"secretHash"`
}
type ClientRequestTicket struct {
	ID        string    `json:"id"`
	Secret    string    `json:"secret"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type ClientRequestResult struct {
	State      string            `json:"state"`
	Connection *ClientConnection `json:"connection,omitempty"`
}
type ClientIPBlock struct {
	ID        string    `json:"id"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"createdAt"`
}

func (e *Engine) ClientIPBlocked(ip string) bool {
	var b ClientIPBlock
	return e.get("client_ip_blocks", credentialHash(ip), &b) == nil
}
func (e *Engine) ClientIPBlocks() ([]ClientIPBlock, error) {
	return list[ClientIPBlock](e, "client_ip_blocks")
}
func (e *Engine) UnblockClientIP(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.change("client_ip_blocks", id, nil, "client.ip.unblock", "解除来源 IP 黑名单")
}
func (e *Engine) ClientRequests() ([]ClientRequest, error) {
	records, err := list[storedClientRequest](e, "client_requests")
	out := []ClientRequest{}
	for _, r := range records {
		if (r.State == "pending" || r.State == "approved") && !e.clock().Before(r.ExpiresAt) {
			r.State = "expired"
		}
		out = append(out, r.ClientRequest)
	}
	return out, err
}
func (e *Engine) CreateClientRequest(name, platform, ip string) (ClientRequestTicket, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(name) == 0 || len(name) > 120 || !hasExact(platform, "linux", "darwin", "windows") {
		return ClientRequestTicket{}, errors.New("设备名称或平台无效")
	}
	if _, err := netip.ParseAddr(ip); err != nil {
		return ClientRequestTicket{}, errors.New("无法识别请求来源")
	}
	if e.ClientIPBlocked(ip) {
		return ClientRequestTicket{}, errors.New("此来源已被禁止接入")
	}
	records, err := e.ClientRequests()
	if err != nil {
		return ClientRequestTicket{}, err
	}
	count, total := 0, 0
	for _, r := range records {
		if e.clock().Before(r.ExpiresAt) {
			total++
			if r.IP == ip {
				count++
			}
		}
	}
	if count >= 5 || total >= 256 {
		return ClientRequestTicket{}, errors.New("接入申请过多，请稍后再试")
	}
	id, secret := ID(), ID()+ID()
	expires := e.clock().Add(10 * time.Minute)
	r := storedClientRequest{ClientRequest: ClientRequest{ID: id, Name: name, Platform: platform, IP: ip, State: "pending", CreatedAt: e.clock(), ExpiresAt: expires}, SecretHash: credentialHash(secret)}
	if err = e.change("client_requests", id, r, "client.request", "收到设备接入申请"); err != nil {
		return ClientRequestTicket{}, err
	}
	return ClientRequestTicket{ID: id, Secret: secret, ExpiresAt: expires}, nil
}
func (e *Engine) DecideClientRequest(id, decision string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var r storedClientRequest
	if err := e.get("client_requests", id, &r); err != nil {
		return errors.New("接入申请不存在")
	}
	if r.State != "pending" || !e.clock().Before(r.ExpiresAt) {
		return errors.New("申请已处理或已过期")
	}
	switch decision {
	case "approve":
		if e.ClientIPBlocked(r.IP) {
			return errors.New("此来源已被拉黑，请先解除黑名单")
		}
		r.State = "approved"
	case "reject":
		r.State = "rejected"
	case "block_ip":
		block := ClientIPBlock{ID: credentialHash(r.IP), IP: r.IP, CreatedAt: e.clock()}
		if err := e.change("client_ip_blocks", block.ID, block, "client.ip.block", "禁止来源 IP 接入"); err != nil {
			return err
		}
		r.State = "rejected"
	default:
		return errors.New("仅支持 approve、reject、block_ip")
	}
	return e.change("client_requests", id, r, "client.request."+decision, "处理设备接入申请")
}
func (e *Engine) PollClientRequest(id, secret, ip string) (ClientRequestResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var r storedClientRequest
	if len(secret) != 64 || e.get("client_requests", id, &r) != nil || credentialHash(secret) != r.SecretHash {
		return ClientRequestResult{}, errors.New("申请凭据无效")
	}
	if e.ClientIPBlocked(ip) || e.ClientIPBlocked(r.IP) {
		return ClientRequestResult{}, errors.New("此来源已被禁止接入")
	}
	if !e.clock().Before(r.ExpiresAt) {
		return ClientRequestResult{State: "expired"}, nil
	}
	if r.State != "approved" {
		return ClientRequestResult{State: r.State}, nil
	}
	r.State = "consumed"
	connection, err := e.issueClientCredential("client_requests", r.ID, r, r.Name, r.Platform)
	if err != nil {
		return ClientRequestResult{}, err
	}
	return ClientRequestResult{State: "approved", Connection: &connection}, nil
}

package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func prepareClientConnection(o *ClientOptions, m *clientManifest) error {
	for _, name := range []string{filepath.Join("adapter", "connection.json"), "enrolled.json"} {
		b, err := os.ReadFile(filepath.Join(o.Dir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		var saved ClientConnection
		if json.Unmarshal(b, &saved) != nil || saved.Endpoint != o.Endpoint || saved.Token == "" || len(saved.NodeID) != 32 {
			return errors.New("已有设备连接与安装地址不匹配，未修改")
		}
		if o.EnrollmentCode != "" {
			return errors.New("已有设备凭据，无需再次提供接入码；直接重新执行安装即可")
		}
		o.Token = saved.Token
		m.NodeID = saved.NodeID
		return nil
	}
	if o.Token != "" {
		return nil
	} // Internal/offline fixtures; public CLI uses enrollment only.
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("接入不允许重定向") }}
	var connection ClientConnection
	if o.EnrollmentCode == "" {
		var err error
		connection, err = awaitClientApproval(client, o)
		if err != nil {
			return err
		}
	} else {
		if len(o.EnrollmentCode) != 64 {
			return errors.New("一次性接入码格式无效")
		}
		res, err := client.Post(o.Endpoint+"/api/v1/client/enroll", "application/json", bytes.NewReader(mustJSON(map[string]string{"code": o.EnrollmentCode, "platform": runtime.GOOS})))
		if err != nil {
			return errors.New("无法连接风控服务，未安装 Hook")
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return errors.New("接入失败：接入码无效、已使用、已过期或服务拒绝请求")
		}
		if json.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&connection) != nil || len(connection.NodeID) != 32 || len(connection.Token) != 64 {
			return errors.New("服务返回了无效的设备凭据")
		}
	}
	connection.Endpoint = o.Endpoint
	o.Token = connection.Token
	m.NodeID = connection.NodeID
	// Keep a private retry record: a successfully consumed code must survive later filesystem errors.
	return AtomicFile(filepath.Join(o.Dir, "enrolled.json"), mustJSON(connection), 0600)
}

func awaitClientApproval(client *http.Client, o *ClientOptions) (ClientConnection, error) {
	type savedRequest struct {
		ClientRequestTicket
		Endpoint string `json:"endpoint"`
	}
	saved := savedRequest{}
	file := filepath.Join(o.Dir, "request.json")
	if b, err := os.ReadFile(file); err == nil {
		if json.Unmarshal(b, &saved) != nil || saved.Endpoint != o.Endpoint {
			return ClientConnection{}, errors.New("已有接入申请与服务地址不匹配")
		}
	} else if !os.IsNotExist(err) {
		return ClientConnection{}, err
	}
	if !time.Now().Before(saved.ExpiresAt) {
		name, _ := os.Hostname()
		if name == "" {
			name = runtime.GOOS + " device"
		}
		res, err := client.Post(o.Endpoint+"/api/v1/client/requests", "application/json", bytes.NewReader(mustJSON(map[string]string{"name": name, "platform": runtime.GOOS})))
		if err != nil {
			return ClientConnection{}, errors.New("无法提交接入申请，请检查风控服务地址")
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&saved.ClientRequestTicket)
		res.Body.Close()
		if res.StatusCode != 200 || err != nil || len(saved.ID) != 32 || len(saved.Secret) != 64 {
			return ClientConnection{}, errors.New("接入申请失败：服务拒绝请求或申请过多")
		}
		saved.Endpoint = o.Endpoint
		if err = AtomicFile(file, mustJSON(saved), 0600); err != nil {
			return ClientConnection{}, err
		}
	}
	if o.Progress != nil {
		fmt.Fprintf(o.Progress, "接入申请 %s\n请在控制台「Agent 接入 → 远程设备」核对申请编号并批准。最多等待十分钟，批准前不会写入 Hook。\n", saved.ID)
	}
	for time.Now().Before(saved.ExpiresAt) {
		req, _ := http.NewRequest("GET", o.Endpoint+"/api/v1/client/requests/"+saved.ID, nil)
		req.Header.Set("Authorization", "Bearer "+saved.Secret)
		res, err := client.Do(req)
		if err != nil {
			return ClientConnection{}, errors.New("接入等待中连接中断，可重新执行安装继续等待")
		}
		var result ClientRequestResult
		err = json.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&result)
		res.Body.Close()
		if res.StatusCode != 200 || err != nil {
			return ClientConnection{}, errors.New("服务拒绝接入申请查询，请检查来源是否被拉黑")
		}
		if result.State == "approved" && result.Connection != nil && len(result.Connection.Token) == 64 && len(result.Connection.NodeID) == 32 {
			// Persist the one-time credential before deleting the application ticket.
			result.Connection.Endpoint = o.Endpoint
			if err = AtomicFile(filepath.Join(o.Dir, "enrolled.json"), mustJSON(result.Connection), 0600); err != nil {
				return ClientConnection{}, err
			}
			_ = os.Remove(file)
			return *result.Connection, nil
		}
		if result.State != "pending" {
			_ = os.Remove(file)
			return ClientConnection{}, fmt.Errorf("接入申请结束（%s），未安装 Hook；需要时重新执行安装申请", result.State)
		}
		time.Sleep(2 * time.Second)
	}
	_ = os.Remove(file)
	return ClientConnection{}, errors.New("接入申请已过期，未安装 Hook")
}

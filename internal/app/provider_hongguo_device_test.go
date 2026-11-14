package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestHongguoDevicePoolNormalizeAndRotate(t *testing.T) {
	client := &hongguoAppClient{state: hongguoCatalogState{
		Version: 1, DeviceID: "8000000000000000001", InstallID: "8000000000000000009", Registered: true,
		Devices: []hongguoDevice{
			{DeviceID: "8000000000000000001", InstallID: "8000000000000000009"},
			{DeviceID: "8000000000000000001", InstallID: "8000000000000000009"},
			{DeviceID: "8000000000000000002", InstallID: "8000000000000000008"},
			{DeviceID: "bad", InstallID: "bad"},
		},
	}}
	normalizeHongguoDevicesLocked(client)
	if len(client.state.Devices) != 2 {
		t.Fatalf("设备池应去重并剔除无效项，实际 %v", client.state.Devices)
	}
	if !useHongguoDeviceLocked(client, "8000000000000000001") {
		t.Fatal("应能切换到池中其它设备")
	}
	if client.state.DeviceID != "8000000000000000002" {
		t.Fatalf("轮换目标错误: %s", client.state.DeviceID)
	}
	if !useHongguoDeviceLocked(client, "8000000000000000002") || client.state.DeviceID != "8000000000000000001" {
		t.Fatalf("应切回未被拒绝的设备: %s", client.state.DeviceID)
	}
	solo := &hongguoAppClient{state: hongguoCatalogState{
		Version: 1, DeviceID: "8000000000000000005", InstallID: "8000000000000000015", Registered: true,
		Devices: []hongguoDevice{{DeviceID: "8000000000000000005", InstallID: "8000000000000000015"}},
	}}
	if useHongguoDeviceLocked(solo, "8000000000000000005") {
		t.Fatal("只剩被拒绝设备时不应切换")
	}
}

func TestHongguoDevicePoolCapsSize(t *testing.T) {
	client := &hongguoAppClient{state: hongguoCatalogState{Version: 1, Feeds: map[string]hongguoCatalogCursor{}}}
	for _, id := range []string{"8000000000000000001", "8000000000000000002", "8000000000000000003", "8000000000000000004"} {
		addHongguoDeviceLocked(client, id, "8000000000000000010")
	}
	if len(client.state.Devices) != hongguoMaxDevices {
		t.Fatalf("设备池上限错误: %d", len(client.state.Devices))
	}
	if client.state.Devices[len(client.state.Devices)-1].DeviceID != "8000000000000000004" {
		t.Fatalf("最新注册设备应在池中: %v", client.state.Devices)
	}
	if client.state.Devices[0].DeviceID != "8000000000000000002" {
		t.Fatalf("应淘汰最早设备: %v", client.state.Devices)
	}
}

func TestHongguoAppRequestRotatesDeviceOnEmptyBody(t *testing.T) {
	var used []string
	d := rankingTestDownloader(t, func(request *http.Request) (*http.Response, error) {
		deviceID := request.URL.Query().Get("device_id")
		used = append(used, deviceID)
		if deviceID == "8000000000000000001" {
			return rankingHTTPResponse(request, 200, ""), nil
		}
		return rankingHTTPResponse(request, 200, `{"code":0}`), nil
	})
	client := d.hongguoClient()
	client.mu.Lock()
	client.state.Devices = []hongguoDevice{
		{DeviceID: "8000000000000000001", InstallID: "8000000000000000011"},
		{DeviceID: "8000000000000000002", InstallID: "8000000000000000012"},
	}
	client.state.DeviceID, client.state.InstallID, client.state.Registered = "8000000000000000001", "8000000000000000011", true
	client.mu.Unlock()

	if _, err := d.hongguoAppRequest(context.Background(), "POST", "/novel/player/video_model/v1/", nil, map[string]any{"video_id": "1"}); err != nil {
		t.Fatalf("轮换设备后应成功: %v", err)
	}
	if len(used) < 2 || used[0] != "8000000000000000001" || used[len(used)-1] != "8000000000000000002" {
		t.Fatalf("空正文后未切换设备: %v", used)
	}
}

func TestHongguoDevicePoolPersistsAcrossRestart(t *testing.T) {
	directory := t.TempDir()
	cfg := Config{OutputDir: t.TempDir(), dataDir: directory, Retries: 1, RequestConcurrency: 1, RequestIntervalMS: 10}
	first := NewDownloader(cfg)
	client := first.hongguoClient()
	client.mu.Lock()
	addHongguoDeviceLocked(client, "8000000000000000001", "8000000000000000011")
	addHongguoDeviceLocked(client, "8000000000000000002", "8000000000000000012")
	client.mu.Unlock()
	first.persistHongguoDevicePool()

	second := NewDownloader(cfg)
	restored := second.hongguoClient()
	restored.mu.Lock()
	devices, registered, current := len(restored.state.Devices), restored.state.Registered, restored.state.DeviceID
	restored.mu.Unlock()
	if !registered || devices != 2 || current != "8000000000000000002" {
		t.Fatalf("设备池未正确恢复: devices=%d registered=%v current=%s", devices, registered, current)
	}
}

func TestHongguoDevicePoolSkipsCorruptFile(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, hongguoDevicePoolFile), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if pool := loadHongguoDevicePool(directory); pool != nil {
		t.Fatalf("损坏文件应被忽略: %+v", pool)
	}
}

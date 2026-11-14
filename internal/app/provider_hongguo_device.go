package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"crypto/rand"
)

const (
	hongguoDeviceRegisterURL   = "https://log.snssdk.com/service/2/device_register/"
	hongguoDeviceRotations     = 3
	hongguoDeviceRegisterPause = 10 * time.Minute
)

type hongguoRegisterRejection struct{ error }

// hongguoAppQuery 是红果 App 的公共查询参数；设备注册时要去掉 device_id/iid。
func hongguoAppQuery(deviceID, installID string) url.Values {
	return url.Values{
		"aid": {"8662"}, "app_name": {"novelread"}, "version_code": {"73532"}, "version_name": {"7.3.5.32"},
		"manifest_version_code": {"73532"}, "update_version_code": {"73532"}, "channel": {"update_64"},
		"device_platform": {"android"}, "os": {"android"}, "ssmix": {"a"}, "device_type": {"25053RT47C"},
		"device_brand": {"Redmi"}, "language": {"zh"}, "os_api": {"36"}, "os_version": {"16"},
		"resolution": {"1280*2772"}, "dpi": {"520"}, "ac": {"wifi"}, "device_id": {deviceID}, "iid": {installID},
	}
}

func (downloader *Downloader) hongguoDevice() (string, string, bool) {
	client := downloader.hongguoClient()
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.state.DeviceID, client.state.InstallID, client.state.Registered
}

// ensureHongguoDevice 确保当前设备已向红果注册；rejected 为被上游判为无效的设备号。
func (downloader *Downloader) ensureHongguoDevice(ctx context.Context, rejected string) error {
	client := downloader.hongguoClient()
	settled := func() bool {
		current, _, registered := downloader.hongguoDevice()
		return rejected == "" && registered || rejected != "" && rejected != current
	}
	if settled() {
		return nil
	}
	client.deviceMu.Lock()
	defer client.deviceMu.Unlock()
	if settled() {
		return nil
	}
	if time.Now().Before(client.registerPausedUntil) {
		return fmt.Errorf("红果设备注册暂不可用：%v", client.registerPauseErr)
	}
	deviceID, installID, err := downloader.registerHongguoDevice(ctx)
	if err != nil {
		var rejection hongguoRegisterRejection
		if errors.As(err, &rejection) {
			client.registerPausedUntil, client.registerPauseErr = time.Now().Add(hongguoDeviceRegisterPause), err
		}
		return err
	}
	client.mu.Lock()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = deviceID, installID, true
	client.mu.Unlock()
	return nil
}

func (downloader *Downloader) registerHongguoDevice(ctx context.Context) (string, string, error) {
	header := map[string]any{
		"display_name": "红果免费短剧", "aid": 8662, "app_version": "7.3.5.32", "version_code": 73532,
		"manifest_version_code": 73532, "update_version_code": 73532, "channel": "update_64", "package": "com.phoenix.read",
		"app_language": "zh", "os": "Android", "os_version": "16", "os_api": 36, "device_model": "25053RT47C",
		"device_brand": "Redmi", "device_manufacturer": "Xiaomi", "cpu_abi": "arm64-v8a", "density_dpi": 520,
		"display_density": "xxhdpi", "resolution": "2772x1280", "language": "zh", "timezone": 8, "access": "wifi",
		"openudid": randomHex(8), "clientudid": uuidLike(), "cdid": uuidLike(),
		"region": "CN", "tz_name": "Asia/Shanghai", "tz_offset": 28800, "sim_region": "cn", "device_platform": "android",
		"not_request_sender": 0, "is_system_app": 0, "sdk_flavor": "china", "rom": "OS2.0", "rom_version": "BP2A.250605.031.A3",
	}
	body, err := json.Marshal(map[string]any{"magic_tag": "ss_app_log", "header": header, "_gen_time": time.Now().UnixMilli()})
	if err != nil {
		return "", "", err
	}
	query := hongguoAppQuery("", "")
	query.Del("device_id")
	query.Del("iid")
	query.Set("req_id", uuidLike())
	query.Set("_rticket", strconv.FormatInt(time.Now().UnixMilli(), 10))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, hongguoDeviceRegisterURL+"?"+query.Encode(), bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	request.Header.Set("User-Agent", hongguoAppUserAgent)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, err := downloader.doCatalogRequestWithTimeout(request, 20*time.Second)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()
	content := make([]byte, 0, 64<<10)
	buffer := make([]byte, 32<<10)
	for {
		n, readErr := response.Body.Read(buffer)
		content = append(content, buffer[:n]...)
		if len(content) > 64<<10 {
			return "", "", hongguoRegisterRejection{errors.New("红果设备注册响应超出大小限制")}
		}
		if readErr != nil {
			break
		}
	}
	if response.StatusCode != http.StatusOK {
		return "", "", hongguoRegisterRejection{fmt.Errorf("红果设备注册 HTTP %d", response.StatusCode)}
	}
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return "", "", hongguoRegisterRejection{errors.New("红果设备注册返回格式异常")}
	}
	deviceID, installID := mapString(result, "device_id_str", "device_id"), mapString(result, "install_id_str", "install_id")
	if !hongguoNumericID.MatchString(deviceID) || !hongguoNumericID.MatchString(installID) || deviceID == "0" || installID == "0" {
		return "", "", hongguoRegisterRejection{errors.New("红果设备注册未返回设备号")}
	}
	return deviceID, installID, nil
}

func uuidLike() string {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	random[6] = random[6]&0x0f | 0x40
	random[8] = random[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", random[0:4], random[4:6], random[6:8], random[8:10], random[10:16])
}

package kontainerdrivermetadata

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/rancher/norman/types/convert"
	"github.com/rancher/rancher/pkg/settings"
	"github.com/rancher/rke/types/kdm"
)

func parseURL(rkeData map[string]interface{}) (*MetadataURL, error) {
	url := &MetadataURL{}
	path, ok := rkeData["url"]
	if !ok {
		return nil, fmt.Errorf("url not present in settings %s", settings.RkeMetadataConfig.Get())
	}
	url.path = convert.ToString(path)
	return url, nil
}

func loadData(url *MetadataURL) (kdm.Data, error) {
	return getDataHTTP(url.path)
}

func getDataHTTP(url string) (kdm.Data, error) {
	var data kdm.Data
	resp, err := httpClient.Get(url)
	if err != nil {
		return data, fmt.Errorf("driverMetadata err %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return data, fmt.Errorf("driverMetadata statusCode %v", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return data, fmt.Errorf("driverMetadata read response body error %v", err)
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return data, fmt.Errorf("driverMetadata %v", err)
	}
	return data, nil
}

func getSettingValues(value string) (map[string]interface{}, error) {
	urlData := map[string]interface{}{}
	if err := json.Unmarshal([]byte(value), &urlData); err != nil {
		return nil, fmt.Errorf("unmarshal err %v", err)
	}
	return urlData, nil
}

func deleteMap(url *MetadataURL) {
	key := getKey(url)
	fileMapLock.Lock()
	delete(fileMapData, key)
	fileMapLock.Unlock()
}

func storeMap(url *MetadataURL) bool {
	key := getKey(url)
	fileMapLock.Lock()
	defer fileMapLock.Unlock()
	if _, ok := fileMapData[key]; ok {
		return false
	}
	fileMapData[key] = true
	return true
}

func getKey(url *MetadataURL) string {
	return url.path
}

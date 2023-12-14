package kontainerdrivermetadata

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/rancher/rke/types/kdm"
)

func loadData(url string) (kdm.Data, error) {
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

	if err = json.Unmarshal(body, &data); err != nil {
		return data, fmt.Errorf("driverMetadata %v", err)
	}
	return data, nil
}

func deleteMap(key string) {
	fileMapLock.Lock()
	defer fileMapLock.Unlock()
	delete(fileMapData, key)
}

func storeMap(key string) bool {
	fileMapLock.Lock()
	defer fileMapLock.Unlock()
	if _, ok := fileMapData[key]; ok {
		return false
	}
	fileMapData[key] = struct{}{}
	return true
}

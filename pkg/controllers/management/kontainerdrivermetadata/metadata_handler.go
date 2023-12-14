package kontainerdrivermetadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/pkg/errors"
	managementv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	"github.com/rancher/rancher/pkg/channelserver"
	v1 "github.com/rancher/rancher/pkg/generated/norman/core/v1"
	v3 "github.com/rancher/rancher/pkg/generated/norman/management.cattle.io/v3"
	"github.com/rancher/rancher/pkg/namespace"
	"github.com/rancher/rancher/pkg/settings"
	"github.com/rancher/rancher/pkg/types/config"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime"
)

type MetadataController struct {
	NamespacesLister     v1.NamespaceLister
	SystemImagesLister   v3.RkeK8sSystemImageLister
	SystemImages         v3.RkeK8sSystemImageInterface
	ServiceOptionsLister v3.RkeK8sServiceOptionLister
	ServiceOptions       v3.RkeK8sServiceOptionInterface
	AddonsLister         v3.RkeAddonLister
	Addons               v3.RkeAddonInterface
	SettingLister        v3.SettingLister
	Settings             v3.SettingInterface
	url                  string
}

var (
	httpClient = &http.Client{
		Timeout: time.Second * 30,
	}
	fileMapLock = sync.Mutex{}
	fileMapData = map[string]struct{}{}
)

func Register(ctx context.Context, management *config.ManagementContext) {
	mgmt := management.Management

	m := &MetadataController{
		SystemImagesLister:   mgmt.RkeK8sSystemImages("").Controller().Lister(),
		SystemImages:         mgmt.RkeK8sSystemImages(""),
		ServiceOptionsLister: mgmt.RkeK8sServiceOptions("").Controller().Lister(),
		ServiceOptions:       mgmt.RkeK8sServiceOptions(""),
		NamespacesLister:     management.Core.Namespaces("").Controller().Lister(),
		AddonsLister:         mgmt.RkeAddons("").Controller().Lister(),
		Addons:               mgmt.RkeAddons(""),
		SettingLister:        mgmt.Settings("").Controller().Lister(),
		Settings:             mgmt.Settings(""),
	}

	mgmt.Settings("").AddHandler(ctx, "rke-metadata-handler", m.sync)
	mgmt.Settings("").Controller().Enqueue("", settings.RkeMetadataConfig.Name)
}

func (m *MetadataController) sync(_ string, setting *managementv3.Setting) (runtime.Object, error) {
	if setting == nil || (setting.Name != settings.RkeMetadataConfig.Name) {
		return nil, nil
	}

	if _, err := m.NamespacesLister.Get("", namespace.GlobalNamespace); err != nil {
		return nil, fmt.Errorf("failed to get %s namespace", namespace.GlobalNamespace)
	}

	value := setting.Value
	if value == "" {
		value = setting.Default
	}

	config, err := getMetadataConfig()
	if err != nil {
		return nil, err
	}

	m.url = config.URL

	interval := config.RefreshIntervalMinutes

	if interval > 0 {
		logrus.Infof("Refreshing driverMetadata in %v minutes", interval)
		m.Settings.Controller().EnqueueAfter(setting.Namespace, setting.Name, time.Minute*time.Duration(interval))
	}

	// refresh to sync k3s/rke2 releases
	channelserver.Refresh()
	return setting, m.refresh()
}

func (m *MetadataController) refresh() error {
	if !storeMap(m.url) {
		logrus.Infof("driverMetadata: already in progress")
		return nil
	}
	defer deleteMap(m.url)
	if err := m.Refresh(m.url); err != nil {
		logrus.Warnf("%v, Fallback to refresh from local file path %v", err, DataJSONLocation)
		return errors.Wrapf(m.createOrUpdateMetadataFromLocal(), "failed to refresh from local file path: %s", DataJSONLocation)
	}
	return nil
}

func (m *MetadataController) Refresh(url string) error {
	data, err := loadData(url)
	if err != nil {
		return errors.Wrapf(err, "failed to refresh data from upstream %v", url)
	}
	logrus.Infof("driverMetadata: refreshing data from upstream %v", url)
	return errors.Wrap(m.createOrUpdateMetadata(data), "failed to create or update driverMetadata")
}

func getMetadataConfig() (settings.MetadataConfig, error) {
	config := settings.MetadataConfig{}
	err := json.Unmarshal([]byte(settings.RkeMetadataConfig.Get()), &config)
	if err != nil {
		return config, err
	}
	return config, nil
}

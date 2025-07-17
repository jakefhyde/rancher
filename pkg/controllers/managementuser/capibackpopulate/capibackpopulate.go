package capibackpopulate

import (
	"context"

	apimgmtv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	capicontrollers "github.com/rancher/rancher/pkg/generated/controllers/cluster.x-k8s.io/v1beta1"
	"github.com/rancher/rancher/pkg/types/config"
	corecontrollers "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
)

type capiBackPopulate struct {
	clusterName      string
	capiClusters     capicontrollers.ClusterClient
	capiClusterCache capicontrollers.ClusterCache
	nodeCache        corecontrollers.NodeCache
}

func Register(ctx context.Context, downstream *config.UserContext) {
	if downstream.ClusterName == "local" {
		return
	}

	c := &capiBackPopulate{
		clusterName:      downstream.ClusterName,
		capiClusters:     downstream.Management.Wrangler.CAPI.Cluster(),
		capiClusterCache: downstream.Management.Wrangler.CAPI.Cluster().Cache(),
		nodeCache:        downstream.Corew.Node().Cache(),
	}

	downstream.Management.Wrangler.Mgmt.Cluster().OnChange(ctx, "capibackpopulate", c.backPopulateCAPIObjects)
}

func (c *capiBackPopulate) backPopulateCAPIObjects(key string, mgmtCluster *apimgmtv3.Cluster) (*apimgmtv3.Cluster, error) {
	if key != c.clusterName {
		return nil, nil
	}

	return mgmtCluster, nil
}

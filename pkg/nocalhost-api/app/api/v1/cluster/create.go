/*
Copyright 2020 The Nocalhost Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cluster

import (
	"encoding/base64"
	"gopkg.in/yaml.v2"
	"nocalhost/internal/nocalhost-api/global"
	"nocalhost/internal/nocalhost-api/service"
	"nocalhost/pkg/nocalhost-api/app/api"
	"nocalhost/pkg/nocalhost-api/pkg/clientgo"
	"nocalhost/pkg/nocalhost-api/pkg/errno"
	"nocalhost/pkg/nocalhost-api/pkg/log"
	"nocalhost/pkg/nocalhost-api/pkg/setupcluster"

	"github.com/gin-gonic/gin"
)

// Create 添加集群
// @Summary 添加集群
// @Description 用户添加集群，暂时不考虑验证集群的 kubeconfig
// @Tags 集群
// @Accept  json
// @Produce  json
// @param Authorization header string true "Authorization"
// @Param createCluster body cluster.CreateClusterRequest true "The cluster info"
// @Success 200 {object} api.Response "{"code":0,"message":"OK","data":null}"
// @Router /v1/cluster [post]
func Create(c *gin.Context) {
	var req CreateClusterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf("createCluster bind params err: %v", err)
		api.SendResponse(c, errno.ErrBind, nil)
		return
	}
	// decode kubeconfig
	DecKubeconfig, err := base64.StdEncoding.DecodeString(req.KubeConfig)
	if err != nil {
		api.SendResponse(c, errno.ErrClusterKubeErr, nil)
		return
	}
	// check kubeconfig server already exist
	t := KubeConfig{}
	yaml.Unmarshal(DecKubeconfig, &t)
	// only allow one cluster kubeconfig
	if len(t.Clusters) > 1 {
		api.SendResponse(c, errno.ErrClusterKubeCreate, nil)
		return
	}
	where := make(map[string]interface{}, 0)
	where["server"] = t.Clusters[0].Cluster.Server
	_, err = service.Svc.ClusterSvc().GetAny(c, where)
	if err == nil {
		api.SendResponse(c, errno.ErrClusterExistCreate, nil)
		return
	}

	// get client go
	goClient, err := clientgo.NewGoClient(DecKubeconfig)
	if err != nil {
		api.SendResponse(c, errno.ErrClusterKubeErr, nil)
		return
	}

	// 1. check is admin Kubeconfig
	// 2. check if Namespace nocalhost-reserved already exist, ignore cause by nocalhost-dep-job installer.sh has checkout this condition and will exit
	// 3. use admin Kubeconfig create configmap for nocalhost-dep-job to create admission webhook cert
	// 4. deploy nocalhost-dep-job and pull on nocalhost-dep
	// see https://codingcorp.coding.net/p/nocalhost/wiki/115
	clusterSetUp := setupcluster.NewSetUpCluster(goClient)
	err, errRes := clusterSetUp.IsAdmin().CreateNs(global.NocalhostSystemNamespace, "").CreateConfigMap(global.NocalhostDepKubeConfigMapName, global.NocalhostSystemNamespace, global.NocalhostDepKubeConfigMapKey, string(DecKubeconfig)).DeployNocalhostDep("", global.NocalhostSystemNamespace).GetErr()
	if err != nil {
		api.SendResponse(c, errRes, nil)
		return
	}

	// TODO 异步获取集群信息例如 NODE 节点、版本号等

	userId, _ := c.Get("userId")
	err = service.Svc.ClusterSvc().Create(c, req.Name, req.Marks, string(DecKubeconfig), t.Clusters[0].Cluster.Server, userId.(uint64))
	if err != nil {
		log.Warnf("create cluster err: %v", err)
		api.SendResponse(c, errno.ErrClusterCreate, nil)
		return
	}

	api.SendResponse(c, nil, nil)
}
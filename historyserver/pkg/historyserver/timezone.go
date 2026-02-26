package historyserver

import (
	"encoding/json"
	"io"
	"net/http"
	"path"

	"github.com/emicklei/go-restful/v3"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/utils"
)

const timezoneEndpoint = "/timezone"

func (s *ServerHandler) getTimezone(req *restful.Request, resp *restful.Response) {
	sessionName := req.Attribute(COOKIE_SESSION_NAME_KEY).(string)
	if sessionName == "live" {
		s.redirectRequest(req, resp)
		return
	}

	clusterName := req.Attribute(COOKIE_CLUSTER_NAME_KEY).(string)
	clusterNamespace := req.Attribute(COOKIE_CLUSTER_NAMESPACE_KEY).(string)
	clusterNameID := clusterName + "_" + clusterNamespace

	storageKey := utils.EndpointPathToStorageKey(timezoneEndpoint)
	endpointPath := path.Join(sessionName, utils.RAY_SESSIONDIR_FETCHED_ENDPOINTS_NAME, storageKey)
	reader := s.reader.GetContent(clusterNameID, endpointPath)
	if reader == nil {
		respData, err := json.Marshal(map[string]string{"offset": "", "value": ""})
		if err != nil {
			logrus.Errorf("Failed to marshal timezone response: %v", err)
			resp.WriteErrorString(http.StatusInternalServerError, err.Error())
			return
		}
		resp.Write(respData)
		return
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		logrus.Errorf("Failed to read timezone metadata: %v", err)
		resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	var timezoneData map[string]string
	if err := json.Unmarshal(content, &timezoneData); err != nil {
		logrus.Errorf("Failed to parse timezone metadata: %v", err)
		resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}

	respData, err := json.Marshal(timezoneData)
	if err != nil {
		logrus.Errorf("Failed to marshal timezone response: %v", err)
		resp.WriteErrorString(http.StatusInternalServerError, err.Error())
		return
	}
	resp.Write(respData)
}

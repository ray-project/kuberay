package eventserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/emicklei/go-restful/v3"
	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"

	"github.com/ray-project/kuberay/historyserver/pkg/collector/logcollector/storage"
)

type Event struct {
	Data      map[string]interface{}
	Timestamp time.Time

	SessionName string
	NodeID      string
}

type EventServer struct {
	storageWriter      storage.StorageWriter
	stopped            chan struct{}
	clusterID          string
	sessionDir         string
	nodeID             string
	clusterName        string
	sessionName        string
	root               string
	currentSessionName string
	currentNodeID      string
	events             []Event
	flushInterval      time.Duration
	mutex              sync.Mutex
}

func NewEventServer(writer storage.StorageWriter, rootDir, sessionDir, nodeID, clusterName, clusterID, sessionName string) *EventServer {
	server := &EventServer{
		events:             make([]Event, 0),
		storageWriter:      writer,
		root:               rootDir,
		sessionDir:         sessionDir,
		nodeID:             nodeID,
		clusterName:        clusterName,
		clusterID:          clusterID,
		sessionName:        sessionName,
		mutex:              sync.Mutex{},
		flushInterval:      time.Hour, // 默认每小时刷新一次
		stopped:            make(chan struct{}),
		currentSessionName: sessionName, // 初始化为配置中的sessionName
		currentNodeID:      nodeID,      // 初始化为配置中的nodeID
	}

	// 启动监听nodeID文件变化的goroutine
	go server.watchNodeIDFile()

	return server
}

func (es *EventServer) InitServer(port int) {
	ws := new(restful.WebService)
	ws.Path("/v1")
	ws.Consumes(restful.MIME_JSON)
	ws.Produces(restful.MIME_JSON)

	ws.Route(ws.POST("/events").To(es.PersistEvents))

	restful.Add(ws)

	go func() {
		logrus.Infof("Starting event server on port %d", port)
		logrus.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
	}()
	go func() {
		es.periodicFlush()
	}()

	// Handle SIGTERM signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigChan
		logrus.Info("Received SIGTERM, flushing events to storage")
		es.flushEvents()
		close(es.stopped)
	}()
}

// watchNodeIDFile 监听 /tmp/ray/raylet_node_id 文件内容变化
func (es *EventServer) watchNodeIDFile() {
	nodeIDFilePath := "/tmp/ray/raylet_node_id"

	// 创建一个新的 watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logrus.Errorf("Failed to create file watcher: %v", err)
		return
	}
	defer watcher.Close()

	// 添加文件到监控列表
	err = watcher.Add(nodeIDFilePath)
	if err != nil {
		logrus.Infof("Failed to add %s to watcher, will watch for file creation: %v", nodeIDFilePath, err)
		// 如果文件不存在，监控其父目录
		err = watcher.Add("/tmp/ray")
		if err != nil {
			logrus.Errorf("Failed to watch directory /tmp/ray: %v", err)
			return
		}
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// 检查是否是我们关心的文件
			if event.Name == nodeIDFilePath && (event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create) {
				// 读取文件内容
				content, err := os.ReadFile(nodeIDFilePath)
				if err != nil {
					logrus.Errorf("Failed to read node ID file %s: %v", nodeIDFilePath, err)
					continue
				}

				// 去除空白字符
				newNodeID := strings.TrimSpace(string(content))
				if newNodeID == "" {
					continue
				}

				// 检查nodeID是否发生变化
				es.mutex.Lock()
				if es.currentNodeID != newNodeID {
					oldNodeID := es.currentNodeID
					logrus.Infof("Node ID changed from %s to %s, flushing events", oldNodeID, newNodeID)

					// 更新当前nodeID
					es.currentNodeID = newNodeID

					// 收集具有相同节点ID的事件
					var eventsToFlush []Event
					var remainingEvents []Event
					for _, event := range es.events {
						if event.NodeID == oldNodeID {
							eventsToFlush = append(eventsToFlush, event)
						} else if event.NodeID == newNodeID {
							remainingEvents = append(remainingEvents, event)
						} else {
							logrus.Errorf("Drop event with nodeId %v, event: %v", event.NodeID, event.Data)
						}
					}

					// 更新事件列表，只保留不同节点ID的事件
					es.events = remainingEvents
					es.mutex.Unlock()

					// 刷新具有相同节点ID的事件
					if len(eventsToFlush) > 0 {
						go es.flushEventsInternal(eventsToFlush)
					}
					continue
				}
				es.mutex.Unlock()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logrus.Errorf("File watcher error: %v", err)
		case <-es.stopped:
			logrus.Info("Event server stopped, exiting node ID watcher")
			return
		}
	}
}

func (es *EventServer) PersistEvents(req *restful.Request, resp *restful.Response) {
	body, err := io.ReadAll(req.Request.Body)
	if err != nil {
		logrus.Errorf("Failed to read request body: %v", err)
		resp.WriteError(http.StatusBadRequest, err)
		return
	}

	var eventDatas []map[string]interface{}
	if err := json.Unmarshal(body, &eventDatas); err != nil {
		logrus.Errorf("Failed to unmarshal event: %v", err)
		resp.WriteError(http.StatusBadRequest, err)
		return
	}

	for _, eventData := range eventDatas {
		// 解析时间戳
		timestampStr, ok := eventData["timestamp"].(string)
		if !ok {
			logrus.Errorf("Event timestamp not found or not a string")
			resp.WriteError(http.StatusBadRequest, fmt.Errorf("timestamp not found"))
			return
		}

		// 获取事件中的sessionName
		sessionNameStr, ok := eventData["sessionName"].(string)
		if !ok {
			logrus.Errorf("Event sessionName not found or not a string")
			resp.WriteError(http.StatusBadRequest, fmt.Errorf("sessionName not found"))
			return
		}

		timestamp, err := time.Parse(time.RFC3339Nano, timestampStr)
		if err != nil {
			logrus.Errorf("Failed to parse timestamp: %v", err)
			resp.WriteError(http.StatusBadRequest, err)
			return
		}

		es.mutex.Lock()
		event := Event{
			Data:        eventData,
			Timestamp:   timestamp,
			SessionName: sessionNameStr,
			NodeID:      es.currentNodeID, // 存储事件到达时的currentNodeID
		}
		es.events = append(es.events, event)

		// 检查sessionName是否发生变化
		if es.currentSessionName != sessionNameStr {
			logrus.Infof("Session name changed from %s to %s, flushing events", es.currentSessionName, sessionNameStr)
			// 保存当前事件后再刷新
			eventsToFlush := make([]Event, len(es.events))
			copy(eventsToFlush, es.events)

			// 清空事件列表
			es.events = es.events[:0]

			// 更新当前sessionName
			es.currentSessionName = sessionNameStr

			// 解锁后执行刷新操作
			es.mutex.Unlock()

			// 刷新之前的事件
			es.flushEventsInternal(eventsToFlush)
			return
		}
		es.mutex.Unlock()

		logrus.Infof("Received event with ID: %v at %v, session: %s, node: %s", eventData["eventId"], timestamp, sessionNameStr, es.currentNodeID)
	}

	resp.WriteHeader(http.StatusOK)
}

func (es *EventServer) periodicFlush() {
	ticker := time.NewTicker(es.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			logrus.Info("Periodic flush triggered")
			es.flushEvents()
		case <-es.stopped:
			logrus.Info("Event server stopped, exiting periodic flush")
			return
		}
	}
}

func (es *EventServer) WaitForStop() <-chan struct{} {
	return es.stopped
}

func (es *EventServer) flushEvents() {
	es.mutex.Lock()
	if len(es.events) == 0 {
		es.mutex.Unlock()
		logrus.Info("No events to flush")
		return
	}

	// 复制当前事件列表
	eventsToFlush := make([]Event, len(es.events))
	copy(eventsToFlush, es.events)

	// 清空事件列表
	es.events = es.events[:0]
	es.mutex.Unlock()

	// 执行实际的刷新操作
	es.flushEventsInternal(eventsToFlush)
}

// flushEventsInternal 实际执行事件刷新的操作
func (es *EventServer) flushEventsInternal(eventsToFlush []Event) {
	// 按小时和事件类型分组事件
	nodeEventsByHour := make(map[string][]Event) // 节点相关事件
	jobEventsByHour := make(map[string][]Event)  // 作业相关事件

	// 分类事件
	for _, event := range eventsToFlush {
		hourKey := event.Timestamp.Truncate(time.Hour).Format("2006-01-02-15")

		// 检查事件类型
		if es.isNodeEvent(event.Data) {
			// 节点相关事件
			nodeEventsByHour[hourKey] = append(nodeEventsByHour[hourKey], event)
		} else if jobID := es.getJobID(event.Data); jobID != "" {
			// 作业相关事件，使用 jobID-hour 作为键
			jobKey := fmt.Sprintf("%s-%s", jobID, hourKey)
			jobEventsByHour[jobKey] = append(jobEventsByHour[jobKey], event)
		} else {
			// 默认归类为节点事件
			nodeEventsByHour[hourKey] = append(nodeEventsByHour[hourKey], event)
		}
	}

	// 并发上传所有事件
	var wg sync.WaitGroup
	errChan := make(chan error, len(nodeEventsByHour)+len(jobEventsByHour))

	// 上传节点相关事件
	for hour, events := range nodeEventsByHour {
		wg.Add(1)
		go func(hourKey string, hourEvents []Event) {
			defer wg.Done()
			if err := es.flushNodeEventsForHour(hourKey, hourEvents); err != nil {
				errChan <- err
			}
		}(hour, events)
	}

	// 上传作业相关事件
	for jobHour, events := range jobEventsByHour {
		wg.Add(1)
		go func(jobHourKey string, hourEvents []Event) {
			defer wg.Done()
			// 分离 jobID 和 hourKey
			parts := strings.SplitN(jobHourKey, "-", 4) // 日期格式中有3个短横线，所以用4个部分
			if len(parts) < 4 {
				errChan <- fmt.Errorf("invalid job hour key: %s", jobHourKey)
				return
			}

			jobID := parts[0]
			hourKey := strings.Join(parts[1:], "-") // 重新组合时间为 hourKey

			if err := es.flushJobEventsForHour(jobID, hourKey, hourEvents); err != nil {
				errChan <- err
			}
		}(jobHour, events)
	}

	wg.Wait()
	close(errChan)

	// 检查是否有错误
	for err := range errChan {
		logrus.Errorf("Error flushing events: %v", err)
	}

	totalEvents := len(eventsToFlush)
	logrus.Infof("Successfully flushed %d events to storage (%d node events, %d job events)",
		totalEvents,
		countEventsInMap(nodeEventsByHour),
		countEventsInMap(jobEventsByHour))
}

// countEventsInMap 计算 map 中所有事件的数量
func countEventsInMap(eventsMap map[string][]Event) int {
	count := 0
	for _, events := range eventsMap {
		count += len(events)
	}
	return count
}

var nodeEventType = []string{"NODE_LIFECYCLE_EVENT", "NODE_DEFINITION_EVENT"}

// isNodeEvent 检查事件是否为节点相关事件
func (es *EventServer) isNodeEvent(eventData map[string]interface{}) bool {
	eventType, ok := eventData["eventType"].(string)
	if !ok {
		return false
	}
	for _, nodeEvent := range nodeEventType {
		if eventType == nodeEvent {
			return true
		}
	}
	return false
}

// getJobID 获取事件关联的作业ID
func (es *EventServer) getJobID(eventData map[string]interface{}) string {
	if jobID, hasJob := eventData["jobId"]; hasJob && jobID != "" {
		return fmt.Sprintf("%v", jobID)
	}
	return ""
}

// flushNodeEventsForHour 刷新节点相关事件到存储
func (es *EventServer) flushNodeEventsForHour(hourKey string, events []Event) error {
	// 创建事件数据
	eventsData := make([]map[string]interface{}, len(events))
	for i, event := range events {
		eventsData[i] = event.Data
	}

	data, err := json.Marshal(eventsData)
	if err != nil {
		return fmt.Errorf("failed to marshal node events: %w", err)
	}

	reader := bytes.NewReader(data)

	// 使用事件中的sessionName而不是配置中的sessionName
	sessionNameToUse := es.sessionName // 默认使用配置中的sessionName
	if len(events) > 0 && events[0].SessionName != "" {
		sessionNameToUse = events[0].SessionName
	}

	// 使用事件中的NodeID而不是当前的NodeID
	nodeIDToUse := es.nodeID // 默认使用配置中的nodeID
	if len(events) > 0 && events[0].NodeID != "" {
		nodeIDToUse = events[0].NodeID
	}

	// 构建节点事件存储路径，使用事件中的nodeID
	basePath := path.Join(
		es.root,
		fmt.Sprintf("%s_%s", es.clusterName, es.clusterID),
		sessionNameToUse,
		"node_events",
		fmt.Sprintf("%s-%s", nodeIDToUse, hourKey))

	// 确保存储目录存在
	dir := path.Dir(basePath)
	if err := es.storageWriter.CreateDirectory(dir); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// 写入事件文件
	if err := es.storageWriter.WriteFile(basePath, reader); err != nil {
		return fmt.Errorf("failed to write node events file %s: %w", basePath, err)
	}

	logrus.Infof("Successfully flushed %d node events for hour %s to %s, context: %s", len(events), hourKey, basePath, string(data))
	return nil
}

// flushJobEventsForHour 刷新作业相关事件到存储
func (es *EventServer) flushJobEventsForHour(jobID, hourKey string, events []Event) error {
	// 创建事件数据
	eventsData := make([]map[string]interface{}, len(events))
	for i, event := range events {
		eventsData[i] = event.Data
	}

	data, err := json.Marshal(eventsData)
	if err != nil {
		return fmt.Errorf("failed to marshal job events: %w", err)
	}

	reader := bytes.NewReader(data)

	// 使用事件中的sessionName而不是配置中的sessionName
	sessionNameToUse := es.sessionName // 默认使用配置中的sessionName
	if len(events) > 0 && events[0].SessionName != "" {
		sessionNameToUse = events[0].SessionName
	}

	// 使用事件中的NodeID而不是当前的NodeID
	nodeIDToUse := es.nodeID // 默认使用配置中的nodeID
	if len(events) > 0 && events[0].NodeID != "" {
		nodeIDToUse = events[0].NodeID
	}

	// 构建作业事件存储路径，使用事件中的nodeID
	basePath := path.Join(
		es.root,
		fmt.Sprintf("%s_%s", es.clusterName, es.clusterID),
		sessionNameToUse,
		"job_events",
		jobID,
		fmt.Sprintf("%s-%s", nodeIDToUse, hourKey))

	// 确保存储目录存在
	dir := path.Dir(basePath)
	if err := es.storageWriter.CreateDirectory(dir); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// 写入事件文件
	if err := es.storageWriter.WriteFile(basePath, reader); err != nil {
		return fmt.Errorf("failed to write job events file %s: %w", basePath, err)
	}

	logrus.Infof("Successfully flushed %d job events for job %s hour %s to %s", len(events), jobID, hourKey, basePath)
	return nil
}

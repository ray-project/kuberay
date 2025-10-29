package server

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
	"github.com/ray-project/kuberay/historyserver/backend/collector/storage"
	"github.com/sirupsen/logrus"
)

type Event struct {
	Data      map[string]interface{}
	Timestamp time.Time
}

type EventServer struct {
	events        []Event
	storageWriter storage.StorageWritter
	sessionDir    string
	nodeID        string
	clusterName   string
	clusterID     string
	sessionName   string
	mutex         sync.Mutex
	flushInterval time.Duration
	stopChan      chan struct{}
}

func NewEventServer(writer storage.StorageWritter, sessionDir, nodeID, clusterName, clusterID, sessionName string) *EventServer {
	server := &EventServer{
		events:        make([]Event, 0),
		storageWriter: writer,
		sessionDir:    sessionDir,
		nodeID:        nodeID,
		clusterName:   clusterName,
		clusterID:     clusterID,
		sessionName:   sessionName,
		mutex:         sync.Mutex{},
		flushInterval: time.Hour, // 默认每小时刷新一次
		stopChan:      make(chan struct{}),
	}

	// 启动定期刷新 goroutine
	go server.periodicFlush()

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

	// Handle SIGTERM signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		select {
		case <-sigChan:
			logrus.Info("Received SIGTERM, flushing events to storage")
			es.Stop()
			os.Exit(0)
		case <-es.stopChan:
			// Server is stopping
			return
		}
	}()
}

func (es *EventServer) PersistEvents(req *restful.Request, resp *restful.Response) {
	body, err := io.ReadAll(req.Request.Body)
	if err != nil {
		logrus.Errorf("Failed to read request body: %v", err)
		resp.WriteError(http.StatusBadRequest, err)
		return
	}

	var eventData map[string]interface{}
	if err := json.Unmarshal(body, &eventData); err != nil {
		logrus.Errorf("Failed to unmarshal event: %v", err)
		resp.WriteError(http.StatusBadRequest, err)
		return
	}

	// 解析时间戳
	timestampStr, ok := eventData["timestamp"].(string)
	if !ok {
		logrus.Errorf("Event timestamp not found or not a string")
		resp.WriteError(http.StatusBadRequest, fmt.Errorf("timestamp not found"))
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
		Data:      eventData,
		Timestamp: timestamp,
	}
	es.events = append(es.events, event)
	es.mutex.Unlock()

	logrus.Infof("Received event with ID: %v at %v", eventData["eventId"], timestamp)

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
		case <-es.stopChan:
			// Server is stopping, do final flush
			logrus.Info("Final flush before stopping server")
			es.flushEvents()
			return
		}
	}
}

func (es *EventServer) Stop() {
	close(es.stopChan)
	// 等待最后一次刷新完成
	time.Sleep(100 * time.Millisecond)
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

// isNodeEvent 检查事件是否为节点相关事件
func (es *EventServer) isNodeEvent(eventData map[string]interface{}) bool {
	if sourceType, ok := eventData["sourceType"].(string); ok && sourceType == "CORE_WORKER" {
		return true
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
		return fmt.Errorf("failed to marshal node events: %v", err)
	}

	reader := bytes.NewReader(data)

	// 构建节点事件存储路径
	basePath := path.Join(
		fmt.Sprintf("%s_%s", es.clusterName, es.clusterID),
		es.sessionName,
		"node_events",
		fmt.Sprintf("%s-%s", es.nodeID, hourKey))

	// 确保存储目录存在
	dir := path.Dir(basePath)
	if err := es.storageWriter.CreateDirectory(dir); err != nil {
		return fmt.Errorf("failed to create directory %s: %v", dir, err)
	}

	// 写入事件文件
	if err := es.storageWriter.WriteFile(basePath, reader); err != nil {
		return fmt.Errorf("failed to write node events file %s: %v", basePath, err)
	}

	logrus.Infof("Successfully flushed %d node events for hour %s to %s", len(events), hourKey, basePath)
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
		return fmt.Errorf("failed to marshal job events: %v", err)
	}

	reader := bytes.NewReader(data)

	// 构建作业事件存储路径
	basePath := path.Join(
		fmt.Sprintf("%s_%s", es.clusterName, es.clusterID),
		es.sessionName,
		"job_events",
		jobID,
		fmt.Sprintf("%s-%s", es.nodeID, hourKey))

	// 确保存储目录存在
	dir := path.Dir(basePath)
	if err := es.storageWriter.CreateDirectory(dir); err != nil {
		return fmt.Errorf("failed to create directory %s: %v", dir, err)
	}

	// 写入事件文件
	if err := es.storageWriter.WriteFile(basePath, reader); err != nil {
		return fmt.Errorf("failed to write job events file %s: %v", basePath, err)
	}

	logrus.Infof("Successfully flushed %d job events for job %s hour %s to %s", len(events), jobID, hourKey, basePath)
	return nil
}

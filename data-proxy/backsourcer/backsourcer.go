package backsourcer

import (
	"io"
	"net"
	"sync"
	"time"

	"data-proxy/aggregator" // 复用你现有的聚合器
	"data-proxy/util"
)

const (
	workerCount   = 18
	taskQueueSize = 4096
	dialTimeout   = 3 * time.Second
	ioTimeout     = 5 * time.Second
)

// BackSourceTask 从隧道解包出来的结构
type BackSourceTask struct {
	HopIP      [4]uint32
	Port       uint16
	UserID     uint32
	OriginAddr string // 必须是 ip:port
	ReqData    []byte
}

type BackSourcer struct {
	taskChan chan *BackSourceTask // 唯一入口管道
	wg       sync.WaitGroup
}

var GlobalBackSourcer *BackSourcer

func NewBackSourcer() *BackSourcer {
	bs := &BackSourcer{
		taskChan: make(chan *BackSourceTask, taskQueueSize),
	}
	bs.startWorkers()
	return bs
}

// Submit 提交任务到管道（外部只调用这个）
func (bs *BackSourcer) Submit(task *BackSourceTask) {
	bs.taskChan <- task
}

func (bs *BackSourcer) startWorkers() {
	for i := 0; i < workerCount; i++ {
		bs.wg.Add(1)
		go func() {
			defer bs.wg.Done()

			// 只从管道消费
			for task := range bs.taskChan {
				bs.doOriginRequest(task)
			}
		}()
	}
}

// 真正访问源站
func (bs *BackSourcer) doOriginRequest(task *BackSourceTask) {
	if task == nil {
		return
	}

	// 拨号源站
	conn, err := net.DialTimeout("tcp", task.OriginAddr, dialTimeout)
	if err != nil {
		return
	}
	defer conn.Close()

	// 写请求
	_ = conn.SetDeadline(time.Now().Add(ioTimeout))
	_, _ = conn.Write(task.ReqData)

	// 读响应
	resp, err := io.ReadAll(conn)
	if err != nil || len(resp) == 0 {
		return
	}

	// 返程：收集有效 hop（从后往前，跳过 0，跳过源站），翻转后逐跳发送
	var hops []net.IP
	for i := len(task.HopIP) - 1; i >= 0; i-- {
		if task.HopIP[i] == 0 {
			continue
		}
		ip := task.HopIP[i]
		hops = append(hops, util.Uint32ToIP(ip))
	}
	// 跳过源站（第一个，即翻转前的最后一个），此时顺序已是正确的返程顺序
	if len(hops) > 1 {
		hops = hops[1:]
	}

	// 组合返程路径为逗号分隔的 string，并构造 PathInfo
	routingKey := ""
	hopStrs := []string{}
	for _, hop := range hops {
		routingKey = routingKey + "," + hop.String()
		hopStrs = append(hopStrs, hop.String())
	}
	routingInfo := util.PathInfo{
		Hops: hopStrs,
	}

	nextHop := hops[1]

	aggregator.GlobalAggResponse.AddToBatch(
		routingKey,
		0, // port=0 表示返程
		routingInfo,
		nextHop,
		task.UserID,
		resp,
	)
}

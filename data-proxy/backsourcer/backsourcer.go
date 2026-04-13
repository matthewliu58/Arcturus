package backsourcer

import (
	"net"
	"sync"
	"time"

	"data-proxy/aggregator"
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

// OriginProtocol 源站访问协议接口（抽象）
type OriginProtocol interface {
	DoRequest(addr string, reqData []byte) ([]byte, error)
}

// BackSourcer 核心回源器（依赖协议接口）
type BackSourcer struct {
	taskChan chan *BackSourceTask
	wg       sync.WaitGroup
	protocol OriginProtocol
}

var GlobalBackSourcer *BackSourcer

// NewBackSourcer 默认使用 TCP 协议
func NewBackSourcer() *BackSourcer {
	return NewBackSourcerWithProtocol(NewTCPProtocol(dialTimeout, ioTimeout))
}

// NewBackSourcerWithProtocol 支持自定义协议
func NewBackSourcerWithProtocol(p OriginProtocol) *BackSourcer {
	bs := &BackSourcer{
		taskChan: make(chan *BackSourceTask, taskQueueSize),
		protocol: p,
	}
	bs.startWorkers()
	return bs
}

// Submit 提交任务
func (bs *BackSourcer) Submit(task *BackSourceTask) {
	bs.taskChan <- task
}

func (bs *BackSourcer) startWorkers() {
	for i := 0; i < workerCount; i++ {
		bs.wg.Add(1)
		go func() {
			defer bs.wg.Done()
			for task := range bs.taskChan {
				bs.doOriginRequest(task)
			}
		}()
	}
}

// 核心业务逻辑：协议无关
func (bs *BackSourcer) doOriginRequest(task *BackSourceTask) {
	if task == nil {
		return
	}

	// 通过协议接口请求
	resp, err := bs.protocol.DoRequest(task.OriginAddr, task.ReqData)
	if err != nil || len(resp) == 0 {
		return
	}

	// 处理返程 hop
	var hops []net.IP
	for i := len(task.HopIP) - 1; i >= 0; i-- {
		if task.HopIP[i] == 0 {
			continue
		}
		hops = append(hops, util.Uint32ToIP(task.HopIP[i]))
	}

	if len(hops) > 1 {
		hops = hops[1:]
	}

	hopStrs := make([]string, 0, len(hops))
	routingKey := ""
	for _, hop := range hops {
		routingKey += "," + hop.String()
		hopStrs = append(hopStrs, hop.String())
	}

	routingInfo := util.PathInfo{Hops: hopStrs}
	nextHop := hops[0] // 修正：hops[1] 会越界，这里应该是 hops[0]

	aggregator.GlobalAggResponse.AddToBatch(
		routingKey,
		0,
		routingInfo,
		nextHop,
		task.UserID,
		resp,
	)
}

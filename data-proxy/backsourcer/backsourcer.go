package backsourcer

import (
	"data-proxy/config"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"data-proxy/aggregator"
	"data-proxy/util"
)

const (
	backWorkerCount = 100
	taskQueueSize   = 100000
	dialTimeout     = 3 * time.Second
	ioTimeout       = 10 * time.Second

	poolMaxIdle     = 20
	poolMaxLifetime = 5 * time.Minute
	poolCleanPeriod = 1 * time.Minute
)

type BackSourceTask struct {
	HopIP      [4]uint32
	Port       uint16
	UserID     uint32
	OriginAddr string //ip:port
	ReqData    []byte
}

type OriginProtocol interface {
	DoRequest(addr string, reqData []byte) ([]byte, error)
}

type BackSourcer struct {
	taskChan chan *BackSourceTask
	wg       sync.WaitGroup
	protocol OriginProtocol
	tcpPool  *TCPConnPool
}

var (
	BackSourcerMap = make(map[string]*BackSourcer)
)

func NewBackSourcer(protocol string, pre string, l *slog.Logger) *BackSourcer {
	l.Info("NewBackSourcer", slog.String("pre", pre), slog.String("protocol", protocol))

	if protocol == "udp" {
		return NewBackSourcerWithProtocol(NewUDPProtocol(dialTimeout, ioTimeout), l)
	}

	//tcpPool := NewTCPConnPool(poolMaxIdle, poolMaxLifetime, poolCleanPeriod, l)
	//bs := &BackSourcer{
	//	taskChan: make(chan *BackSourceTask, taskQueueSize),
	//	protocol: NewTCPProtocolWithPool(tcpPool, dialTimeout, ioTimeout, l),
	//	tcpPool:  tcpPool,
	//}
	//bs.startWorkers(l)
	//return bs
	return NewBackSourcerWithProtocol(NewTCPProtocol(dialTimeout, ioTimeout), l)
}

func NewBackSourcerWithProtocol(p OriginProtocol, l *slog.Logger) *BackSourcer {
	bs := &BackSourcer{
		taskChan: make(chan *BackSourceTask, taskQueueSize),
		protocol: p,
	}
	bs.startWorkers(l)
	return bs
}

func (bs *BackSourcer) Submit(task *BackSourceTask) {
	bs.taskChan <- task
}

func (bs *BackSourcer) Close() {
	if bs.tcpPool != nil {
		bs.tcpPool.Close()
	}
	close(bs.taskChan)
	bs.wg.Wait()
}

func (bs *BackSourcer) startWorkers(l *slog.Logger) {

	backWorkerCount_ := config.Config_.BackWorkerCount
	if backWorkerCount_ <= 0 {
		backWorkerCount_ = backWorkerCount
	}

	for i := 0; i < backWorkerCount_; i++ {
		bs.wg.Add(1)
		go func() {
			defer bs.wg.Done()
			for task := range bs.taskChan {
				bs.doOriginRequest(task, l)
			}
		}()
	}
}

func (bs *BackSourcer) doOriginRequest(task *BackSourceTask, l *slog.Logger) {
	if task == nil {
		return
	}

	l.Info("doOriginRequest", slog.Any("UserID", task.UserID),
		slog.String("originAddr", task.OriginAddr), slog.Any("port", task.Port))

	l.Debug("doOriginRequest request content", slog.Any("UserID", task.UserID),
		slog.String("ReqData", string(task.ReqData)))

	resp, err := bs.protocol.DoRequest(task.OriginAddr, task.ReqData)
	if err != nil || len(resp) == 0 {
		l.Error("doOriginRequest failed", slog.Any("UserID", task.UserID),
			slog.Any("resp", len(resp)), slog.Any("err", err))
		return
	}

	l.Debug("doOriginRequest response content", slog.Any("UserID", task.UserID),
		slog.Any("resp", string(resp)))

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
	var routingKey string
	for _, hop := range hops {
		hopStrs = append(hopStrs, hop.String())
	}
	routingKey = strings.Join(hopStrs, ",")

	l.Info("doOriginRequest response", slog.Any("UserID", task.UserID),
		slog.String("originAddr", task.OriginAddr), slog.Any("response HopIP", hopStrs))

	routingInfo := util.PathInfo{Hops: hopStrs}
	nextHop := hops[1]

	aggregator.GlobalAggResponse.AddToBatch(
		false,
		routingKey,
		"",
		0,
		routingInfo,
		nextHop,
		task.UserID,
		resp,
	)
}

// task_dispatching/pusher.go
package task_dispatching

import (
	"context"
	"fmt"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"scheduling/controller/metrics_processing/metrics_storage"
	pb "scheduling/controller/metrics_processing/protocol"
	"scheduling/controller/metrics_processing/utils"
	"scheduling/goroutine_pool"
	"sync"
	"time"
)

type Pusher struct {
	connections map[string]*grpc.ClientConn
	mu          sync.RWMutex
}

func NewPusher() *Pusher {
	goroutine_pool.InitPool(goroutine_pool.ConfigPushPool, 100, pushTaskHandler)
	return &Pusher{
		connections: make(map[string]*grpc.ClientConn),
	}
}

func pushTaskHandler(payload interface{}) {
	params := payload.([]interface{})
	ip := params[0].(string)
	nodeList := params[1].(*pb.NodeList)
	tasks := params[2].([]*pb.ProbeTask)
	domainIPMappings := params[3].([]*pb.DomainIPMapping)
	pusher := params[4].(*Pusher)
	pusher.doPushToNode(ip, nodeList, tasks, domainIPMappings)
}

func (p *Pusher) PushToAllNodes(nodeList *pb.NodeList, fileManager *metrics_storage.FileManager) {
	domainIPMappings := fileManager.GetDomainIPMappings()
	for _, node := range nodeList.Nodes {
		ip := node.Ip
		log.Infof("PushToAllNodes, %s (%s) ", ip, node.Region)
		tasks := fileManager.GetNodeTasks(ip)
		configPool := goroutine_pool.GetPool(goroutine_pool.ConfigPushPool)
		if configPool != nil {
			err := configPool.Invoke([]interface{}{ip, nodeList, tasks, domainIPMappings, p})
			if err != nil {
				log.Errorf("Invoke failed, IP: %s, err: %v", ip, err)
			}
		} else {
			log.Infof("PushToAllNodes, ip=%s", ip)
			p.doPushToNode(ip, nodeList, tasks, domainIPMappings)
		}
	}
}

func (p *Pusher) doPushToNode(ip string, nodeList *pb.NodeList, tasks []*pb.ProbeTask, domainIPMappings []*pb.DomainIPMapping) {
	conn, err := p.getConnection(ip)
	if err != nil {
		log.Errorf("doPushToNode failed, ip=%s : err=%v", ip, err)
		return
	}
	client := pb.NewConfigServiceClient(conn)
	req := &pb.PushConfigRequest{
		NodeList:         nodeList,
		ProbeTasks:       tasks,
		DomainIpMappings: domainIPMappings,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := client.PushConfig(ctx, req)
	if err != nil {
		log.Errorf("PushConfig failed, ip=%s : err=%v", ip, err)
		return
	}
	log.Infof("doPushToNode, ip=%s : Status=%s", ip, resp.Status)
}

func (p *Pusher) getConnection(ip string) (*grpc.ClientConn, error) {
	p.mu.RLock()
	conn, ok := p.connections[ip]
	if ok && conn.GetState() != connectivity.Shutdown {
		p.mu.RUnlock()
		return conn, nil
	}
	p.mu.RUnlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if conn, ok = p.connections[ip]; ok && conn.GetState() != connectivity.Shutdown {
		return conn, nil
	}
	if conn != nil {
		delete(p.connections, ip)
	}
	addr := fmt.Sprintf("%s:50052", ip)
	newConn, err := utils.CreateGRPCConnection(addr, 3*time.Second)
	if err != nil {
		return nil, err
	}
	p.connections[ip] = newConn
	return newConn, nil
}

func (p *Pusher) Release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, conn := range p.connections {
		conn.Close()
	}
	p.connections = make(map[string]*grpc.ClientConn)
	goroutine_pool.ReleasePool(goroutine_pool.ConfigPushPool)
}

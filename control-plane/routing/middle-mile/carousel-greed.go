package middle_mile

import (
	"container/list"
	"control-plane/routing/graph"
	"control-plane/routing/routing"
	"fmt"
	"log/slog"
)

// HeuristicSolver 仿照 DijkstraSolver 风格
// 实现你给的伪代码：多路径+禁塞+频率+迭代优化
type HeuristicSolver struct {
	edges []*graph.Edge
	alpha float64 // 迭代次数系数
	beta  float64 // 删除路径比例
}

// NewHeuristicSolver 创建实例（和 Dijkstra 风格一致）
func NewHeuristicSolver(edges []*graph.Edge) *HeuristicSolver {
	return &HeuristicSolver{
		edges: edges,
		alpha: 1.5,
		beta:  0.2,
	}
}

// Computing 接口完全和 Dijkstra 一致！
// 返回：最优路径集合 [][]string
func (d *HeuristicSolver) Computing(start, end string, pre string, logger *slog.Logger) ([]routing.PathInfo, error) {
	// 1. 构建残差图
	resGraph := d.buildResidualGraph()

	// 2. 贪心添加所有可行路径 S
	S := list.New()
	for {
		path := d.findFeasiblePath(resGraph, start, end)
		if path == nil {
			break
		}
		S.PushBack(path)
		d.banPath(resGraph, path) // 占用边
	}

	if S.Len() == 0 {
		return nil, fmt.Errorf("no feasible path found from %s to %s", start, end)
	}

	// 3. P = S，记录所有历史路径
	P := list.New()
	for e := S.Front(); e != nil; e = e.Next() {
		P.PushBack(e.Value)
	}

	// 4. 初始化最优解 S*
	bestPaths := d.copyList(S)
	bestScore := d.objective(bestPaths)

	// 5. 删除最新的 beta*|S| 条
	removeNum := int(d.beta * float64(S.Len()))
	for i := 0; i < removeNum && S.Len() > 0; i++ {
		S.Remove(S.Back())
	}

	// 6. 迭代 alpha * |S| 次
	maxIter := int(d.alpha * float64(S.Len()))
	for i := 0; i < maxIter; i++ {
		if S.Len() == 0 {
			break
		}

		// 7. 移除最老路径
		oldestElm := S.Front()
		oldestPath := oldestElm.Value.([]string)
		S.Remove(oldestElm)

		// 8. 找到 P 中频率最高的路径
		freqPath := d.mostFrequentPath(P)

		// 9. 禁塞：高频路径所有边 + 最老路径第一条边
		d.banPathArcs(resGraph, freqPath)
		if len(oldestPath) >= 2 {
			d.banArc(resGraph, oldestPath[0], oldestPath[1])
		}

		// 10. 找新路径
		newPath := d.findFeasiblePath(resGraph, start, end)
		if newPath != nil {
			S.PushBack(newPath)
			P.PushBack(newPath)
		}

		// 11. 更新最优解
		currentScore := d.objective(S)
		if currentScore > bestScore {
			bestPaths = d.copyList(S)
			bestScore = currentScore
		}
	}

	// 把 list 转成 [][]string
	result := make([][]string, 0, bestPaths.Len())
	for e := bestPaths.Front(); e != nil; e = e.Next() {
		result = append(result, e.Value.([]string))
	}

	// 转换结果为 PathInfo
	pathInfos := make([]routing.PathInfo, 0, len(result))
	for _, path := range result {
		pathInfos = append(pathInfos, routing.PathInfo{
			Hops: path,
			Rtt:  0, // HeuristicSolver 不计算 Rtt
		})
	}
	logger.Info("HeuristicSolver path found", slog.String("pre", pre),
		slog.String("start", start), slog.String("end", end),
		slog.Int("pathCount", len(pathInfos)))
	return pathInfos, nil
}

// 构建残差图 map[from][to]bool
func (d *HeuristicSolver) buildResidualGraph() map[string]map[string]bool {
	g := make(map[string]map[string]bool)
	for _, e := range d.edges {
		if g[e.SourceIp] == nil {
			g[e.SourceIp] = make(map[string]bool)
		}
		g[e.SourceIp][e.DestinationIp] = true
	}
	return g
}

// 找一条可行路径（简单DFS）
func (d *HeuristicSolver) findFeasiblePath(g map[string]map[string]bool, s, t string) []string {
	visited := make(map[string]bool)
	path := []string{}
	var dfs func(string) bool
	dfs = func(u string) bool {
		if u == t {
			path = append(path, u)
			return true
		}
		visited[u] = true
		path = append(path, u)

		for v, available := range g[u] {
			if available && !visited[v] {
				if dfs(v) {
					return true
				}
			}
		}

		path = path[:len(path)-1]
		return false
	}

	if dfs(s) {
		return path
	}
	return nil
}

// 禁用整条路径（流量占用）
func (d *HeuristicSolver) banPath(g map[string]map[string]bool, path []string) {
	for i := 0; i < len(path)-1; i++ {
		from := path[i]
		to := path[i+1]
		if g[from] != nil {
			g[from][to] = false
		}
	}
}

// 永久禁塞边（伪代码里的 ban）
func (d *HeuristicSolver) banArc(g map[string]map[string]bool, from, to string) {
	if g[from] != nil {
		g[from][to] = false
	}
}

func (d *HeuristicSolver) banPathArcs(g map[string]map[string]bool, path []string) {
	for i := 0; i < len(path)-1; i++ {
		d.banArc(g, path[i], path[i+1])
	}
}

// 目标函数：路径数量（可改为总时延）
func (d *HeuristicSolver) objective(lst *list.List) int {
	return lst.Len()
}

// 复制路径列表
func (d *HeuristicSolver) copyList(l *list.List) *list.List {
	nl := list.New()
	for e := l.Front(); e != nil; e = e.Next() {
		nl.PushBack(e.Value)
	}
	return nl
}

// 统计路径频率，返回最高频路径
func (d *HeuristicSolver) mostFrequentPath(lst *list.List) []string {
	cnt := make(map[string]int)
	maxCnt := 0
	var bestPath []string

	for e := lst.Front(); e != nil; e = e.Next() {
		path := e.Value.([]string)
		key := ""
		for _, s := range path {
			key += s + "|"
		}
		cnt[key]++
		if cnt[key] > maxCnt {
			maxCnt = cnt[key]
			bestPath = path
		}
	}
	return bestPath
}

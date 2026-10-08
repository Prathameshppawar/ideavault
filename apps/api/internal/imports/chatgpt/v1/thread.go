package chatgptv1

import (
	"encoding/json"
	"sort"
)

// ActiveThread returns the active branch of a conversation tree in
// root→leaf order: it walks from currentNode up the parent pointers. When
// currentNode is missing or unknown it falls back to the longest root→leaf
// path (ties broken by the most recent message). Cycles are tolerated.
func ActiveThread(nodes map[string]Node, currentNode string) []Node {
	if len(nodes) == 0 {
		return nil
	}
	leaf := currentNode
	if _, ok := nodes[leaf]; !ok {
		leaf = deepestLeaf(nodes)
	}
	var path []Node
	seen := make(map[string]bool, len(nodes))
	for id := leaf; id != "" && !seen[id]; {
		n, ok := nodes[id]
		if !ok {
			break
		}
		seen[id] = true
		path = append(path, n)
		id = n.Parent
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// deepestLeaf returns the id of the node farthest from a root.
func deepestLeaf(nodes map[string]Node) string {
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids) // deterministic traversal
	var queue []string
	for _, id := range ids {
		p := nodes[id].Parent
		if _, ok := nodes[p]; p == "" || !ok || p == id {
			queue = append(queue, id)
		}
	}
	if len(queue) == 0 { // every node has a parent: a cycle. Pick any.
		queue = append(queue, ids[0])
	}
	depth := make(map[string]int, len(nodes))
	for _, id := range queue {
		depth[id] = 0
	}
	best := queue[0]
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		d := depth[id]
		if d > depth[best] || (d == depth[best] && newer(nodes[id], nodes[best])) {
			best = id
		}
		for _, child := range nodes[id].Children {
			if _, ok := nodes[child]; !ok {
				continue
			}
			if _, visited := depth[child]; visited {
				continue
			}
			depth[child] = d + 1
			queue = append(queue, child)
		}
	}
	return best
}

func newer(a, b Node) bool {
	if a.Message == nil || b.Message == nil {
		return a.Message != nil
	}
	return a.Message.CreateTime.After(b.Message.CreateTime.Time)
}

// decodeMapping decodes a raw mapping. Nodes whose message is malformed keep
// their tree links (so the thread is not cut) but lose the message; the
// number of such nodes is returned.
func decodeMapping(mapping map[string]json.RawMessage) (map[string]Node, int) {
	nodes := make(map[string]Node, len(mapping))
	bad := 0
	for id, raw := range mapping {
		n, err := DecodeNode(raw)
		if err != nil {
			bad++
		}
		n.ID = id
		nodes[id] = n
	}
	return nodes, bad
}

package chatgptv1

import (
	"reflect"
	"testing"
	"time"

	"github.com/Prathameshppawar/ideavault/apps/api/internal/imports"
)

func tree(edges map[string]string) map[string]Node {
	nodes := map[string]Node{}
	for id, parent := range edges {
		n := nodes[id]
		n.ID, n.Parent = id, parent
		nodes[id] = n
	}
	for id, parent := range edges {
		if p, ok := nodes[parent]; ok {
			p.Children = append(p.Children, id)
			nodes[parent] = p
		}
	}
	return nodes
}

func ids(nodes []Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.ID
	}
	return out
}

func TestActiveThread(t *testing.T) {
	//        root
	//         |
	//         a
	//       /   \
	//      b1    b2
	//      |
	//      c1
	edges := map[string]string{"root": "", "a": "root", "b1": "a", "b2": "a", "c1": "b1"}
	tests := []struct {
		name    string
		current string
		want    []string
	}{
		{"current selects short branch", "b2", []string{"root", "a", "b2"}},
		{"current selects long branch", "c1", []string{"root", "a", "b1", "c1"}},
		{"missing current falls back to longest path", "", []string{"root", "a", "b1", "c1"}},
		{"unknown current falls back", "zzz", []string{"root", "a", "b1", "c1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ids(ActiveThread(tree(edges), tt.current))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestActiveThreadTieBreaksOnNewestMessage(t *testing.T) {
	nodes := tree(map[string]string{"root": "", "x": "root", "y": "root"})
	older, newer := imports.Timestamp{Time: time.Unix(100, 0)}, imports.Timestamp{Time: time.Unix(200, 0)}
	x, y := nodes["x"], nodes["y"]
	x.Message = &Message{CreateTime: newer}
	y.Message = &Message{CreateTime: older}
	nodes["x"], nodes["y"] = x, y
	if got := ids(ActiveThread(nodes, "")); !reflect.DeepEqual(got, []string{"root", "x"}) {
		t.Fatalf("got %v", got)
	}
}

func TestActiveThreadSurvivesCycles(t *testing.T) {
	nodes := tree(map[string]string{"a": "b", "b": "a"})
	got := ActiveThread(nodes, "a")
	if len(got) != 2 {
		t.Fatalf("got %v", ids(got))
	}
	if ActiveThread(nil, "x") != nil {
		t.Fatal("nil mapping")
	}
	if got := ActiveThread(nodes, ""); len(got) == 0 || len(got) > 2 {
		t.Fatalf("fallback on cycle = %v", ids(got))
	}
}

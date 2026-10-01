package relationships

import (
	"fmt"
	"math"
	"strings"

	"github.com/zeusangis/dendrite/internal/storage"
)

// Importance combines graph connectivity with edge strength into a
// normalized 0..1 score. It is deterministic and cheap, so the frontend can
// map it straight to node radius.
//
//	score = 0.55*degree + 0.45*weighted strength (log-scaled)
func ComputeImportance(nodes []storage.Note, edges []storage.Edge, degrees map[int64]int) map[int64]float64 {
	scores := make(map[int64]float64, len(nodes))
	if len(nodes) == 0 {
		return scores
	}

	degree := make(map[int64]int, len(nodes))
	weighted := make(map[int64]float64, len(nodes))
	for _, e := range edges {
		degree[e.SourceID]++
		degree[e.TargetID]++
		w := math.Log1p(e.Strength)
		weighted[e.SourceID] += w
		weighted[e.TargetID] += w
	}
	// Each unordered relationship is counted once; explicit links already
	// contribute their larger strength. The legacy degrees argument is ignored.

	maxDeg, maxW := 1.0, 1.0
	for i := range nodes {
		id := nodes[i].ID
		if float64(degree[id]) > maxDeg {
			maxDeg = float64(degree[id])
		}
		if weighted[id] > maxW {
			maxW = weighted[id]
		}
	}

	for i := range nodes {
		node := &nodes[i]
		conn := float64(degree[node.ID]) / maxDeg
		strength := weighted[node.ID] / maxW
		scores[node.ID] = clamp01(0.55*conn + 0.45*strength)
	}
	return scores
}

// Describe renders a short human explanation of an edge for the inspector.
func Describe(e storage.Edge, sourceTitle, targetTitle string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s\nStrength: %.0f%% (%s)\n\nWhy:\n", sourceTitle, targetTitle, e.Strength/MaxScore*100, e.Source)
	for _, line := range strings.Split(e.Reason, "; ") {
		if line != "" {
			fmt.Fprintf(&b, "• %s\n", line)
		}
	}
	return b.String()
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

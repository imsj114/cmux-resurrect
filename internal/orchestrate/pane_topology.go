package orchestrate

import (
	"math"
	"sort"

	"github.com/drolosoft/cmux-resurrect/internal/client"
	"github.com/drolosoft/cmux-resurrect/internal/model"
)

const paneFrameTolerance = 3.0

type paneGeometry struct {
	pane model.Pane
	ref  string
	rect client.RectFrame
}

type paneLayoutNode struct {
	pane      *paneGeometry
	first     *paneLayoutNode
	second    *paneLayoutNode
	direction string
}

type paneCreateStep struct {
	index       int
	split       string
	focusTarget int
}

func (s *Saver) applyPaneTopology(ws *model.Workspace, workspaceRef string, treePanes []client.TreePane) bool {
	if len(ws.Panes) < 2 {
		return false
	}

	cmux, ok := s.Client.(client.CmuxRemoteBackend)
	if !ok {
		return false
	}
	resp, err := cmux.PaneListJSON(workspaceRef)
	if err != nil || resp == nil || len(resp.Panes) == 0 {
		return false
	}

	byRef := make(map[string]client.PaneRow, len(resp.Panes))
	byIndex := make(map[int]client.PaneRow, len(resp.Panes))
	for _, row := range resp.Panes {
		if !validFrame(row.PixelFrame) {
			continue
		}
		if row.Ref != "" {
			byRef[row.Ref] = row
		}
		byIndex[row.Index] = row
	}

	treeRefByIndex := make(map[int]string, len(treePanes))
	for _, pane := range treePanes {
		if pane.Ref != "" {
			treeRefByIndex[pane.Index] = pane.Ref
		}
	}

	geometries := make([]paneGeometry, 0, len(ws.Panes))
	for _, pane := range ws.Panes {
		row, ok := byRef[treeRefByIndex[pane.Index]]
		if !ok {
			row, ok = byIndex[pane.Index]
		}
		if !ok || !validFrame(row.PixelFrame) {
			return false
		}
		geometries = append(geometries, paneGeometry{
			pane: pane,
			ref:  row.Ref,
			rect: row.PixelFrame,
		})
	}

	root, ok := inferPaneLayout(geometries)
	if !ok {
		return false
	}
	steps := paneLayoutSteps(root)
	if len(steps) != len(ws.Panes) {
		return false
	}

	panesByIndex := make(map[int]model.Pane, len(ws.Panes))
	for _, pane := range ws.Panes {
		panesByIndex[pane.Index] = pane
	}

	reordered := make([]model.Pane, 0, len(ws.Panes))
	seen := make(map[int]bool, len(ws.Panes))
	for i, step := range steps {
		pane, ok := panesByIndex[step.index]
		if !ok || seen[step.index] {
			return false
		}
		seen[step.index] = true
		if i == 0 {
			pane.Split = ""
		} else {
			pane.Split = step.split
			pane.FocusTarget = step.focusTarget
		}
		reordered = append(reordered, pane)
	}
	ws.Panes = reordered
	return true
}

func validFrame(frame client.RectFrame) bool {
	return frame.Width > paneFrameTolerance && frame.Height > paneFrameTolerance
}

func inferPaneLayout(panes []paneGeometry) (*paneLayoutNode, bool) {
	items := append([]paneGeometry(nil), panes...)
	sort.Slice(items, func(i, j int) bool {
		if nearlyEqual(items[i].rect.Y, items[j].rect.Y) {
			if nearlyEqual(items[i].rect.X, items[j].rect.X) {
				return items[i].pane.Index < items[j].pane.Index
			}
			return items[i].rect.X < items[j].rect.X
		}
		return items[i].rect.Y < items[j].rect.Y
	})
	return inferPaneLayoutNode(items)
}

func inferPaneLayoutNode(panes []paneGeometry) (*paneLayoutNode, bool) {
	if len(panes) == 0 {
		return nil, false
	}
	if len(panes) == 1 {
		pane := panes[0]
		return &paneLayoutNode{pane: &pane}, true
	}

	if first, second, ok := partitionPanes(panes, "right"); ok {
		left, okLeft := inferPaneLayoutNode(first)
		right, okRight := inferPaneLayoutNode(second)
		if okLeft && okRight {
			return &paneLayoutNode{first: left, second: right, direction: "right"}, true
		}
	}
	if first, second, ok := partitionPanes(panes, "down"); ok {
		top, okTop := inferPaneLayoutNode(first)
		bottom, okBottom := inferPaneLayoutNode(second)
		if okTop && okBottom {
			return &paneLayoutNode{first: top, second: bottom, direction: "down"}, true
		}
	}
	return nil, false
}

func partitionPanes(panes []paneGeometry, direction string) ([]paneGeometry, []paneGeometry, bool) {
	candidates := splitCandidates(panes, direction)
	for _, boundary := range candidates {
		var first, second []paneGeometry
		for _, pane := range panes {
			if direction == "right" {
				if centerX(pane.rect) <= boundary {
					first = append(first, pane)
				} else {
					second = append(second, pane)
				}
			} else {
				if centerY(pane.rect) <= boundary {
					first = append(first, pane)
				} else {
					second = append(second, pane)
				}
			}
		}
		if len(first) == 0 || len(second) == 0 {
			continue
		}
		if validPanePartition(first, second, direction) {
			return first, second, true
		}
	}
	return nil, nil, false
}

func splitCandidates(panes []paneGeometry, direction string) []float64 {
	values := make([]float64, 0, len(panes))
	seen := make(map[int64]bool, len(panes))
	for _, pane := range panes {
		var value float64
		if direction == "right" {
			value = pane.rect.X + pane.rect.Width
		} else {
			value = pane.rect.Y + pane.rect.Height
		}
		key := int64(math.Round(value / paneFrameTolerance))
		if !seen[key] {
			seen[key] = true
			values = append(values, value)
		}
	}
	sort.Float64s(values)
	return values
}

func validPanePartition(first, second []paneGeometry, direction string) bool {
	firstBounds := paneBounds(first)
	secondBounds := paneBounds(second)
	if direction == "right" {
		return firstBounds.X+firstBounds.Width <= secondBounds.X+paneFrameTolerance
	}
	return firstBounds.Y+firstBounds.Height <= secondBounds.Y+paneFrameTolerance
}

func paneBounds(panes []paneGeometry) client.RectFrame {
	minX := math.Inf(1)
	minY := math.Inf(1)
	maxX := math.Inf(-1)
	maxY := math.Inf(-1)
	for _, pane := range panes {
		minX = math.Min(minX, pane.rect.X)
		minY = math.Min(minY, pane.rect.Y)
		maxX = math.Max(maxX, pane.rect.X+pane.rect.Width)
		maxY = math.Max(maxY, pane.rect.Y+pane.rect.Height)
	}
	return client.RectFrame{
		X:      minX,
		Y:      minY,
		Width:  maxX - minX,
		Height: maxY - minY,
	}
}

func paneLayoutSteps(root *paneLayoutNode) []paneCreateStep {
	if root == nil {
		return nil
	}
	first := representativePane(root)
	if first == nil {
		return nil
	}
	steps := []paneCreateStep{{index: first.pane.Index}}
	appendPaneLayoutSteps(root, &steps)
	return steps
}

func appendPaneLayoutSteps(node *paneLayoutNode, steps *[]paneCreateStep) {
	if node == nil || node.pane != nil {
		return
	}
	target := representativePane(node.first)
	created := representativePane(node.second)
	if target == nil || created == nil {
		return
	}
	*steps = append(*steps, paneCreateStep{
		index:       created.pane.Index,
		split:       node.direction,
		focusTarget: target.pane.Index,
	})
	appendPaneLayoutSteps(node.first, steps)
	appendPaneLayoutSteps(node.second, steps)
}

func representativePane(node *paneLayoutNode) *paneGeometry {
	if node == nil {
		return nil
	}
	if node.pane != nil {
		return node.pane
	}
	return representativePane(node.first)
}

func centerX(frame client.RectFrame) float64 {
	return frame.X + frame.Width/2
}

func centerY(frame client.RectFrame) float64 {
	return frame.Y + frame.Height/2
}

func nearlyEqual(a, b float64) bool {
	return math.Abs(a-b) <= paneFrameTolerance
}

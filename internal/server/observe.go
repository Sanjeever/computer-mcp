package server

import "github.com/Sanjeever/computer-mcp/internal/desktop"

type SnapshotDiff struct {
	Snapshot
	BaseID   string   `json:"base_snapshot_id"`
	Removed  []string `json:"removed_element_ids"`
	Order    []string `json:"element_order"`
	Complete bool     `json:"diff_complete"`
}

func sameScope(a, b desktop.ObserveOptions) bool {
	if (a.Region == nil) != (b.Region == nil) {
		return false
	}
	if a.Region != nil && *a.Region != *b.Region {
		return false
	}
	a.Region, b.Region = nil, nil
	return a == b
}

func diffSnapshots(old, current *Snapshot) SnapshotDiff {
	d := SnapshotDiff{Snapshot: *current, BaseID: old.ID, Removed: []string{}, Order: []string{}, Complete: !old.Truncated && !current.Truncated}
	d.Elements = []desktop.Element{}
	previous := make(map[string]desktop.Element, len(old.Elements))
	for _, e := range old.Elements {
		previous[e.ID] = e
	}
	for _, e := range current.Elements {
		d.Order = append(d.Order, e.ID)
		if p, ok := previous[e.ID]; !ok || p != e {
			d.Elements = append(d.Elements, e)
		}
		delete(previous, e.ID)
	}
	for _, e := range old.Elements {
		if _, ok := previous[e.ID]; ok {
			d.Removed = append(d.Removed, e.ID)
		}
	}
	return d
}

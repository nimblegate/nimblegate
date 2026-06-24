// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"sort"
	"strings"

	"nimblegate/internal/buckets"
	v2 "nimblegate/internal/config/v2"
	"nimblegate/internal/frames"
)

// TreeNode is one node in the dashboard's hierarchical v2 selection view
// (spec §7.5.2). The hierarchy is:
//
//	axis  → bucket → frame
//	core  → frame   (no intermediate; core/<frame>)
//	frame → lang   → sub-bucket → frame  (framework/<lang>/<concept>/<frame>)
//	plat  → vendor → sub-bucket → frame  (platform/<vendor>/<concept>/<frame>)
//	domain → concept → frame             (domains/<concept>/<frame>)
//
// Each node knows its label, state (active / partial / excluded / inactive),
// total/active frame counts, and children. Leaf nodes (frames) have no
// children and report their own activation.
type TreeNode struct {
	Label      string
	State      NodeState
	Total      int        // total frames at-or-under this node
	Active     int        // active frames at-or-under this node
	MissingIDs []string   // v1 frame IDs at-or-under this node that are inactive
	Children   []TreeNode // empty for leaf (frame) nodes
}

// NodeState mirrors PackState for tree-node display purposes. The dashboard
// renders different glyphs/colors per state per spec §7.5.2.
type NodeState int

const (
	// NodeInactive - the axis selection doesn't include this branch
	// (e.g., a framework that isn't selected, or a domain not in the
	// multi-select list). Whole subtree dark/grayed in UI.
	NodeInactive NodeState = iota
	// NodeFullyActive - every frame in the subtree is enabled
	NodeFullyActive
	// NodePartial - some frames enabled, some excluded (per-frame override
	// stripped or sub-bucket excluded). Informational badge.
	NodePartial
	// NodePartialWarn - >50%% of the subtree's frames stripped. Per spec
	// §7.5.3 the operator probably should have excluded the whole pack
	// instead. Escalated UI severity.
	NodePartialWarn
	// NodeExcluded - every frame at-or-under the node is inactive (either
	// the whole sub-bucket is excluded or every individual frame is
	// stripped via override).
	NodeExcluded
)

// String returns a stable label suitable for audit logs / debugging.
func (n NodeState) String() string {
	switch n {
	case NodeInactive:
		return "inactive"
	case NodeFullyActive:
		return "active"
	case NodePartial:
		return "partial"
	case NodePartialWarn:
		return "partial-warn"
	case NodeExcluded:
		return "excluded"
	}
	return "unknown"
}

// frameInfo bundles a stdlib frame's resolution info for tree building.
// Package-private since it's an implementation detail of BuildTree's helpers.
type frameInfo struct {
	v1ID   string
	bucket buckets.Bucket
	active bool
}

// BuildTree produces the hierarchical view of an operator's v2 selection.
// Walks the v2 frame map once, groups frames by axis/bucket, and computes
// per-node state by aggregating leaf activations.
func (m *V2FrameMap) BuildTree(cfg *v2.Config, stdlibFrames []frames.Frame) []TreeNode {
	if cfg == nil || m == nil {
		return nil
	}
	sel := cfg.Selection()
	// axisRoot → "lang or vendor or concept or empty" → sub-bucket → frames
	var coreFrames []frameInfo
	frameworkAxis := map[string]map[string][]frameInfo{} // lang → sub-bucket → frames
	platformAxis := map[string]map[string][]frameInfo{}  // vendor → sub-bucket → frames
	domainAxis := map[string][]frameInfo{}               // concept → frames

	for _, f := range stdlibFrames {
		bucket, ok := m.IDToBucket[f.ID()]
		if !ok {
			continue
		}
		fi := frameInfo{v1ID: f.ID(), bucket: bucket}
		// Use a "leaf bucket" copy with this frame's FrameID for the activation
		// check (per-frame overrides key off the bucket's FrameID).
		leafBucket := bucket
		fi.active = sel.IsFrameActive(leafBucket)

		switch bucket.Axis {
		case buckets.AxisCore:
			coreFrames = append(coreFrames, fi)
		case buckets.AxisFramework:
			subKey := bucket.SubBucket
			if frameworkAxis[bucket.Lang] == nil {
				frameworkAxis[bucket.Lang] = map[string][]frameInfo{}
			}
			frameworkAxis[bucket.Lang][subKey] = append(frameworkAxis[bucket.Lang][subKey], fi)
		case buckets.AxisPlatform:
			if platformAxis[bucket.Vendor] == nil {
				platformAxis[bucket.Vendor] = map[string][]frameInfo{}
			}
			platformAxis[bucket.Vendor][bucket.SubBucket] = append(platformAxis[bucket.Vendor][bucket.SubBucket], fi)
		case buckets.AxisDomain:
			domainAxis[bucket.Concept] = append(domainAxis[bucket.Concept], fi)
		}
	}

	var roots []TreeNode

	// core root
	if len(coreFrames) > 0 {
		roots = append(roots, buildBucketLeafNode("core", coreFrames))
	}

	// framework root - only show selected framework + its sub-buckets
	if cfg.Framework.Selected != "" {
		if subBuckets, ok := frameworkAxis[cfg.Framework.Selected]; ok {
			roots = append(roots, buildFrameworkRoot(cfg.Framework.Selected, subBuckets))
		}
	}

	// platform root - only show selected vendor + its sub-buckets, marking excluded subs
	if cfg.Platform.Selected != "" {
		if subBuckets, ok := platformAxis[cfg.Platform.Selected]; ok {
			roots = append(roots, buildPlatformRoot(cfg, cfg.Platform.Selected, subBuckets))
		}
	}

	// domains root - show selected domains (inactive ones still listed for transparency)
	if len(domainAxis) > 0 {
		roots = append(roots, buildDomainsRoot(cfg, domainAxis))
	}

	return roots
}

// buildBucketLeafNode builds the core node - a flat bucket with frames as leaves.
func buildBucketLeafNode(label string, fis []frameInfo) TreeNode {
	root := TreeNode{Label: label}
	for _, fi := range fis {
		leaf := TreeNode{
			Label: fi.v1ID,
			Total: 1,
		}
		if fi.active {
			leaf.State = NodeFullyActive
			leaf.Active = 1
		} else {
			leaf.State = NodeExcluded
			leaf.MissingIDs = []string{fi.v1ID}
		}
		root.Children = append(root.Children, leaf)
		root.Total++
		if fi.active {
			root.Active++
		} else {
			root.MissingIDs = append(root.MissingIDs, fi.v1ID)
		}
	}
	sort.Slice(root.Children, func(i, j int) bool { return root.Children[i].Label < root.Children[j].Label })
	root.State = aggregateState(root.Active, root.Total)
	return root
}

// buildFrameworkRoot builds the framework axis node for the selected language
// (e.g., "html") with optional sub-concept children (e.g., svelte-security).
func buildFrameworkRoot(lang string, subBuckets map[string][]frameInfo) TreeNode {
	root := TreeNode{Label: "framework/" + lang}
	subKeys := make([]string, 0, len(subBuckets))
	for k := range subBuckets {
		subKeys = append(subKeys, k)
	}
	sort.Strings(subKeys)
	for _, subKey := range subKeys {
		fis := subBuckets[subKey]
		var subLabel string
		if subKey == "" {
			subLabel = "framework/" + lang // flat - no sub-concept
		} else {
			subLabel = "framework/" + lang + "/" + subKey
		}
		subNode := buildBucketLeafNode(subLabel, fis)
		root.Children = append(root.Children, subNode)
		root.Total += subNode.Total
		root.Active += subNode.Active
		root.MissingIDs = append(root.MissingIDs, subNode.MissingIDs...)
	}
	root.State = aggregateState(root.Active, root.Total)
	return root
}

// buildPlatformRoot builds the platform axis node for the selected vendor,
// honoring [platform.<vendor>] exclude list so excluded sub-buckets show as
// NodeExcluded (not just hidden).
func buildPlatformRoot(cfg *v2.Config, vendor string, subBuckets map[string][]frameInfo) TreeNode {
	root := TreeNode{Label: "platform/" + vendor}
	excludeSet := make(map[string]bool)
	for _, e := range cfg.PlatformOverrides[vendor].Exclude {
		excludeSet[e] = true
	}

	subKeys := make([]string, 0, len(subBuckets))
	for k := range subBuckets {
		subKeys = append(subKeys, k)
	}
	sort.Strings(subKeys)

	for _, subKey := range subKeys {
		fis := subBuckets[subKey]
		label := "platform/" + vendor + "/" + subKey
		var subNode TreeNode
		if excludeSet[subKey] {
			// Excluded sub-bucket: all frames inactive.
			subNode = TreeNode{
				Label: label,
				State: NodeExcluded,
				Total: len(fis),
			}
			for _, fi := range fis {
				subNode.MissingIDs = append(subNode.MissingIDs, fi.v1ID)
				subNode.Children = append(subNode.Children, TreeNode{
					Label:      fi.v1ID,
					State:      NodeExcluded,
					Total:      1,
					MissingIDs: []string{fi.v1ID},
				})
			}
		} else {
			subNode = buildBucketLeafNode(label, fis)
		}
		root.Children = append(root.Children, subNode)
		root.Total += subNode.Total
		root.Active += subNode.Active
		root.MissingIDs = append(root.MissingIDs, subNode.MissingIDs...)
	}
	root.State = aggregateState(root.Active, root.Total)
	return root
}

// buildDomainsRoot builds the domains axis node. Selected domains show their
// frames; unselected domains appear inactive (helps operators see what's
// available without picking).
func buildDomainsRoot(cfg *v2.Config, domainAxis map[string][]frameInfo) TreeNode {
	root := TreeNode{Label: "domains"}
	selected := make(map[string]bool)
	for _, d := range cfg.Domains.Selected {
		selected[d] = true
	}
	concepts := make([]string, 0, len(domainAxis))
	for k := range domainAxis {
		concepts = append(concepts, k)
	}
	sort.Strings(concepts)
	for _, concept := range concepts {
		fis := domainAxis[concept]
		label := "domains/" + concept
		if !selected[concept] {
			// Inactive domain - all frames inactive but listed for visibility.
			node := TreeNode{
				Label: label,
				State: NodeInactive,
				Total: len(fis),
			}
			for _, fi := range fis {
				node.MissingIDs = append(node.MissingIDs, fi.v1ID)
				node.Children = append(node.Children, TreeNode{
					Label: fi.v1ID,
					State: NodeInactive,
					Total: 1,
				})
			}
			root.Children = append(root.Children, node)
			continue
		}
		subNode := buildBucketLeafNode(label, fis)
		root.Children = append(root.Children, subNode)
		root.Total += subNode.Total
		root.Active += subNode.Active
		root.MissingIDs = append(root.MissingIDs, subNode.MissingIDs...)
	}
	root.State = aggregateState(root.Active, root.Total)
	return root
}

// aggregateState computes a node's state from its active/total counts.
// Mirrors the PackState thresholds from spec §4.5: >0 missing → partial;
// >50%% missing → partial-warn; none active → excluded; all active → active.
func aggregateState(active, total int) NodeState {
	if total == 0 {
		return NodeInactive
	}
	if active == 0 {
		return NodeExcluded
	}
	if active == total {
		return NodeFullyActive
	}
	if active*2 < total {
		return NodePartialWarn
	}
	return NodePartial
}

// FlatBucketPath returns the path-only form (no frame ID) of a leaf-bucket node's
// label, mainly for audit-log output where the leaf node's parent path is
// what matters.
func FlatBucketPath(label string) string {
	// label is already path-only for non-leaf nodes; this is a stable helper
	// for callers that want to strip a trailing /<frame> from a leaf label.
	idx := strings.LastIndex(label, "/")
	if idx <= 0 {
		return label
	}
	return label[:idx]
}

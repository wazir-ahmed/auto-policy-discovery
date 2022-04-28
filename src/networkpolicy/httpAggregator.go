package networkpolicy

import (
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/accuknox/auto-policy-discovery/src/libs"
	types "github.com/accuknox/auto-policy-discovery/src/types"
)

var WildPathDigit string = "/[0-9]+"
var WildPathDigitLeaf string = "/[0-9^/]+"
var WildPathChar string = "/.+"
var WildPathCharLeaf string = "/.[^/]+"
var WildPaths []string

var MergedSrcPerMergedDstForHTTP map[string][]*HTTPDst

func init() {
	WildPaths = []string{WildPathDigit, WildPathChar}
	MergedSrcPerMergedDstForHTTP = map[string][]*HTTPDst{}
}

// ====================== //
// == HTTP aggregation == //
// ====================== //

var httpMethods = []string{
	http.MethodGet,
	http.MethodHead,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodConnect,
	http.MethodOptions,
	http.MethodTrace,
}

func CheckHTTPMethod(method string) bool {
	for _, m := range httpMethods {
		if strings.Contains(method, m) {
			return true
		}
	}

	return false
}

// ================== //
// == Get/Set Tree == //
// ================== //

func getHTTPTree(targetSrc string, targetDst MergedPortDst) map[string]map[string]*Node {
	if httpDsts, ok := MergedSrcPerMergedDstForHTTP[targetSrc]; ok {
		for _, httpDst := range httpDsts {
			if targetDst.Namespace == httpDst.Namespace &&
				(targetDst.MatchLabels == httpDst.MatchLabels ||
					targetDst.Additionals[0] == httpDst.Additional) {
				toPortInclude := true

				for _, targetToPort := range targetDst.ToPorts {
					if !libs.ContainsElement(httpDst.ToPorts, targetToPort) {
						toPortInclude = false
						break
					}
				}

				if toPortInclude {
					return httpDst.HTTPTree
				}
			}
		}
	}

	return nil
}

func setHTTPTree(targetSrc string, targetDst MergedPortDst, tree map[string]map[string]*Node) {
	if httpDsts, ok := MergedSrcPerMergedDstForHTTP[targetSrc]; ok {
		for i, httpDst := range httpDsts {
			if targetDst.Namespace == httpDst.Namespace &&
				(targetDst.MatchLabels == httpDst.MatchLabels ||
					targetDst.Additionals[0] == httpDst.Additional) {
				toPortInclude := true

				for _, targetToPort := range targetDst.ToPorts {
					if !libs.ContainsElement(httpDst.ToPorts, targetToPort) {
						toPortInclude = false
						break
					}
				}

				if toPortInclude {
					httpDsts[i].HTTPTree = tree
				}
			}
		}

		MergedSrcPerMergedDstForHTTP[targetSrc] = httpDsts
	} else {

		var labels, dstInfo string
		if strings.Contains(targetDst.Namespace, "reserved") {
			dstInfo = targetDst.Additionals[0]
		} else {
			labels = targetDst.MatchLabels
		}

		httpDst := HTTPDst{
			Namespace:   targetDst.Namespace,
			MatchLabels: labels,
			Additional:  dstInfo,
			ToPorts:     []types.SpecPort{},
			HTTPTree:    tree,
		}

		httpDst.ToPorts = append(httpDst.ToPorts, targetDst.ToPorts...)

		MergedSrcPerMergedDstForHTTP[targetSrc] = []*HTTPDst{&httpDst}
	}
}

// ============================ //
// == PathNode and functions == //
// ============================ //

type Node struct {
	path string

	depth      int
	touchCount int
	childNodes []*Node
}

type MergedNode struct {
	path string

	depth      int
	touchCount int
}

type HTTPDst struct {
	Namespace   string
	MatchLabels string
	Additional  string
	ToPorts     []types.SpecPort
	HTTPTree    map[string]map[string]*Node
}

func (n *Node) getChildNodesCount() int {
	results := 0

	for _, childNode := range n.childNodes {
		results = results + childNode.touchCount
	}

	return results
}

func (n *Node) generatePaths(results map[string]bool, parentPath string) {
	for _, childNode := range n.childNodes {
		childNode.generatePaths(results, parentPath+n.path)
	}

	// leaf node
	if n.getChildNodesCount() == 0 {
		if libs.ContainsElement(WildPaths, n.path) {
			if n.path == WildPathDigit {
				results[parentPath+WildPathDigitLeaf] = true
			} else {
				results[parentPath+WildPathCharLeaf] = true
			}
		} else {
			results[parentPath+n.path] = true
		}
	} else if n.touchCount > n.getChildNodesCount() {
		// intermediate node
		results[parentPath+n.path] = true
	}
}

func (n *Node) insert(paths []string) {
	for _, path := range paths {
		child := n.findChildNode(path, n.depth+1)

		if child == nil {
			newChild := &Node{
				depth:      n.depth + 1,
				path:       path,
				touchCount: 1,
				childNodes: []*Node{},
			}

			n.childNodes = append(n.childNodes, newChild)
			newChild.insert(paths[1:])
		} else {
			child.touchCount++
			child.insert(paths[1:])
		}

		break
	}
}

func (n *Node) aggregateChildNodes() {
	// depth first iterate
	for _, childNode := range n.childNodes {
		childNode.aggregateChildNodes()
	}

	// #child nodes > threshold
	if len(n.childNodes) > HTTPThreshold {
		childPaths := []string{}
		for _, childNode := range n.childNodes {
			childPaths = append(childPaths, childNode.path)
		}

		// replace with wild card path
		wildPath := ""
		if checkDigitsOnly(childPaths) {
			wildPath = WildPathDigit
		} else {
			wildPath = WildPathChar
		}

		tempChild := &Node{
			depth:      n.depth + 1,
			path:       wildPath,
			childNodes: []*Node{},
		}

		//   a     --->   a
		// b   c        [temp]
		// d   e         d  e
		for _, childNode := range n.childNodes {
			tempChild.touchCount = tempChild.touchCount + childNode.touchCount

			// child node's child node --> grand child
			tempChild.childNodes = append(tempChild.childNodes, childNode.childNodes...)
		}

		// after aggregating child nodes, check same child nodes,
		tempChild.mergeSameChildNodes()

		n.childNodes = []*Node{tempChild}
	}
}

func (n *Node) findChildNode(path string, depth int) *Node {
	for _, child := range n.childNodes {
		// case 1: regex matching
		if libs.ContainsElement(WildPaths, child.path) && child.depth == depth {
			r, _ := regexp.Compile(child.path)
			if r.FindString(path) == path {
				return child
			}
			// case 2: exact matching
		} else if child.path == path && child.depth == depth {
			return child
		}
	}

	return nil
}

func (n *Node) mergeSameChildNodes() {
	if len(n.childNodes) == 0 {
		return
	}

	nodeMap := map[MergedNode][]*Node{}
	nodeMapTouchCount := map[MergedNode]int{}

	merged := false

	for _, childNode := range n.childNodes {
		temp := MergedNode{
			path:  childNode.path,
			depth: childNode.depth,
		}

		// check existing same child nodes
		if exist, ok := nodeMap[temp]; ok {
			exist = append(exist, childNode.childNodes...)
			nodeMap[temp] = exist
			merged = true
		} else {
			nodeMap[temp] = childNode.childNodes
		}

		// merge touch count
		nodeMapTouchCount[temp] = nodeMapTouchCount[temp] + childNode.touchCount
	}

	// if not merged, return
	if !merged {
		return
	}

	n.childNodes = []*Node{}

	for uniqueChildNodes, grandChildNodes := range nodeMap {
		newChildNode := &Node{
			depth:      uniqueChildNodes.depth,
			path:       uniqueChildNodes.path,
			touchCount: nodeMapTouchCount[uniqueChildNodes],
			childNodes: grandChildNodes,
		}

		n.childNodes = append(n.childNodes, newChildNode)
	}
}

// =================== //
// == Tree Handling == //
// =================== //

func checkSamePathLength(paths []string) bool {
	pathLength := map[int]bool{}

	for _, path := range paths {
		pathLength[len(path)] = true
	}

	return len(pathLength) <= 1
}

func checkDigitsOnly(paths []string) bool {
	isDigit := true

	for _, path := range paths {
		woSlash := strings.Split(path, "/")[1]
		if _, err := strconv.Atoi(woSlash); err != nil {
			isDigit = false
		}
	}

	return isDigit
}

// ===================== //
// == Build Path Tree == //
// ===================== //

func buildPathTree(treeMap map[string]*Node, paths []string) {
	pattern, _ := regexp.Compile("(/.[^/]*)")

	// sorting paths
	sort.Strings(paths)

	// iterate paths
	for _, path := range paths {
		if path == "/" { // rootpath
			continue
		}

		// example: /usr/lib/python2.7/UserDict.py
		// 			--> '/usr', '/lib', '/python2.7', '/UserDict.py'
		//			in this case, '/usr' is rootNode
		tokenizedPaths := pattern.FindAllString(path, -1)
		rootPath := tokenizedPaths[0]

		if rootNode, ok := treeMap[rootPath]; !ok {
			newRoot := &Node{
				depth:      0,
				path:       rootPath,
				touchCount: 1,
				childNodes: []*Node{},
			}

			newRoot.insert(tokenizedPaths[1:])
			treeMap[rootPath] = newRoot
		} else {
			rootNode.touchCount++
			rootNode.insert(tokenizedPaths[1:])
		}
	}
}

// ========================== //
// == Aggreagtion function == //
// ========================== //

func AggregatePaths(treeMap map[string]*Node, paths []string) []string {
	// build path tree
	buildPathTree(treeMap, paths)

	// aggregate path
	for _, root := range treeMap {
		root.aggregateChildNodes()
	}

	// generate path
	aggregatedPaths := map[string]bool{}
	for _, root := range treeMap {
		root.generatePaths(aggregatedPaths, "")
	}

	results := []string{}
	for path := range aggregatedPaths {
		results = append(results, path)
	}

	// check root path '/'
	for _, path := range paths {
		if path == "/" {
			results = append(results, path)
		}
	}

	return results
}

func AggregateHTTPRule(aggregatedSrcPerAggregatedDst map[string][]MergedPortDst) {
	for aggregatedSrc, dsts := range aggregatedSrcPerAggregatedDst {
		for i, dst := range dsts {
			// check if dst has HTTP rules
			if len(dst.ToHTTPs) == 0 {
				continue
			}

			// httpTree = key: METHOD - val: Tree
			httpTree := getHTTPTree(aggregatedSrc, dst)
			if httpTree == nil {
				httpTree = map[string]map[string]*Node{}
			}

			updatedHTTPs := []types.SpecHTTP{}

			methodToPaths := map[string][]string{}

			for _, http := range dst.ToHTTPs {
				method := http.Method
				path := http.Path

				if val, ok := methodToPaths[method]; ok {
					if !libs.ContainsElement(val, path) {
						val = append(val, path)
					}
					methodToPaths[method] = val
				} else {
					methodToPaths[method] = []string{path}
				}
			}

			for method, paths := range methodToPaths {
				httpPathTree := map[string]*Node{}
				if existed, ok := httpTree[method]; ok {
					httpPathTree = existed
				}

				aggregatedPaths := AggregatePaths(httpPathTree, paths)
				for _, aggPath := range aggregatedPaths {
					aggHTTP := types.SpecHTTP{Method: method, Path: aggPath}
					updatedHTTPs = append(updatedHTTPs, aggHTTP)
				}

				httpTree[method] = httpPathTree
			}

			dsts[i].ToHTTPs = updatedHTTPs

			setHTTPTree(aggregatedSrc, dst, httpTree)
		}

		aggregatedSrcPerAggregatedDst[aggregatedSrc] = dsts
	}
}

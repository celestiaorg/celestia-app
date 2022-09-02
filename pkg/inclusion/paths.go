package inclusion

// genSubTreeRootPath calculates the path to a given subtree root of a node, given the
// depth and position of the node. note: the root of the tree is depth 0.
// The following nolint can be removed after this function is used.
//nolint:unused,deadcode
func genSubTreeRootPath(depth int, pos uint) []bool {
	path := make([]bool, depth)
	counter := 0
	for i := depth - 1; i >= 0; i-- {
		if (pos & (1 << i)) == 0 {
			path[counter] = false
		} else {
			path[counter] = true
		}
		counter++
	}
	return path
}

// coord identifies a tree node using the depth and position
// Depth       Position
// 0              0
//               / \
//              /   \
// 1           0     1
//            /\     /\
// 2         0  1   2  3
//          /\  /\ /\  /\
// 3       0 1 2 3 4 5 6 7
type coord struct {
	// depth is the typical depth of a tree, 0 being the root
	depth uint64
	// position is the index of a node of a given depth, 0 being the left most
	// node
	position uint64
}

// climb is a state transition function to simulate climbing a balanced binary
// tree, using the current node as input and the next highest node as output.
func (c coord) climb() coord {
	return coord{
		depth:    c.depth - 1,
		position: c.position / 2,
	}
}

// canClimbRight uses the current position to calculate the direction of the next
// climb. Returns true if the next climb is right (if the position (index) is
// even). please see depth and position example map in docs for coord.
func (c coord) canClimbRight() bool {
	return c.position%2 == 0 && c.depth > 0
}

// calculateSubTreeRootCoordinates generates the sub tree root coordinates of a
// set of shares for a balanced binary tree of a given depth. It assumes that
// end does not exceed the range of a tree of the provided depth, and that end
// >= start. This function works by starting at the first index of the msg and
// working our way right.
func calculateSubTreeRootCoordinates(maxDepth, start, end uint64) []coord {
	cds := []coord{}
	// leafCursor keeps track of the current leaf that we are starting with when
	// finding the subtree root for some set. When leafCursor == end, we are
	// finished calculating sub tree roots
	leafCursor := start
	// nodeCursor keeps track of the current tree node when finding sub
	// tree roots
	nodeCursor := coord{
		depth:    maxDepth,
		position: start,
	}
	// lastNodeCursor keeps track of the last node cursor so that when we climb
	// too high, we can use this node as a sub tree root
	lastNodeCursor := nodeCursor
	lastLeafCursor := leafCursor
	// nodeRangeCursor keeps track of the number of leaves that are under the
	// current tree node. We could calculate this each time, but this acts as a
	// cache
	nodeRangeCursor := uint64(1)
	// reset is used to reset the above state after finding a subtree root. We
	// reset by setting the node cursors to the values equal to the next leaf
	// node.
	reset := func() {
		lastNodeCursor = nodeCursor
		lastLeafCursor = leafCursor
		nodeCursor = coord{
			depth:    maxDepth,
			position: leafCursor,
		}
		nodeRangeCursor = uint64(1)
	}
	// recursively climb the tree starting at the left most leaf node (the
	// starting leaf), and save each subtree root as we find it. After finding a
	// subtree root, if there's still leaves left in the message, then restart
	// the process from that leaf.
	for {
		switch {
		// check if we're finished, if so add the last coord and return
		case leafCursor == end:
			cds = append(cds, nodeCursor)
			return cds
		// check if we've climbed too high in the tree. if so, add the last
		// highest node and proceed.
		case leafCursor > end:
			cds = append(cds, lastNodeCursor)
			leafCursor = lastLeafCursor + 1
			reset()
		// check if can climb right again (only even positions will climb
		// right). If not, we want to record this coord as it is a subtree
		// root, then adjust the cursor and proceed.
		case !nodeCursor.canClimbRight():
			cds = append(cds, nodeCursor)
			leafCursor++
			reset()
		// proceed to climb higher by incrementing the relevant state and
		// progressing through the loop.
		default:
			lastLeafCursor = leafCursor
			lastNodeCursor = nodeCursor
			leafCursor = leafCursor + nodeRangeCursor
			nodeRangeCursor = nodeRangeCursor * 2
			nodeCursor = nodeCursor.climb()
		}
	}
}

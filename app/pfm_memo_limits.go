package app

// maxPFMForwardMemoDepth is the deepest JSON nesting accepted in a forward
// memo. The work to encode forward.next grows with the depth times the
// length. Each hop adds two levels, so 16 levels allow 8 hops.
const maxPFMForwardMemoDepth = 16

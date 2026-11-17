package app

// maxPFMForwardMemoDepth bounds forward memo nesting, and so the work to encode forward.next; 16 levels allow 8 hops.
const maxPFMForwardMemoDepth = 16

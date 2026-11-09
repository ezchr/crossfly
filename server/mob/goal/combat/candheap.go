//go:build crossfly_full

package combat

// candHeap is a binary min-heap of candidates by distance, then by the order
// they were found in: popping it gives the order of a stable sort by
// distance, without sorting the candidates never looked at.
type candHeap []cand

func (h candHeap) less(i, j int) bool {
	if h[i].dist != h[j].dist {
		return h[i].dist < h[j].dist
	}
	return h[i].i < h[j].i
}

func (h candHeap) init() {
	for i := len(h)/2 - 1; i >= 0; i-- {
		h.down(i)
	}
}

func (h candHeap) down(i int) {
	n := len(h)
	for {
		l := 2*i + 1
		if l >= n {
			return
		}
		j := l
		if r := l + 1; r < n && h.less(r, l) {
			j = r
		}
		if !h.less(j, i) {
			return
		}
		h[i], h[j] = h[j], h[i]
		i = j
	}
}

// pop removes the smallest candidate and returns it with the heap left.
func (h candHeap) pop() (cand, candHeap) {
	c := h[0]
	n := len(h) - 1
	h[0] = h[n]
	h = h[:n]
	h.down(0)
	return c, h
}

package jobs

// Select returns the k-th smallest element (0-based) using quickselect, reordering xs.
// Average O(n), versus O(n log n) for sorting just to read one percentile.
func Select(xs []float64, k int) float64 {
	lo, hi := 0, len(xs)-1
	for lo < hi {
		p := partition(xs, lo, hi)
		switch {
		case k == p:
			return xs[k]
		case k < p:
			hi = p - 1
		default:
			lo = p + 1
		}
	}
	return xs[lo]
}

// partition is Lomuto with a median-of-three pivot, which avoids the O(n^2) case on sorted input.
func partition(xs []float64, lo, hi int) int {
	mid := lo + (hi-lo)/2
	if xs[mid] < xs[lo] {
		xs[mid], xs[lo] = xs[lo], xs[mid]
	}
	if xs[hi] < xs[lo] {
		xs[hi], xs[lo] = xs[lo], xs[hi]
	}
	if xs[mid] < xs[hi] {
		xs[mid], xs[hi] = xs[hi], xs[mid]
	}
	pivot, i := xs[hi], lo
	for j := lo; j < hi; j++ {
		if xs[j] < pivot {
			xs[i], xs[j] = xs[j], xs[i]
			i++
		}
	}
	xs[i], xs[hi] = xs[hi], xs[i]
	return i
}

package observability

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"math"
)

// QuantileEstimate interpolates within the framework's cumulative histogram.
// Overflow means the estimate exceeds the largest finite bound. No samples
// means Available=false, never a fabricated zero duration.
type QuantileEstimate struct {
	Seconds             float64
	Available, Overflow bool
}

func EstimateQuantile(buckets []uint64, count uint64, q float64) (QuantileEstimate, error) {
	if len(buckets) != len(durationBounds) || math.IsNaN(q) || q < 0 || q > 1 {
		return QuantileEstimate{}, fault.New(fault.Invalid, "invalid histogram quantile")
	}
	var previous uint64
	for _, value := range buckets {
		if value < previous || value > count {
			return QuantileEstimate{}, fault.New(fault.Invalid, "invalid cumulative histogram")
		}
		previous = value
	}
	if count == 0 {
		return QuantileEstimate{}, nil
	}
	target := q * float64(count)
	var lower float64
	previous = 0
	for i, bound := range durationBounds {
		upper := bound.Seconds()
		if float64(buckets[i]) >= target && buckets[i] > previous {
			return QuantileEstimate{Seconds: lower + (upper-lower)*(target-float64(previous))/float64(buckets[i]-previous), Available: true}, nil
		}
		lower = upper
		previous = buckets[i]
	}
	return QuantileEstimate{Seconds: lower, Available: true, Overflow: true}, nil
}

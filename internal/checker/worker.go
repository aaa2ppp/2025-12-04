package checker

import (
	"math"
	"time"
)

func runWorker(firstJob job, maxIdle time.Duration, nextJobs <-chan job) {
	firstJob.run()

	tm := time.NewTimer(time.Duration(math.MaxInt64))
	for {
		if maxIdle > 0 {
			tm.Reset(maxIdle)
		}
		select {
		case <-tm.C:
			return
		case job, ok := <-nextJobs:
			if !ok {
				tm.Stop()
				return
			}
			job.run()
		}
	}
}

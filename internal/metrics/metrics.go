package metrics

import "sync/atomic"

var jobsSucceeded int64
var jobsFailed int64

func IncSucceeded() { atomic.AddInt64(&jobsSucceeded, 1) }
func IncFailed()    { atomic.AddInt64(&jobsFailed, 1) }

func Snapshot() map[string]int64 {
    return map[string]int64{
        "jobs_succeeded": atomic.LoadInt64(&jobsSucceeded),
        "jobs_failed":    atomic.LoadInt64(&jobsFailed),
    }
}

package runtime

import (
	"feidex/internal/application/autoretry"
	"time"
)

func ScheduleDelayedTask(delay time.Duration, fn func()) autoretry.DelayedTask {
	return time.AfterFunc(delay, fn)
}

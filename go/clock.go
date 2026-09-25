package ctxsdk

import "time"

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) AfterFunc(d time.Duration, f func()) Timer {
	return time.AfterFunc(d, f)
}

func (systemClock) Sleep(d time.Duration) { time.Sleep(d) }

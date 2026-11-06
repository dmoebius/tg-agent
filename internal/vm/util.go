package vm

import "time"

func after(sec int) <-chan time.Time { return time.After(time.Duration(sec) * time.Second) }

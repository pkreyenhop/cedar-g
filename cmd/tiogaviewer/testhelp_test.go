package main

import "time"

func timeAfter() <-chan time.Time { return time.After(2 * time.Second) }

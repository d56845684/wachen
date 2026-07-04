package main

import (
	"testing"
	"time"

	"github.com/robfig/cron/v3"
)

func TestDueNow(t *testing.T) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	everyMinute, err := parser.Parse("* * * * *")
	if err != nil {
		t.Fatal(err)
	}
	last := time.Date(2026, 7, 4, 10, 0, 30, 0, time.UTC) // 下一次應為 10:01:00

	cases := []struct {
		name string
		last *time.Time
		now  time.Time
		want bool
	}{
		{"首次執行立即派工", nil, time.Date(2026, 7, 4, 10, 0, 50, 0, time.UTC), true},
		{"未到下次排程時間", &last, time.Date(2026, 7, 4, 10, 0, 50, 0, time.UTC), false},
		{"剛好到排程時間", &last, time.Date(2026, 7, 4, 10, 1, 0, 0, time.UTC), true},
		{"已超過排程時間", &last, time.Date(2026, 7, 4, 10, 1, 5, 0, time.UTC), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := dueNow(everyMinute, tc.last, tc.now); got != tc.want {
				t.Errorf("dueNow = %v, want %v", got, tc.want)
			}
		})
	}
}

package utils

import (
	"fmt"
	"sync"
	"time"

	"github.com/sony/sonyflake"
)

var (
	sf   *sonyflake.Sonyflake
	once sync.Once
)

func InitSnowflake() {
	once.Do(func() {
		sf = sonyflake.NewSonyflake(sonyflake.Settings{
			StartTime: time.Unix(0, 0),
		})
	})
}

func NextSnowID() (uint64, error) {
	if sf == nil {
		InitSnowflake()
	}
	id, err := sf.NextID()
	if err != nil {
		return 0, fmt.Errorf("failed to generate snowflake id: %w", err)
	}
	return id, nil
}

func NextSnowIDString() string {
	id, err := NextSnowID()
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d", id)
}

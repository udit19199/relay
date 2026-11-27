package helps

import (
	"time"
)

type CodexCache struct {
	ID     string
	Expire time.Time
}

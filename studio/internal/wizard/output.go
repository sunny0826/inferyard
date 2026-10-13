package wizard

import "time"

func uniqueID() int64 { return time.Now().UnixNano() }

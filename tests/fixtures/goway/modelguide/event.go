package modelguide

import "time"

//foundry:model table=guide_events var=Events key=ID
type Event struct {
	ID        int64
	CreatedAt time.Time `foundry:"role=time,default=database"`
}

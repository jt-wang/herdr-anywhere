package opencode

import (
	"database/sql"
	"encoding/json"

	"herdr-anywhere/companion/internal/chat"
)

func (r *Reader) Before(path, first string) (string, error) { return first, nil }

// History uses a keyset of message IDs, not OFFSET or an ever-growing read of the session. Both database
// generations are bounded to 4096 messages/parts and MaxBack bytes per request; oversize pages fail closed.
func (r *Reader) History(path, before string) (chat.HistoryPage, error) {
	p, id, err := split(path)
	if err != nil {
		return chat.HistoryPage{}, err
	}
	d, err := open(p)
	if err != nil {
		return chat.HistoryPage{}, err
	}
	defer d.Close()
	v2 := table(d, "session_v2")
	tab, key, boundary := "session_message", "seq", "type = 'idle'"
	if !v2 {
		tab, key = "message", "time_created"
		boundary = "json_extract(data, '$.role') = 'assistant' AND (json_type(data, '$.error') NOT IN ('null') OR (json_extract(data, '$.time.completed') > 0 AND json_extract(data, '$.finish') NOT IN ('', 'tool-calls')))"
	}
	var end int64 = 1<<63 - 1
	endID := "\uffff"
	if before != "" {
		if err := d.QueryRow("SELECT "+key+", id FROM "+tab+" WHERE session_id = ? AND id = ?", id, before).Scan(&end, &endID); err != nil {
			return chat.HistoryPage{}, chat.ErrCursor
		}
	}
	// Start after a completed turn, not a queued user message inside one. One extra boundary proves more history.
	rs, err := d.Query("SELECT "+key+", id FROM "+tab+" WHERE session_id = ? AND ("+key+", id) < (?, ?) AND ("+boundary+") ORDER BY "+key+" DESC, id DESC LIMIT 31", id, end, endID)
	if err != nil {
		return chat.HistoryPage{}, err
	}
	var start int64
	var startID string
	n := 0
	for rs.Next() {
		if err = rs.Scan(&start, &startID); err != nil {
			break
		}
		n++
	}
	if e := rs.Err(); err == nil {
		err = e
	}
	rs.Close()
	if err != nil {
		return chat.HistoryPage{}, err
	}
	if n < 31 {
		start, startID = -1<<63, ""
	}
	const dataCols = "length(CAST(data AS BLOB)), CASE WHEN length(CAST(data AS BLOB)) <= 33554432 THEN data ELSE '' END"
	cols := "id, type, seq, time_created, " + dataCols
	if !v2 {
		cols = "id, '', time_created, time_created, " + dataCols
	}
	rs, err = d.Query("SELECT "+cols+" FROM "+tab+" WHERE session_id = ? AND ("+key+", id) >= (?, ?) AND ("+key+", id) < (?, ?) ORDER BY "+key+", id LIMIT 4097", id, start, startID, end, endID)
	if err != nil {
		return chat.HistoryPage{}, err
	}
	var ms []message
	budget := chat.MaxBack
	for rs.Next() {
		var m message
		var data string
		var size int
		if err = rs.Scan(&m.ID, &m.Type, &m.Seq, &m.Created, &size, &data); err != nil {
			break
		}
		budget -= size
		if budget < 0 || len(ms) == 4096 {
			err = chat.ErrHistoryLimit
			break
		}
		m.Data = json.RawMessage(data)
		ms = append(ms, m)
	}
	if e := rs.Err(); err == nil {
		err = e
	}
	rs.Close()
	if err != nil {
		return chat.HistoryPage{}, err
	}
	if !v2 {
		ms, err = history1(d, id, ms, &budget)
		if err != nil {
			return chat.HistoryPage{}, err
		}
	}
	ts := parseAll(ms)
	page := chat.HistoryPage{Turns: ts}
	if len(ts) > chat.MaxTurns {
		page.Turns = ts[len(ts)-chat.MaxTurns:]
	}
	if n == 31 && len(page.Turns) > 0 {
		page.Before = page.Turns[0].ID
	}
	return page, nil
}

func history1(d *sql.DB, id string, ms []message, budget *int) ([]message, error) {
	var old []msg1
	var parts []part1
	for _, m := range ms {
		old = append(old, msg1{ID: m.ID, Created: m.Created, Data: m.Data})
		rs, err := d.Query("SELECT id, message_id, length(CAST(data AS BLOB)), CASE WHEN length(CAST(data AS BLOB)) <= 33554432 THEN data ELSE '' END FROM part WHERE session_id = ? AND message_id = ? ORDER BY id LIMIT 4097", id, m.ID)
		if err != nil {
			return nil, err
		}
		for rs.Next() {
			var p part1
			var data string
			var size int
			if err = rs.Scan(&p.ID, &p.Message, &size, &data); err != nil {
				break
			}
			*budget -= size
			if *budget < 0 || len(parts) == 4096 {
				err = chat.ErrHistoryLimit
				break
			}
			p.Data = json.RawMessage(data)
			parts = append(parts, p)
		}
		if e := rs.Err(); err == nil {
			err = e
		}
		rs.Close()
		if err != nil {
			return nil, err
		}
	}
	return convert1(old, parts), nil
}

package install

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type ApprovalDecision struct {
	ID         string
	WorkshopID string
	JarHash    string
	Decision   bool
}

func GameRunning() bool { return DefaultSystem().Running(gameProcesses...) }

func SetApprovalDecision(existing []byte, d ApprovalDecision) ([]byte, bool, error) {
	var o *orderedObject
	if len(bytes.TrimSpace(existing)) == 0 {
		o = &orderedObject{vals: map[string]json.RawMessage{}}
		o.set("formatVersion", json.RawMessage("2"))
		o.set("mods", json.RawMessage("[]"))
	} else {
		var err error
		o, err = parseOrdered(bytes.TrimPrefix(existing, []byte("\xef\xbb\xbf")))
		if err != nil {
			return nil, false, fmt.Errorf("mod_approvals.json: %w", err)
		}
	}
	var mods []json.RawMessage
	if raw, ok := o.vals["mods"]; ok && string(bytes.TrimSpace(raw)) != "null" {
		if err := json.Unmarshal(raw, &mods); err != nil {
			return nil, false, fmt.Errorf("mod_approvals.json mods: %w", err)
		}
	}
	match := -1
	for i, m := range mods {
		var e struct {
			ID      string `json:"id"`
			JarHash string `json:"jar_hash"`
		}
		_ = json.Unmarshal(m, &e)
		if strings.EqualFold(e.ID, d.ID) && strings.EqualFold(e.JarHash, d.JarHash) {
			match = i
		}
	}
	want := json.RawMessage("false")
	if d.Decision {
		want = json.RawMessage("true")
	}
	if match >= 0 {
		eo, err := parseOrdered(mods[match])
		if err != nil {
			return nil, false, fmt.Errorf("mod_approvals.json entry: %w", err)
		}
		if string(bytes.TrimSpace(eo.vals["decision"])) == string(want) {
			return existing, false, nil
		}
		eo.set("decision", want)
		nb, err := eo.marshal()
		if err != nil {
			return nil, false, err
		}
		mods[match] = nb
	} else {
		e := &orderedObject{vals: map[string]json.RawMessage{}}
		idb, _ := json.Marshal(d.ID)
		e.set("id", idb)
		if ws := strings.TrimSpace(d.WorkshopID); ws != "" && isDigits(ws) {
			e.set("workshop_id", json.RawMessage(ws))
		}
		hb, _ := json.Marshal(strings.ToLower(d.JarHash))
		e.set("jar_hash", hb)
		e.set("decision", want)
		nb, err := e.marshal()
		if err != nil {
			return nil, false, err
		}
		mods = append(mods, nb)
	}
	mb, err := json.Marshal(mods)
	if err != nil {
		return nil, false, err
	}
	o.set("mods", mb)
	out, err := o.marshal()
	return out, true, err
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

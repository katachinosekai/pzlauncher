package webview

import (
	"fmt"
	"strings"
)

type QueueItem struct {
	ID    string
	Title string
}

type SubQueue struct {
	Items   []QueueItem
	Pos     int
	Done    int
	Already int
	Failed  []string
	Stopped bool
	Reason  string
}

func NewSubQueue(items []QueueItem) *SubQueue { return &SubQueue{Items: items} }

func (q *SubQueue) Current() (QueueItem, bool) {
	if q.Finished() {
		return QueueItem{}, false
	}
	return q.Items[q.Pos], true
}

func (q *SubQueue) Finished() bool { return q.Stopped || q.Pos >= len(q.Items) }

func (q *SubQueue) Stop(reason string) {
	if !q.Stopped {
		q.Stopped, q.Reason = true, reason
	}
}

func (q *SubQueue) Record(result, errText string) {
	it, ok := q.Current()
	if !ok {
		return
	}
	switch result {
	case ResultDone:
		q.Done++
	case ResultAlready:
		q.Already++
	case ResultNotSignedIn:
		q.Failed = append(q.Failed, it.label()+": not signed in")
		q.Stop("not signed in to Steam")
	default:
		if errText == "" {
			errText = "failed"
		}
		q.Failed = append(q.Failed, it.label()+": "+errText)
	}
	q.Pos++
}

func (it QueueItem) label() string {
	if it.Title != "" {
		return it.Title
	}
	return it.ID
}

func (q *SubQueue) Progress() string {
	it, ok := q.Current()
	if !ok {
		return q.Summary()
	}
	return fmt.Sprintf("Subscribing %d of %d: %s...", q.Pos+1, len(q.Items), it.label())
}

func (q *SubQueue) Summary() string {
	var b strings.Builder
	if q.Stopped && q.Pos < len(q.Items) {
		fmt.Fprintf(&b, "Stopped after %d of %d", q.Pos, len(q.Items))
		if q.Reason != "" {
			b.WriteString(" (" + q.Reason + ")")
		}
		b.WriteString(": ")
	} else {
		b.WriteString("Done: ")
	}
	fmt.Fprintf(&b, "%d subscribed", q.Done)
	if q.Already > 0 {
		fmt.Fprintf(&b, ", %d already subscribed", q.Already)
	}
	if len(q.Failed) > 0 {
		fmt.Fprintf(&b, ", %d failed (%s)", len(q.Failed), strings.Join(q.Failed, "; "))
	}
	b.WriteString(".")
	return b.String()
}
